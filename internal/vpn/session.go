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
	"sync/atomic"
	"time"

	"lmvpn/internal/auth"
	"lmvpn/internal/cidrsource"
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
	ServerURL     string
	SNIHost       string   // TLS SNI hostname for CDN
	ServerIPs     []string // CDN edge IPs for failover
	Username      string
	Password      string
	AuthMode      model.AuthMode
	Token         string // pre-obtained JWT (empty = fetch via HTTP login)
	RoutingMode   route.Mode
	CIDRV4        []string              // static IPv4 CIDRs (proxy/bypass mode)
	CIDRV6        []string              // static IPv6 CIDRs (proxy/bypass mode)
	CIDRV4URLs    []model.CIDRURLSource // IPv4 CIDR URL sources
	CIDRV6URLs    []model.CIDRURLSource // IPv6 CIDR URL sources
	MTUOverride   int                   // 0 = use server MTU
	TLSCACert     string                // inline CA cert PEM (wss only)
	TLSCAPath     string                // CA cert file path (wss only)
	TLSInsecure   bool                  // skip cert verification (wss only)
	TLSPinnedHash string                // SHA-256 cert pin (wss only)
	IPPreference  string                // "auto" (default), "v4", "v6" - hostname mode only
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

	// CIDR hit tracking. Set during setupTUN, cleared on cleanup.
	cidrTracker *cidrTracker

	// lastCfg stores the session config for RefreshCIDRs.
	lastCfg SessionConfig

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
	sm.lastCfg = cfg
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
	// Extract resources so we can clean them up outside the lock.
	// Setting them to nil here also prevents the run goroutine's
	// cleanup from double-cleaning (it will see nil and skip).
	routeMgr := sm.routeMgr
	conn := sm.conn
	dev := sm.dev
	sm.routeMgr = nil
	sm.conn = nil
	sm.dev = nil
	sm.cidrTracker = nil
	sm.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	// CRITICAL: remove routes BEFORE closing the TUN device. If the TUN
	// is closed first, the /1 cover routes still point at the dead TUN
	// and all traffic is blackholed until routeMgr.Cleanup() runs -
	// this causes a brief network outage (browsers, DNS, etc.).
	if routeMgr != nil {
		if err := routeMgr.Cleanup(); err != nil {
			log.L().Error("route cleanup error during disconnect", "error", err)
		}
	}

	// Now safe to close the transport and TUN device.
	if conn != nil {
		conn.Close()
	}
	if dev != nil {
		dev.Close()
	}

	// Wait for the run goroutine to fully exit. By now it should be
	// unblocked (ctx cancelled, conn/dev closed) and will see nil
	// routeMgr/conn/dev in cleanup, so it won't double-clean.
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

	// Fetch "before-proxy" CIDR lists ONCE before the reconnect loop.
	// These HTTP requests go through the physical NIC (routes are
	// clean). The result is reused across reconnection attempts so we
	// don't re-fetch on every retry. This runs outside the handshake
	// to avoid consuming the server's 30s ReadyTimeout budget.
	var beforeCIDRs []string
	allURLSources := append(append([]model.CIDRURLSource{}, cfg.CIDRV4URLs...), cfg.CIDRV6URLs...)
	if len(allURLSources) > 0 {
		sm.stats.SetConnectStep("fetch_cidrs")
		sm.setState(stats.StateConnecting)
		log.L().Info("fetching before-proxy CIDR lists", "url_count", len(allURLSources))
		fetched, err := cidrsource.FetchBeforeProxy(ctx, allURLSources)
		if err != nil {
			log.L().Error("fetch before-proxy CIDR lists failed (continuing)", "error", err)
		}
		beforeCIDRs = fetched
		log.L().Info("before-proxy CIDR lists ready", "total_cidrs", len(beforeCIDRs))
		sm.stats.SetConnectStep("")
	}

	backoff := time.Second
	maxBackoff := 60 * time.Second

	// Build the full target list. When ServerIPs is empty, connect via
	// hostname (IPPreference + race dialer apply). When ServerIPs is
	// non-empty, skip hostname and use IPs directly with failover.
	usingHostname := len(cfg.ServerIPs) == 0
	var targets []string
	if usingHostname {
		targets = []string{""} // "" = use base URL (hostname)
	} else {
		targets = cfg.ServerIPs // direct IP mode: sequential failover
	}
	ipIndex := 0

	for {
		if ctx.Err() != nil {
			sm.cleanup()
			return
		}

		targetIP := ""
		if ipIndex < len(targets) {
			targetIP = targets[ipIndex]
		}

		err := sm.connectOnce(ctx, cfg, targetIP, beforeCIDRs)
		if ctx.Err() != nil {
			sm.cleanup()
			return
		}

		if err != nil {
			log.L().Error("VPN connection failed", "error", err)

			// Safety net: ensure no TUN/routes leak from a failed
			// attempt. connectOnce should have already cleaned up, but
			// this guards against any path that returned early.
			sm.cleanupResources()

			// A TLS certificate verification failure on the original
			// hostname (ipIndex == 0, hostname mode) is not retryable:
			// the cert won't change between attempts, so stop the loop
			// and surface the reason to the user. On a CDN edge IP the
			// TLS error likely means that IP points to a different
			// server; skip it and try the next target.
			if tlsconfig.IsTLSError(err) {
				if usingHostname && ipIndex == 0 {
					log.L().Warn("fatal TLS error, stopping reconnect", "error", err)
					sm.setState(stats.StateError)
					if sm.onError != nil {
						sm.onError("tls_error", err.Error())
					}
					fatal = true
					sm.cleanup()
					return
				}
				log.L().Warn("TLS error on IP, skipping",
					"index", ipIndex, "ip", targets[ipIndex], "error", err)
			}

			// A fatal authentication failure (wrong password, disabled
			// account, expired token, rate limit) on the original
			// hostname is not retryable. On a CDN edge IP it likely
			// means the IP points to a different server that returned
			// 401/403, so skip it instead of stopping the loop.
			if code, msg, isFatal := fatalAuthError(err); isFatal {
				if usingHostname && ipIndex == 0 {
					log.L().Warn("fatal auth error, stopping reconnect", "code", code, "message", msg)
					sm.setState(stats.StateError)
					if sm.onError != nil {
						sm.onError(string(code), msg)
					}
					fatal = true
					sm.cleanup()
					return
				}
				log.L().Warn("auth error on IP, skipping",
					"index", ipIndex, "ip", targets[ipIndex], "code", code)
			}

			sm.setState(stats.StateReconnecting)

			// Try next target IP immediately.
			ipIndex++
			if ipIndex < len(targets) {
				log.L().Info("trying next server IP", "index", ipIndex, "ip", targets[ipIndex])
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
// beforeCIDRs contains CIDRs pre-fetched from "before" timing URL
// sources (fetched once in run(), reused across reconnection attempts).
func (sm *SessionManager) connectOnce(ctx context.Context, cfg SessionConfig, targetIP string, beforeCIDRs []string) error {
	sm.setState(stats.StateConnecting)
	sm.stats.SetConnectStep("connecting")

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
		result, err := auth.Login(ctx, httpBase, cfg.Username, cfg.Password, tlsCfg, cfg.IPPreference)
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
		ServerURL:    serverURL,
		SNIHost:      cfg.SNIHost,
		Token:        token,
		Username:     cfg.Username,
		Password:     cfg.Password,
		IPPreference: cfg.IPPreference,
		OnInit: func(init protocol.InitMessage) error {
			return sm.setupTUN(init, cfg, beforeCIDRs)
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
				// Clean up any resources created by the failed attempt's
				// setupTUN before retrying.
				sm.cleanupResources()
				handshake.Token = ""
				conn, err = transport.Connect(ctx, handshake)
			}
		}
		if err != nil {
			// Clean up TUN/routes if setupTUN ran but the handshake
			// failed (e.g. server closed the connection after
			// ReadyTimeout). Without this, leaked /1 cover routes
			// blackhole all traffic and prevent reconnection.
			sm.cleanupResources()
			return err
		}
	}

	sm.mu.Lock()
	sm.conn = conn
	sm.mu.Unlock()

	serverHost := serverHostFromURL(cfg.ServerURL)
	connectedIP := conn.RemoteIP()
	sm.stats.SetConnected(conn.Init().IP, conn.Init().IP6, serverHost, connectedIP)
	sm.stats.SetConnectStep("load_routes")
	sm.setState(stats.StateConnected)
	log.L().Info("VPN connected",
		"ip", conn.Init().IP, "server_ip", conn.Init().ServerIP,
		"ip6", conn.Init().IP6, "server_ip6", conn.Init().ServerIP6,
		"mtu", conn.Init().MTU,
		"server_host", serverHost, "connected_ip", connectedIP)

	// Start stats reporter.
	statsDone := make(chan struct{})
	go sm.reportStats(statsDone, ctx)

	// Apply deferred routes (user CIDRs) and fetch after-proxy CIDR
	// lists in a background goroutine. For proxy/bypass modes with
	// thousands of CIDRs this uses batch script execution (~3-5s).
	go func() {
		sm.mu.Lock()
		mgr := sm.routeMgr
		sm.mu.Unlock()
		if mgr != nil {
			sm.stats.SetRouteLoading(true)
			sm.stats.SetConnectStep("load_routes")
			log.L().Info("applying deferred routes")
			if err := mgr.ApplyDeferred(); err != nil {
				log.L().Error("apply deferred routes failed (continuing)", "error", err)
			}
			sm.stats.SetRouteLoading(false)
			sm.stats.SetConnectStep("")
			log.L().Info("deferred routes applied")
		}
		sm.fetchAfterProxyCIDRs(ctx, cfg)
	}()

	// Run the packet pump (blocks until connection breaks).
	sm.pumpPackets(ctx, conn)

	close(statsDone)
	sm.cleanup()
	return nil
}

// setupTUN creates and configures the TUN device and applies routes.
// This is called by the transport during the handshake, between init
// and ready. It performs NO network calls (HTTP/DNS) so it completes
// in milliseconds and never exceeds the server's ReadyTimeout.
//
// beforeCIDRs contains CIDRs fetched from "before" timing URL sources,
// pre-fetched by connectOnce before the handshake began.
func (sm *SessionManager) setupTUN(init protocol.InitMessage, cfg SessionConfig, beforeCIDRs []string) error {
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
		return fmt.Errorf("invalid init IPs: %s / %s", init.IP, init.ServerIP)
	}

	if err := dev.Configure(localIP, init.Prefix, peerIP); err != nil {
		return fmt.Errorf("configure tun: %w", err)
	}

	// Configure IPv6 address when the server assigned one (dual-stack).
	hasV6 := init.IP6 != ""
	if hasV6 {
		ip6 := net.ParseIP(init.IP6)
		if ip6 == nil {
			return fmt.Errorf("invalid init IPv6: %s", init.IP6)
		}
		if err := dev.ConfigureIPv6(ip6, init.Prefix6); err != nil {
			return fmt.Errorf("configure tun ipv6: %w", err)
		}
	}

	mtu := init.MTU
	if cfg.MTUOverride > 0 {
		mtu = cfg.MTUOverride
	}
	if err := dev.SetMTU(mtu); err != nil {
		return fmt.Errorf("set mtu: %w", err)
	}

	// Merge static CIDRs with pre-fetched before-proxy CIDRs.
	var cidrs []string
	cidrs = append(cidrs, cfg.CIDRV4...)
	cidrs = append(cidrs, cfg.CIDRV6...)
	cidrs = append(cidrs, beforeCIDRs...)

	// Apply routing.
	routeCfg := route.Config{
		Mode:          cfg.RoutingMode,
		InterfaceName: dev.Name(),
		VPNIP:         init.IP,
		VPNPrefix:     init.Prefix,
		VPNIP6:        init.IP6,
		VPNPrefix6:    init.Prefix6,
		ServerHost:    serverHostFromURL(cfg.ServerURL),
		CIDRs:         cidrs,
	}
	sm.routeMgr = route.NewManager(routeCfg)
	if err := sm.routeMgr.Apply(); err != nil {
		log.L().Error("route apply failed (continuing)", "error", err)
	}

	// Initialise the CIDR hit tracker for proxy/bypass modes.
	sm.cidrTracker = newCIDRTracker(cfg.RoutingMode, cidrs)

	// Log CIDR breakdown by family for diagnostics.
	v4Total, _, v6Total, _ := sm.cidrTracker.Stats()
	log.L().Info("TUN configured",
		"dev", dev.Name(), "ip", init.IP, "prefix", init.Prefix,
		"ip6", init.IP6, "prefix6", init.Prefix6, "mtu", mtu,
		"routing_mode", cfg.RoutingMode,
		"cidr_v4", v4Total, "cidr_v6", v6Total,
		"before_proxy_cidrs", len(beforeCIDRs))
	return nil
}

// fetchAfterProxyCIDRs fetches CIDR lists from URLs with "after" timing
// (via the tunnel) and dynamically adds their routes to the route
// manager. This is called in a goroutine after the data plane is up.
func (sm *SessionManager) fetchAfterProxyCIDRs(ctx context.Context, cfg SessionConfig) {
	allURLSources := append(append([]model.CIDRURLSource{}, cfg.CIDRV4URLs...), cfg.CIDRV6URLs...)
	// Count only "after" sources for logging.
	afterCount := 0
	for _, s := range allURLSources {
		if s.FetchTiming == model.FetchAfter {
			afterCount++
		}
	}
	if afterCount == 0 {
		return
	}

	sm.stats.SetRouteLoading(true)
	sm.stats.SetConnectStep("fetch_cidrs")
	log.L().Info("fetching after-proxy CIDR lists", "url_count", afterCount)
	fetched, err := cidrsource.FetchAfterProxy(ctx, allURLSources)
	sm.stats.SetConnectStep("load_routes")
	if err != nil {
		log.L().Error("fetch after-proxy CIDR lists completed with errors", "error", err)
		sm.stats.SetCIDRError(err.Error())
	} else {
		sm.stats.SetCIDRError("")
	}
	if len(fetched) == 0 {
		sm.stats.SetRouteLoading(false)
		sm.stats.SetConnectStep("")
		return
	}

	merged := fetched // AddRoutes already calls mergeCIDRs internally
	added := sm.addCIDRRoutes(fetched)
	if added > 0 {
		log.L().Info("added after-proxy routes", "fetched", len(fetched), "added", added, "merged", len(merged))
	}

	sm.stats.SetRouteLoading(false)
	sm.stats.SetConnectStep("")
}

// addCIDRRoutes adds routes and updates the CIDR tracker. Returns the
// number of CIDRs successfully added (after merge).
func (sm *SessionManager) addCIDRRoutes(cidrs []string) int {
	sm.mu.Lock()
	mgr := sm.routeMgr
	tracker := sm.cidrTracker
	sm.mu.Unlock()
	if mgr == nil {
		return 0
	}

	if err := mgr.AddRoutes(cidrs); err != nil {
		log.L().Error("add CIDR routes failed (continuing)", "error", err)
	}
	if tracker != nil {
		tracker.AddCIDRs(cidrs)
	}
	return len(cidrs)
}

// RefreshCIDRs re-fetches all CIDR URL sources (both before and after
// timing) via the current tunnel and dynamically adds their routes.
// This is called when the user clicks the "Refresh CIDR" button.
func (sm *SessionManager) RefreshCIDRs() {
	sm.mu.Lock()
	ctx := sm.cancel
	cfg := sm.lastCfg
	sm.mu.Unlock()
	if ctx == nil {
		return
	}

	// Use a background context with 30s timeout so the refresh works
	// even if the session context is in a weird state.
	refreshCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = refreshCtx

	allURLSources := append(append([]model.CIDRURLSource{}, cfg.CIDRV4URLs...), cfg.CIDRV6URLs...)
	if len(allURLSources) == 0 {
		return
	}

	sm.stats.SetRouteLoading(true)
	sm.stats.SetCIDRError("")
	log.L().Info("refreshing CIDR lists", "url_count", len(allURLSources))

	fetched, err := cidrsource.FetchAfterProxy(refreshCtx, allURLSources)
	if err != nil {
		log.L().Error("refresh CIDR lists completed with errors", "error", err)
		sm.stats.SetCIDRError(err.Error())
	} else {
		sm.stats.SetCIDRError("")
	}
	if len(fetched) == 0 {
		sm.stats.SetRouteLoading(false)
		return
	}

	added := sm.addCIDRRoutes(fetched)
	log.L().Info("refreshed CIDR routes", "fetched", len(fetched), "added", added)
	sm.stats.SetRouteLoading(false)
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
		// Cache the cidrTracker pointer once; it is stable for the
		// lifetime of pumpPackets (set in setupTUN, cleared in cleanup
		// which only runs after pumpPackets returns).
		sm.mu.Lock()
		tracker := sm.cidrTracker
		sm.mu.Unlock()
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
			if tracker != nil {
				tracker.Record(buf[:n])
			}
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

// cleanupResources tears down the TUN device, routes, and transport
// connection WITHOUT changing the session state. This is used on
// handshake-failure paths where the caller will set the appropriate
// state (e.g. StateReconnecting). Returns without error if nothing
// was set up.
func (sm *SessionManager) cleanupResources() {
	sm.mu.Lock()
	dev := sm.dev
	routeMgr := sm.routeMgr
	conn := sm.conn
	sm.dev = nil
	sm.routeMgr = nil
	sm.conn = nil
	sm.cidrTracker = nil
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
}

// cleanup tears down the TUN device, routes, and transport, then marks
// the session as disconnected. Used on normal session termination.
func (sm *SessionManager) cleanup() {
	sm.cleanupResources()
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

			// Fill in CIDR hit statistics.
			sm.mu.Lock()
			tracker := sm.cidrTracker
			sm.mu.Unlock()
			if tracker != nil {
				v4Total, v4Hits, v6Total, v6Hits := tracker.Stats()
				snap.RoutingMode = string(tracker.mode)
				snap.CIDRV4Total = v4Total
				snap.CIDRV4Hits = v4Hits
				snap.CIDRV6Total = v6Total
				snap.CIDRV6Hits = v6Hits
			}

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

// cidrTracker tracks which configured CIDRs have been "hit" by
// outbound traffic (TUN -> WebSocket). The behaviour differs by mode:
//
//   - Proxy mode: each outbound packet's destination IP is matched
//     against the CIDR list; the first matching CIDR is marked as hit.
//     Stats() returns the count of hit CIDRs vs total CIDRs.
//   - Bypass mode: outbound traffic through TUN is, by definition,
//     traffic that did NOT match any bypass CIDR (bypassed traffic
//     goes via the physical NIC). We count distinct destination
//     /24 (v4) or /48 (v6) prefixes seen on TUN as "unmatched
//     destinations". Stats() returns the total bypass CIDR count
//     and the unmatched destination count.
type cidrTracker struct {
	mode route.Mode
	mu   sync.RWMutex

	// Proxy mode: pre-parsed CIDR nets + per-CIDR hit flags.
	// Protected by mu for concurrent AddCIDRs (write) vs Record/Stats
	// (read). The atomic.Bool hit flags are individually atomic, but
	// the slices themselves need the lock.
	v4CIDRs []*net.IPNet
	v6CIDRs []*net.IPNet
	v4Hits  []atomic.Bool
	v6Hits  []atomic.Bool

	// Bypass mode: distinct destination prefix sets.
	// v4 key = first 3 bytes of dest IP (a /24 prefix).
	// v6 key = first 6 bytes of dest IP (a /48 prefix).
	// sync.Map is already goroutine-safe; no lock needed.
	v4Prefixes sync.Map
	v6Prefixes sync.Map
	v4Count    atomic.Int64
	v6Count    atomic.Int64
}

// newCIDRTracker creates a tracker for the given routing mode and CIDR
// list. For full-tunnel mode, a tracker is still created but will
// report zero totals (no CIDRs configured).
func newCIDRTracker(mode route.Mode, cidrs []string) *cidrTracker {
	t := &cidrTracker{mode: mode}
	if mode == route.ModeFull {
		return t
	}
	t.addCIDRs(cidrs)
	return t
}

// AddCIDRs appends additional CIDRs to the tracker (used for
// after-proxy fetched CIDRs). Existing hit flags are preserved.
func (t *cidrTracker) AddCIDRs(cidrs []string) {
	t.addCIDRs(cidrs)
}

func (t *cidrTracker) addCIDRs(cidrs []string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, cidrStr := range cidrs {
		cidrStr = strings.TrimSpace(cidrStr)
		if cidrStr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidrStr)
		if err != nil {
			continue
		}
		if ipNet.IP.To4() != nil {
			t.v4CIDRs = append(t.v4CIDRs, ipNet)
			t.v4Hits = append(t.v4Hits, atomic.Bool{})
		} else {
			t.v6CIDRs = append(t.v6CIDRs, ipNet)
			t.v6Hits = append(t.v6Hits, atomic.Bool{})
		}
	}
}

// Record inspects an outbound IP packet (from TUN) and updates hit
// counters. It is called from the TUN -> WebSocket goroutine for every
// packet. Packets too short to contain an IP header or with an unknown
// version are silently ignored.
func (t *cidrTracker) Record(p []byte) {
	if len(p) < 1 {
		return
	}
	switch p[0] >> 4 {
	case 4:
		if len(p) < 20 {
			return
		}
		dst := net.IP(p[16:20])
		t.recordV4(dst)
	case 6:
		if len(p) < 40 {
			return
		}
		dst := net.IP(p[24:40])
		t.recordV6(dst)
	}
}

func (t *cidrTracker) recordV4(dst net.IP) {
	switch t.mode {
	case route.ModeProxy:
		t.mu.RLock()
		defer t.mu.RUnlock()
		for i, cidr := range t.v4CIDRs {
			if !t.v4Hits[i].Load() && cidr.Contains(dst) {
				t.v4Hits[i].Store(true)
			}
		}
	case route.ModeBypass:
		key := string(dst.To4()[:3])
		if _, loaded := t.v4Prefixes.LoadOrStore(key, struct{}{}); !loaded {
			t.v4Count.Add(1)
		}
	}
}

func (t *cidrTracker) recordV6(dst net.IP) {
	switch t.mode {
	case route.ModeProxy:
		t.mu.RLock()
		defer t.mu.RUnlock()
		for i, cidr := range t.v6CIDRs {
			if !t.v6Hits[i].Load() && cidr.Contains(dst) {
				t.v6Hits[i].Store(true)
			}
		}
	case route.ModeBypass:
		v6 := dst.To16()
		if len(v6) < 6 {
			return
		}
		key := string(v6[:6])
		if _, loaded := t.v6Prefixes.LoadOrStore(key, struct{}{}); !loaded {
			t.v6Count.Add(1)
		}
	}
}

// Stats returns the total and hit counts for IPv4 and IPv6 CIDRs.
//
// For proxy mode: "hits" = number of CIDRs that have been matched by
// at least one outbound packet.
// For bypass mode: "hits" = number of distinct destination prefixes
// seen on TUN (i.e. unmatched destinations that went through the
// tunnel because they didn't match any bypass CIDR).
func (t *cidrTracker) Stats() (v4Total, v4Hits, v6Total, v6Hits int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	switch t.mode {
	case route.ModeProxy:
		v4Total = len(t.v4CIDRs)
		for i := range t.v4Hits {
			if t.v4Hits[i].Load() {
				v4Hits++
			}
		}
		v6Total = len(t.v6CIDRs)
		for i := range t.v6Hits {
			if t.v6Hits[i].Load() {
				v6Hits++
			}
		}
	case route.ModeBypass:
		v4Total = len(t.v4CIDRs)
		v4Hits = int(t.v4Count.Load())
		v6Total = len(t.v6CIDRs)
		v6Hits = int(t.v6Count.Load())
	}
	return
}
