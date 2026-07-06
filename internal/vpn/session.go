// Package vpn orchestrates the full VPN session lifecycle: transport
// connection, TUN device setup, route management, the bidirectional
// packet pump, and automatic reconnection with exponential backoff.
//
// A SessionManager runs in its own goroutine once Connect is called.
// State changes and periodic stats are reported via callbacks, making
// it suitable for both the daemon (IPC) and headless use.
package vpn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"lmvpn/internal/auth"
	"lmvpn/internal/log"
	"lmvpn/internal/model"
	"lmvpn/internal/protocol"
	"lmvpn/internal/route"
	"lmvpn/internal/stats"
	"lmvpn/internal/transport"
	"lmvpn/internal/tun"
)

// SessionConfig describes how to connect to a VPN server.
type SessionConfig struct {
	ServerURL   string
	SNIHost     string   // TLS SNI hostname for CDN
	ServerIPs   []string // CDN edge IPs for failover
	Username    string
	Password    string
	AuthMode    model.AuthMode
	Token       string // pre-obtained JWT (empty = fetch via HTTP login)
	RoutingMode route.Mode
	CustomCIDRs []string
	MTUOverride int // 0 = use server MTU
}

// SessionManager manages a single VPN session with auto-reconnect.
type SessionManager struct {
	stats   *stats.Stats
	onState func(stats.State)
	onStats func(stats.Snapshot)

	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	dev      tun.Device
	routeMgr *route.Manager
	conn     *transport.Conn
}

// New creates a SessionManager. The onState callback (if non-nil) is
// invoked on every state transition. The onStats callback (if non-nil)
// is invoked periodically while connected.
func New(onState func(stats.State), onStats func(stats.Snapshot)) *SessionManager {
	return &SessionManager{
		stats:   stats.New(),
		onState: onState,
		onStats: onStats,
	}
}

// Stats returns the live stats handle.
func (sm *SessionManager) Stats() *stats.Stats { return sm.stats }

// State returns the current session state.
func (sm *SessionManager) State() stats.State { return sm.stats.State() }

// Connect starts the VPN session. It returns immediately; the session
// runs in a background goroutine until Disconnect is called or the
// context is cancelled. If already running, it returns an error.
func (sm *SessionManager) Connect(ctx context.Context, cfg SessionConfig) error {
	sm.mu.Lock()
	if sm.running {
		sm.mu.Unlock()
		return errors.New("session already running")
	}
	ctx, cancel := context.WithCancel(ctx)
	sm.cancel = cancel
	sm.running = true
	sm.mu.Unlock()

	go sm.run(ctx, cfg)
	return nil
}

// Disconnect stops the session and cleans up resources. It blocks
// until the session has fully shut down.
func (sm *SessionManager) Disconnect() {
	sm.mu.Lock()
	if !sm.running {
		sm.mu.Unlock()
		return
	}
	sm.running = false
	cancel := sm.cancel
	sm.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	// Close the transport to unblock the packet pump.
	sm.mu.Lock()
	conn := sm.conn
	sm.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
}

// run is the main session loop with exponential-backoff reconnection
// and CDN IP failover.
func (sm *SessionManager) run(ctx context.Context, cfg SessionConfig) {
	defer sm.setState(stats.StateDisconnected)

	backoff := time.Second
	maxBackoff := 60 * time.Second

	// Build the full target list: original host first, then CDN IPs.
	targets := append([]string{""}, cfg.ServerIPs...) // "" = use base URL
	ipIndex := 0

	for {
		if ctx.Err() != nil {
			return
		}

		targetIP := ""
		if ipIndex > 0 && ipIndex < len(targets) {
			targetIP = targets[ipIndex]
		}

		err := sm.connectOnce(ctx, cfg, targetIP)
		if ctx.Err() != nil {
			sm.cleanup()
			return
		}

		if err != nil {
			log.L().Error("VPN connection failed", "error", err)
			sm.setState(stats.StateReconnecting)

			// Try next CDN IP immediately.
			ipIndex++
			if ipIndex < len(targets) {
				log.L().Info("trying next CDN IP", "index", ipIndex, "ip", targets[ipIndex])
				continue
			}
			// All targets exhausted; reset and wait with backoff.
			ipIndex = 0
		} else {
			sm.setState(stats.StateReconnecting)
			ipIndex = 0
		}

		select {
		case <-ctx.Done():
			sm.cleanup()
			return
		case <-time.After(backoff):
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// connectOnce performs a single connection lifecycle: authenticate,
// handshake, configure TUN, apply routes, pump packets until failure.
func (sm *SessionManager) connectOnce(ctx context.Context, cfg SessionConfig, targetIP string) error {
	sm.setState(stats.StateConnecting)

	// Build URL for this attempt. If targetIP is set (CDN failover),
	// build a URL with that IP. Otherwise use base ServerURL.
	serverURL := cfg.ServerURL
	if targetIP != "" {
		serverURL = replaceHost(cfg.ServerURL, targetIP)
	}

	// Determine auth strategy and obtain JWT if needed.
	token := cfg.Token
	if token == "" && (cfg.AuthMode == model.AuthModeJWT || cfg.AuthMode == model.AuthModeBoth) {
		httpBase, err := wsURLToHTTP(serverURL)
		if err != nil {
			return fmt.Errorf("parse server URL: %w", err)
		}
		result, err := auth.Login(httpBase, cfg.Username, cfg.Password)
		if err != nil {
			if cfg.AuthMode == model.AuthModeBoth {
				// Fall back to password auth.
				token = ""
			} else {
				return fmt.Errorf("login: %w", err)
			}
		} else {
			token = result.Token
		}
	}

	// Prepare the TUN + route setup callback (called during handshake,
	// between receiving init and sending ready).
	handshake := transport.HandshakeConfig{
		ServerURL: serverURL,
		SNIHost:   cfg.SNIHost,
		Token:     token,
		Username:  cfg.Username,
		Password:  cfg.Password,
		OnInit: func(init protocol.InitMessage) error {
			return sm.setupTUN(init, cfg)
		},
	}

	// Attempt JWT connection first; fall back to password on auth error.
	conn, err := transport.Connect(ctx, handshake)
	if err != nil {
		if cfg.AuthMode == model.AuthModeBoth && token != "" {
			// JWT failure can surface as AuthError (password-auth path)
			// or ServerError with Type=auth_err (JWT rejected at /ws).
			var authErr *transport.AuthError
			var serverErr *transport.ServerError
			if errors.As(err, &authErr) ||
				(errors.As(err, &serverErr) && serverErr.Type == protocol.TypeAuthErr) {
				log.L().Info("JWT auth failed, falling back to password auth", "error", err)
				handshake.Token = ""
				conn, err = transport.Connect(ctx, handshake)
			}
		}
		if err != nil {
			return err
		}
	}

	sm.mu.Lock()
	sm.conn = conn
	sm.mu.Unlock()

	sm.stats.SetConnected(conn.Init().IP)
	sm.setState(stats.StateConnected)
	log.L().Info("VPN connected",
		"ip", conn.Init().IP, "server_ip", conn.Init().ServerIP,
		"mtu", conn.Init().MTU)

	// Start stats reporter.
	statsDone := make(chan struct{})
	go sm.reportStats(statsDone, ctx)

	// Run the packet pump (blocks until connection breaks).
	sm.pumpPackets(ctx, conn)

	close(statsDone)
	sm.cleanup()
	return nil
}

// setupTUN creates and configures the TUN device and applies routes.
// This is called by the transport during the handshake, between init
// and ready.
func (sm *SessionManager) setupTUN(init protocol.InitMessage, cfg SessionConfig) error {
	dev, err := tun.Create("")
	if err != nil {
		return fmt.Errorf("create tun: %w", err)
	}
	sm.mu.Lock()
	sm.dev = dev
	sm.mu.Unlock()

	localIP := net.ParseIP(init.IP)
	peerIP := net.ParseIP(init.ServerIP)
	if localIP == nil || peerIP == nil {
		dev.Close()
		return fmt.Errorf("invalid init IPs: %s / %s", init.IP, init.ServerIP)
	}

	if err := dev.Configure(localIP, init.Prefix, peerIP); err != nil {
		dev.Close()
		return fmt.Errorf("configure tun: %w", err)
	}

	mtu := init.MTU
	if cfg.MTUOverride > 0 {
		mtu = cfg.MTUOverride
	}
	if err := dev.SetMTU(mtu); err != nil {
		dev.Close()
		return fmt.Errorf("set mtu: %w", err)
	}

	// Apply routing.
	routeCfg := route.Config{
		Mode:          cfg.RoutingMode,
		InterfaceName: dev.Name(),
		VPNIP:         init.IP,
		VPNPrefix:     init.Prefix,
		ServerHost:    serverHostFromURL(cfg.ServerURL),
		CustomCIDRs:   cfg.CustomCIDRs,
	}
	sm.routeMgr = route.NewManager(routeCfg)
	if err := sm.routeMgr.Apply(); err != nil {
		log.L().Error("route apply failed (continuing)", "error", err)
	}

	log.L().Info("TUN configured",
		"dev", dev.Name(), "ip", init.IP, "prefix", init.Prefix, "mtu", mtu)
	return nil
}

// pumpPackets runs the bidirectional packet loop until the connection
// breaks.
func (sm *SessionManager) pumpPackets(ctx context.Context, conn *transport.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	// TUN → WebSocket
	go func() {
		defer wg.Done()
		buf := make([]byte, 65536)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			n, err := sm.readTUN(buf)
			if err != nil {
				if ctx.Err() == nil {
					log.L().Error("tun read error", "error", err)
				}
				conn.Close()
				return
			}
			if n == 0 {
				continue
			}
			if err := conn.WritePacket(buf[:n]); err != nil {
				if ctx.Err() == nil {
					log.L().Error("ws write error", "error", err)
				}
				return
			}
			sm.stats.TxBytes.Add(int64(n))
		}
	}()

	// WebSocket → TUN
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			data, err := conn.ReadPacket()
			if err != nil {
				if ctx.Err() == nil {
					log.L().Error("ws read error", "error", err)
				}
				return
			}
			if _, err := sm.writeTUN(data); err != nil {
				if ctx.Err() == nil {
					log.L().Error("tun write error", "error", err)
				}
				conn.Close()
				return
			}
			sm.stats.RxBytes.Add(int64(len(data)))
		}
	}()

	wg.Wait()
}

// cleanup tears down the TUN device and routes.
func (sm *SessionManager) cleanup() {
	sm.mu.Lock()
	dev := sm.dev
	routeMgr := sm.routeMgr
	conn := sm.conn
	sm.dev = nil
	sm.routeMgr = nil
	sm.conn = nil
	sm.mu.Unlock()

	if routeMgr != nil {
		if err := routeMgr.Cleanup(); err != nil {
			log.L().Error("route cleanup error", "error", err)
		}
	}
	if dev != nil {
		dev.Close()
	}
	if conn != nil {
		conn.Close()
	}
	sm.stats.SetDisconnected()
}

func (sm *SessionManager) readTUN(p []byte) (int, error) {
	sm.mu.Lock()
	dev := sm.dev
	sm.mu.Unlock()
	if dev == nil {
		return 0, errors.New("tun device not available")
	}
	return dev.Read(p)
}

func (sm *SessionManager) writeTUN(p []byte) (int, error) {
	sm.mu.Lock()
	dev := sm.dev
	sm.mu.Unlock()
	if dev == nil {
		return 0, errors.New("tun device not available")
	}
	return dev.Write(p)
}

func (sm *SessionManager) setState(s stats.State) {
	sm.stats.SetState(s)
	if sm.onState != nil {
		sm.onState(s)
	}
}

// reportStats periodically calls the onStats callback while connected.
func (sm *SessionManager) reportStats(done <-chan struct{}, ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if sm.onStats != nil {
				sm.onStats(sm.stats.Snapshot())
			}
		}
	}
}

// serverHostFromURL extracts the host portion from a WebSocket URL.
func serverHostFromURL(wsURL string) string {
	u := wsURL
	for _, p := range []string{"wss://", "ws://"} {
		if len(u) > len(p) && u[:len(p)] == p {
			u = u[len(p):]
			break
		}
	}
	// Strip port.
	for i := 0; i < len(u); i++ {
		if u[i] == ':' {
			u = u[:i]
			break
		}
	}
	// Strip path.
	for i := 0; i < len(u); i++ {
		if u[i] == '/' {
			u = u[:i]
			break
		}
	}
	return u
}

// wsURLToHTTP converts a WebSocket URL to HTTP origin.
func wsURLToHTTP(wsURL string) (string, error) {
	return auth.WSURLToHTTP(wsURL)
}

// replaceHost substitutes the host portion of a URL string.
// e.g. wss://host:443/ws with 1.2.3.4 → wss://1.2.3.4:443/ws
func replaceHost(rawURL, newHost string) string {
	u := rawURL
	for _, prefix := range []string{"wss://", "ws://"} {
		if len(u) > len(prefix) && u[:len(prefix)] == prefix {
			rest := u[len(prefix):]
			// Find end of host (either port or path).
			end := 0
			for end < len(rest) && rest[end] != ':' && rest[end] != '/' {
				end++
			}
			return prefix + newHost + rest[end:]
		}
	}
	return rawURL
}
