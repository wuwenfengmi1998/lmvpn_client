// Package daemon implements the privileged daemon process that owns
// the WebSocket transport, TUN device, and routing. It receives
// commands from the GUI over an IPC unix socket and broadcasts
// state/stats events back.
//
// The daemon is launched (as root) by the GUI via osascript. It holds
// no persistent state — all configuration is provided by the GUI in
// the Start command.
//
// The daemon accepts --user-home, --uid, and --gid flags so it can:
//   - Write logs to the user's ~/Library/Logs/ (not /var/root)
//   - Chown the IPC socket so the user can connect
//   - Chown log files so the user can read them
package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"lmvpn/internal/ipc"
	"lmvpn/internal/log"
	"lmvpn/internal/model"
	"lmvpn/internal/paths"
	"lmvpn/internal/stats"
	"lmvpn/internal/vpn"
)

// Run starts the daemon and blocks until Shutdown is received or a
// signal (SIGINT/SIGTERM) is delivered.
//
// userHome, uid, gid are the invoking GUI user's home directory and
// IDs, used to place logs in the user's Library and chown the IPC
// socket so the non-root GUI can connect.
func Run(userHome string, uid, gid int) error {
	// Override paths to use the user's home directory (not root's).
	paths.SetUserHome(userHome)
	if err := paths.EnsureDirs(); err != nil {
		// Non-fatal: root can usually create these anyway.
		fmt.Fprintf(os.Stderr, "ensure dirs: %v\n", err)
	}

	log.Init(log.RoleDaemon, paths.DaemonLogFile())
	// Chown the daemon log file so the user can read it.
	chownToUser(paths.DaemonLogFile(), uid, gid)

	log.L().Info("lmvpn daemon starting",
		"user_home", userHome, "uid", uid, "gid", gid)

	server, err := ipc.NewServer()
	if err != nil {
		return fmt.Errorf("ipc server: %w", err)
	}
	defer server.Close()

	// Chown the IPC socket so the non-root GUI process can connect.
	// This is the critical fix: without it, the socket is owned by
	// root:wheel with mode 0660, and the user cannot dial it.
	chownToUser(paths.IPCSocketPath(), uid, gid)
	log.L().Info("daemon listening", "socket", paths.IPCSocketPath())

	d := &daemon{server: server}

	// Signal handling for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.L().Info("daemon received signal, shutting down")
		d.stopSession()
		server.Close()
		os.Exit(0)
	}()

	return server.Accept(d.handle)
}

// chownToUser changes the ownership of a file to the given uid:gid.
// Errors are logged but not fatal (e.g. if uid is -1).
func chownToUser(path string, uid, gid int) {
	if uid < 0 {
		return
	}
	if err := os.Chown(path, uid, gid); err != nil {
		log.L().Warn("chown failed", "path", path, "error", err)
	}
}

type daemon struct {
	server  *ipc.Server
	session *vpn.SessionManager
	cancel  context.CancelFunc
}

func (d *daemon) handle(conn net.Conn, req ipc.Request) {
	switch req.Cmd {
	case ipc.CmdStart:
		d.startSession(conn, req)
	case ipc.CmdStop:
		d.stopSession()
		_ = ipc.WriteOK(conn)
	case ipc.CmdShutdown:
		d.stopSession()
		_ = ipc.WriteOK(conn)
		d.server.Close()
		os.Exit(0)
	case ipc.CmdStats:
		if d.session != nil {
			snap := d.session.Stats().Snapshot()
			d.server.Broadcast(ipc.Event{Event: ipc.EvStats, Stats: &snap})
		}
		_ = ipc.WriteOK(conn)
	default:
		_ = ipc.WriteErr(conn, "unknown command: "+req.Cmd)
	}
}

func (d *daemon) startSession(conn net.Conn, req ipc.Request) {
	if req.Config == nil {
		_ = ipc.WriteErr(conn, "missing config")
		return
	}
	if d.session != nil {
		d.stopSession()
	}

	cfg := vpn.SessionConfig{
		ServerURL:   req.Config.ServerURL,
		SNIHost:     req.Config.SNIHost,
		ServerIPs:   req.Config.ServerIPs,
		Username:    req.Config.Username,
		Password:    req.Config.Password,
		Token:       req.Config.Token,
		AuthMode:    model.AuthMode(req.Config.AuthMode),
		RoutingMode: ipc.RoutingModeFromIPC(req.Config.RoutingMode),
		CustomCIDRs: req.Config.CustomCIDRs,
		MTUOverride: req.Config.MTUOverride,
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.session = vpn.New(
		func(s stats.State) {
			d.server.Broadcast(ipc.Event{Event: ipc.EvState, State: string(s)})
		},
		func(snap stats.Snapshot) {
			s := snap
			d.server.Broadcast(ipc.Event{Event: ipc.EvStats, Stats: &s})
		},
	)

	if err := d.session.Connect(ctx, cfg); err != nil {
		_ = ipc.WriteErr(conn, "connect: "+err.Error())
		d.session = nil
		return
	}
}

func (d *daemon) stopSession() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	if d.session != nil {
		d.session.Disconnect()
		d.session = nil
	}
}
