package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"lmvpn/internal/ipc"
	"lmvpn/internal/log"
	"lmvpn/internal/paths"
	"lmvpn/internal/version"
)

// ensureDaemon checks if the daemon is running and launches it (as
// root via osascript) if not. It blocks until the daemon is reachable
// or times out.
//
// The daemon is launched via the `daemon-launch` subcommand of the GUI
// binary, which uses Go's syscall.SysProcAttr{Setsid: true} to fork
// the actual daemon binary (lmvpnd) into a new session. This is the
// robust replacement for shell-level `nohup ... &` which fails inside
// osascript's `do shell script` (no TTY → ioctl error).
//
// The --daemon-bin flag tells daemon-launch where to find lmvpnd. If
// not given, daemon-launch auto-detects it relative to its own path.
func ensureDaemon() (*ipc.Client, error) {
	// Fast path: a daemon is already running. Verify its build version
	// matches ours; if not, it's a stale process from a previous build
	// and must be restarted so the new code takes effect.
	if c, err := ipc.Dial(); err == nil {
		if ver, qerr := c.QueryVersion(); qerr == nil && ver == version.Version {
			return c, nil
		} else {
			log.L().Info("stale daemon detected, restarting",
				"got", ver, "expected", version.Version, "query_err", qerr)
			_ = ipc.SendShutdown(c)
			c.Close()
			// Wait for the old daemon to exit and release the socket.
			for i := 0; i < 30; i++ {
				time.Sleep(100 * time.Millisecond)
				if cc, err := ipc.Dial(); err == nil {
					cc.Close()
				} else {
					break // socket gone
				}
			}
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	// Resolve symlinks (e.g. .app bundle's MacOS/lmvpn).
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, fmt.Errorf("resolve symlink: %w", err)
	}

	// Compute the daemon binary path: same directory as the GUI binary.
	daemonBin := filepath.Join(filepath.Dir(exe), daemonBinaryName)
	if _, err := os.Stat(daemonBin); err != nil {
		return nil, fmt.Errorf("daemon binary not found at %s: %w", daemonBin, err)
	}

	home, _ := os.UserHomeDir()
	uid := os.Getuid()
	gid := os.Getgid()

	if err := launchElevated(exe, daemonBin, home, uid, gid); err != nil {
		return nil, err
	}

	// Wait for the daemon to become reachable.
	logFile := paths.DaemonLogFile()
	for i := 0; i < 60; i++ {
		time.Sleep(250 * time.Millisecond)
		if c, err := ipc.Dial(); err == nil {
			log.L().Info("daemon connected", "socket", paths.IPCAddress())
			return c, nil
		} else if i == 0 || i == 10 || i == 30 || i == 59 {
			log.L().Warn("daemon not reachable yet, retrying",
				"attempt", i+1, "socket", paths.IPCAddress(), "error", err)
		}
	}
	return nil, fmt.Errorf("daemon did not become reachable (check %s)", logFile)
}


