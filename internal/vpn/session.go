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
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"lmvpn/internal/auth"
	"lmvpn/internal/log"
	"lmvpn/internal/model"
	"lmvpn/internal/protocol"
	"lmvpn/internal/route"
	"lmvpn/internal/stats"
	"lmvpn/internal/tlsconfig"
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
	TLSCACert     string // inline CA cert PEM (wss only)
	TLSCAPath     string // CA cert file path (wss only)
	TLSInsecure   bool   // skip cert verification (wss only)
	TLSPinnedHash string // SHA-256 cert pin (wss only)
}

// SessionManager manages a single VPN session with auto-reconnect.
type SessionManager struct {
	stats   *stats.Stats
	onState func(stats.State)
	onStats func(stats.Snapshot)
	onError func(code string, msg string)

	mu       sync.Mutex
	running  bool
	cancel   context.CancelFunc
	dev      tun.Device
	routeMgr *route.Manager
	conn     *transport.Conn
	done     chan struct{}

	// EWMA speed smoothing state. Only touched by reportStats (single
	// goroutine), so no lock needed. ewma* fields hold the smoothed
	// bytes/sec; prev* hold the last snapshot's cumulative counters and
	// tick time for delta computation.
	ewmaRxV4   float64
	ewmaTxV4   float64
	ewmaRxV6   float64
	ewmaTxV6   float64
	prevSnap   stats.Snapshot
	prevTick   time.Time
	speedReady bool
}

// New creates a SessionManager. The onState callback (if non-nil) is
// invoked on every state transition. The onStats callback (if non-nil)
// is invoked periodically while connected. The onError callback (if
// non-nil) is invoked once when a fatal, non-retryable error (such as
// an authentication failure) terminates the session.
func New(onState func(stats.State), onStats func(stats.Snapshot), onError func(string, string)) *SessionManager {
	return &SessionManager{
		stats:   stats.New(),
		onState: onState,
		onStats: onStats,
		onError: onError,
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
	sm.done = make(chan struct{})
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
	// Close the transport to unblock the WS->TUN goroutine.
	sm.mu.Lock()
	conn := sm.conn
	dev := sm.dev
	sm.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	// Close the TUN device to unblock the TUN->WS goroutine's dev.Read().
	if dev != nil {
		dev.Close()
	}
	// Wait for the run goroutine to fully exit so that cleanup
	// (route removal, TUN teardown) is complete before returning.
	if sm.done != nil {
		<-sm.done
	}
}

// run is the main session loop with exponential-backoff reconnection
// and CDN IP failover.
func (sm *SessionManager) run(ctx context.Context, cfg SessionConfig) {
	fatal := false
	defer close(sm.done)
	defer func() {
		if !fatal && ctx.Err() == nil {
			sm.setState(stats.StateDisconnected)
		}
	}()

	backoff := time.Second
	maxBackoff := 60 * time.Second

	// Build the full target list: original host first, then CDN IPs.
	targets := append([]string{""}, cfg.ServerIPs...) // "" = use base URL
	ipIndex := 0

	for {
		if ctx.Err() != nil {
			sm.cleanup()
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

			// A TLS certificate verification failure on the original
			// hostname (ipIndex == 0) is not retryable: the cert won't
			// change between attempts, so stop the loop and surface the
			// reason to the user. On a CDN edge IP (ipIndex > 0) the
			// TLS error likely means that IP points to a different
			// server; skip it and try the next target.
			if tlsconfig.IsTLSError(err) {
				if ipIndex == 0 {
					log.L().Warn("fatal TLS error, stopping reconnect", "error", err)
					sm.setState(stats.StateError)
					if sm.onError != nil {
						sm.onError("tls_error", err.Error())
					}
					fatal = true
					sm.cleanup()
					return
				}
				log.L().Warn("TLS error on CDN IP, skipping",
					"index", ipIndex, "ip", targets[ipIndex], "error", err)
			}

			// A fatal authentication failure (wrong password, disabled
			// account, expired token, rate limit) on the original
			// hostname is not retryable. On a CDN edge IP it likely
			// means the IP points to a different server that returned
			// 401/403, so skip it instead of stopping the loop.
			if code, msg, isFatal := fatalAuthError(err); isFatal {
				if ipIndex == 0 {
					log.L().Warn("fatal auth error, stopping reconnect", "code", code, "message", msg)
					sm.setState(stats.StateError)
					if sm.onError != nil {
						sm.onError(string(code), msg)
					}
					fatal = true
					sm.cleanup()
					return
				}
				log.L().Warn("auth error on CDN IP, skipping",
					"index", ipIndex, "ip", targets[ipIndex], "code", code)
			}

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

// fatalAuthError inspects err and, if it represents a permanent
// authentication failure that should not be retried, returns the
// stable error code, the raw server message, and true. It recognises
// all three auth-failure shapes produced by the transport/auth layers:
//   - *transport.AuthError        (WebSocket auth_err at the auth stage)
//   - *transport.ServerError      (auth_err at the init stage, JWT path)
//   - *auth.LoginError            (HTTP /api/login failure, JWT path)
//
// errors.As transparently unwraps fmt.Errorf("...: %w", err) chains,
// so the wrapped LoginError returned by connectOnce is matched too.
// A non-empty code is required: an auth_err with an unrecognized
// message is treated as non-fatal so the loop falls back to retrying
// (the server still closed the connection, but we lack a categorical
// reason to give up).
func fatalAuthError(err error) (protocol.AuthErrorCode, string, bool) {
	var authErr *transport.AuthError
	if errors.As(err, &authErr) {
		return authErr.Code, authErr.Message, authErr.Code != ""
	}
	var serverErr *transport.ServerError
	if errors.As(err, &serverErr) && serverErr.Type == protocol.TypeAuthErr {
		return serverErr.Code, serverErr.Message, serverErr.Code != ""
	}
	var loginErr *auth.LoginError
	if errors.As(err, &loginErr) {
		switch loginErr.Code {
		case http.StatusTooManyRequests:
			return protocol.AuthCodeRateLimited, loginErr.Message, true
		case http.StatusUnauthorized, http.StatusForbidden:
			return protocol.AuthCodeWrongCredentials, loginErr.Message, true
		}
	}
	return "", "", false
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

	// Build TLS config for wss:// connections. For ws:// there is no
	// TLS layer, so tlsCfg remains nil and both the HTTP client and
	// the WebSocket dialer use their default (plaintext) behaviour.
	var tlsCfg *tls.Config
	if strings.HasPrefix(serverURL, "wss://") {
		serverName := cfg.SNIHost
		if serverName == "" {
			serverName = serverHostFromURL(cfg.ServerURL)
		}
		var err error
		tlsCfg, err = tlsconfig.Build(tlsconfig.Config{
			ServerName:         serverName,
			CACertPEM:          cfg.TLSCACert,
			CACertPath:         cfg.TLSCAPath,
			InsecureSkipVerify: cfg.TLSInsecure,
			PinnedCertHash:     cfg.TLSPinnedHash,
		})
		if err != nil {
			return fmt.Errorf("tls config: %w", err)
		}
	}

	// Determine auth strategy and obtain JWT if needed.
	token := cfg.Token
	if token == "" && (cfg.AuthMode == model.AuthModeJWT || cfg.AuthMode == model.AuthModeBoth) {
		httpBase, err := wsURLToHTTP(serverURL)
		if err != nil {
			return fmt.Errorf("parse server URL: %w", err)
		}
		result, err := auth.Login(httpBase, cfg.Username, cfg.Password, tlsCfg)
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
		TLSConfig: tlsCfg,
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

	sm.stats.SetConnected(conn.Init().IP, conn.Init().IP6)
	sm.setState(stats.StateConnected)
	log.L().Info("VPN connected",
		"ip", conn.Init().IP, "server_ip", conn.Init().ServerIP,
		"ip6", conn.Init().IP6, "server_ip6", conn.Init().ServerIP6,
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

	// Configure IPv6 address when the server assigned one (dual-stack).
	hasV6 := init.IP6 != ""
	if hasV6 {
		ip6 := net.ParseIP(init.IP6)
		if ip6 == nil {
			dev.Close()
			return fmt.Errorf("invalid init IPv6: %s", init.IP6)
		}
		if err := dev.ConfigureIPv6(ip6, init.Prefix6); err != nil {
			dev.Close()
			return fmt.Errorf("configure tun ipv6: %w", err)
		}
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
		VPNIP6:        init.IP6,
		VPNPrefix6:    init.Prefix6,
		ServerHost:    serverHostFromURL(cfg.ServerURL),
		CustomCIDRs:   cfg.CustomCIDRs,
	}
	sm.routeMgr = route.NewManager(routeCfg)
	if err := sm.routeMgr.Apply(); err != nil {
		log.L().Error("route apply failed (continuing)", "error", err)
	}

	log.L().Info("TUN configured",
		"dev", dev.Name(), "ip", init.IP, "prefix", init.Prefix,
		"ip6", init.IP6, "prefix6", init.Prefix6, "mtu", mtu)
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
			sm.stats.AddTx(buf[:n])
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
			sm.stats.AddRx(data)
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
// On each tick it derives per-family speeds (bytes/sec) from the
// delta between the current and previous cumulative counters, then
// applies EWMA smoothing (0.7 old + 0.3 new) so the displayed rates
// don't jitter. The combined speeds are the sum of the per-family
// smoothed values.
func (sm *SessionManager) reportStats(done <-chan struct{}, ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	const ewmaAlpha = 0.3
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if sm.onStats == nil {
				continue
			}
			snap := sm.stats.Snapshot()
			now := time.Now()
			if sm.speedReady {
				elapsed := now.Sub(sm.prevTick).Seconds()
				if elapsed <= 0 {
					elapsed = 1
				}
				// Per-second deltas (bytes/sec), clamped to >= 0 in case
				// of counter resets between reconnects within the same
				// SessionManager lifetime.
				rxV4 := max(0.0, float64(snap.RxBytesV4-sm.prevSnap.RxBytesV4)/elapsed)
				txV4 := max(0.0, float64(snap.TxBytesV4-sm.prevSnap.TxBytesV4)/elapsed)
				rxV6 := max(0.0, float64(snap.RxBytesV6-sm.prevSnap.RxBytesV6)/elapsed)
				txV6 := max(0.0, float64(snap.TxBytesV6-sm.prevSnap.TxBytesV6)/elapsed)
				if sm.ewmaRxV4 == 0 && sm.ewmaTxV4 == 0 && sm.ewmaRxV6 == 0 && sm.ewmaTxV6 == 0 {
					// First real sample: seed instead of ramping from 0.
					sm.ewmaRxV4, sm.ewmaTxV4, sm.ewmaRxV6, sm.ewmaTxV6 = rxV4, txV4, rxV6, txV6
				} else {
					sm.ewmaRxV4 = sm.ewmaRxV4*(1-ewmaAlpha) + rxV4*ewmaAlpha
					sm.ewmaTxV4 = sm.ewmaTxV4*(1-ewmaAlpha) + txV4*ewmaAlpha
					sm.ewmaRxV6 = sm.ewmaRxV6*(1-ewmaAlpha) + rxV6*ewmaAlpha
					sm.ewmaTxV6 = sm.ewmaTxV6*(1-ewmaAlpha) + txV6*ewmaAlpha
				}
				snap.RxSpeedV4 = int64(sm.ewmaRxV4)
				snap.TxSpeedV4 = int64(sm.ewmaTxV4)
				snap.RxSpeedV6 = int64(sm.ewmaRxV6)
				snap.TxSpeedV6 = int64(sm.ewmaTxV6)
				snap.RxSpeed = snap.RxSpeedV4 + snap.RxSpeedV6
				snap.TxSpeed = snap.TxSpeedV4 + snap.TxSpeedV6
			}
			sm.prevSnap = snap
			// Clear speed fields on the stored prev copy so we don't
			// accidentally carry stale speed into the next delta base
			// (only cumulative bytes matter for deltas).
			sm.prevSnap.RxSpeedV4, sm.prevSnap.TxSpeedV4 = 0, 0
			sm.prevSnap.RxSpeedV6, sm.prevSnap.TxSpeedV6 = 0, 0
			sm.prevSnap.RxSpeed, sm.prevSnap.TxSpeed = 0, 0
			sm.prevTick = now
			sm.speedReady = true
			sm.onStats(snap)
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
// Bare IPv6 addresses are automatically bracketed:
// wss://host:443/ws with 2001:db8::1 → wss://[2001:db8::1]:443/ws
func replaceHost(rawURL, newHost string) string {
	// Auto-bracket bare IPv6 addresses so the colons in the address
	// are not confused with the port separator.
	if ip := net.ParseIP(newHost); ip != nil && ip.To4() == nil && !strings.HasPrefix(newHost, "[") {
		newHost = "[" + newHost + "]"
	}
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
