// Package route manages VPN routing on the client. It supports three
// modes:
//
//   - Full tunnel:   all traffic (0.0.0.0/0 and ::/0) via the TUN
//     interface, with bypass routes for the server's
//     public IP (v4 and v6) so the WebSocket connection
//     stays on the physical NIC
//   - Proxy CIDR:    only the specified CIDRs (v4 and v6) via TUN
//   - Bypass CIDR:   all traffic via TUN except the specified CIDRs,
//     which are routed via the original gateway
//
// Routing is applied in two phases to avoid blocking the server's
// ReadyTimeout:
//
//   - Apply (essential): server bypass + /1 cover routes (4-6 commands,
//     <1s). Runs inside the handshake before "ready" is sent.
//   - ApplyDeferred: user CIDR routes (potentially thousands). Runs in
//     a background goroutine after the tunnel is up. Uses batch script
//     execution to minimize process creation overhead.
//
// IPv6 routes are applied automatically when the server assigned an
// IPv6 address (Config.VPNIP6 != ""). All routes are tracked so they
// can be cleanly removed on disconnect.
package route

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"lmvpn/internal/log"
)

// Mode selects which traffic goes through the VPN tunnel.
type Mode string

const (
	ModeFull   Mode = "full"
	ModeProxy  Mode = "proxy"
	ModeBypass Mode = "bypass"
)

// Config describes the desired routing configuration.
type Config struct {
	Mode          Mode
	InterfaceName string   // e.g. "utun4"
	VPNIP         string   // assigned tunnel IPv4, e.g. "192.168.77.5"
	VPNPrefix     int      // IPv4 subnet prefix, e.g. 24
	VPNIP6        string   // assigned tunnel IPv6 (empty = v4-only)
	VPNPrefix6    int      // IPv6 subnet prefix
	ServerHost    string   // server hostname/IP (for full-tunnel bypass)
	CIDRs         []string // CIDR list for ModeProxy and ModeBypass
}

// Manager applies and removes routes. It tracks all added routes so
// they can be cleaned up deterministically. All methods are safe for
// concurrent use (Apply/ApplyDeferred may run in one goroutine while
// Cleanup runs in another).
type Manager struct {
	mu               sync.Mutex
	cfg              Config
	addedRoutes      []string // v4 route specs added via TUN, for deletion
	addedRoutes6     []string // v6 route specs added via TUN, for deletion
	bypassRoutes     []string // v4 bypass route specs added via gateway
	bypassRoutes6    []string // v6 bypass route specs added via gateway
	serverBypass     bool
	serverBypass6    bool
	originalGateway  string
	originalGateway6 string
	serverIP         string // resolved v4 (for bypass delete)
	serverIP6        string // resolved v6 (for bypass delete)
}

// NewManager creates a route manager for the given configuration.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// Apply adds essential routes that must be in place before the VPN
// tunnel "ready" signal is sent. This completes in <1 second (4-6
// route commands). User CIDR routes are deferred to ApplyDeferred.
func (m *Manager) Apply() error {
	switch m.cfg.Mode {
	case ModeFull:
		return m.applyFull()
	case ModeProxy:
		return m.applyProxyEssential()
	case ModeBypass:
		return m.applyBypassEssential()
	default:
		return fmt.Errorf("unknown routing mode: %s", m.cfg.Mode)
	}
}

// ApplyDeferred adds user CIDR routes that were deferred from Apply.
// This should be called in a background goroutine after the tunnel is
// up. For full-tunnel mode this is a no-op. For proxy/bypass modes it
// uses batch script execution to handle potentially thousands of CIDRs
// efficiently.
func (m *Manager) ApplyDeferred() error {
	switch m.cfg.Mode {
	case ModeFull:
		return nil
	case ModeProxy:
		return m.applyProxyDeferred()
	case ModeBypass:
		return m.applyBypassDeferred()
	default:
		return nil
	}
}

// AddRoutes dynamically adds routes for additional CIDRs after the
// initial Apply/ApplyDeferred. This is used for CIDRs fetched from URLs
// after the tunnel is established. Uses batch execution.
func (m *Manager) AddRoutes(cidrs []string) error {
	if len(cidrs) == 0 {
		return nil
	}

	// Merge CIDRs to reduce route count.
	merged := mergeCIDRs(cidrs)
	logRouteMerge(len(cidrs), len(merged))

	// Split CIDRs by family.
	var v4, v6 []string
	for _, cidr := range merged {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if isIPv6CIDR(cidr) {
			v6 = append(v6, cidr)
		} else {
			v4 = append(v4, cidr)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string

	if m.cfg.Mode == ModeBypass {
		if len(v4) > 0 && m.originalGateway != "" {
			if err := addRoutesViaBatch(v4, m.originalGateway); err != nil {
				errs = append(errs, err.Error())
			} else {
				m.bypassRoutes = append(m.bypassRoutes, v4...)
			}
		}
		if len(v6) > 0 && m.originalGateway6 != "" {
			if err := addRoutesVia6Batch(v6, m.originalGateway6); err != nil {
				errs = append(errs, err.Error())
			} else {
				m.bypassRoutes6 = append(m.bypassRoutes6, v6...)
			}
		}
	} else {
		if len(v4) > 0 {
			if err := addRoutesBatch(v4, m.cfg.InterfaceName); err != nil {
				errs = append(errs, err.Error())
			} else {
				m.addedRoutes = append(m.addedRoutes, v4...)
			}
		}
		if len(v6) > 0 {
			if err := addRoutes6Batch(v6, m.cfg.InterfaceName); err != nil {
				errs = append(errs, err.Error())
			} else {
				m.addedRoutes6 = append(m.addedRoutes6, v6...)
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("add routes errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// HasOriginalGateway reports whether the manager captured an original
// default gateway (v4 or v6) during Apply.
func (m *Manager) HasOriginalGateway() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.originalGateway != "" || m.originalGateway6 != ""
}

// OriginalGatewayV4 returns the captured IPv4 default gateway, if any.
func (m *Manager) OriginalGatewayV4() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.originalGateway
}

// OriginalGatewayV6 returns the captured IPv6 default gateway, if any.
func (m *Manager) OriginalGatewayV6() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.originalGateway6
}

// Cleanup removes all routes that were added by Apply, ApplyDeferred,
// or AddRoutes. Safe to call concurrently with ApplyDeferred.
func (m *Manager) Cleanup() error {
	m.mu.Lock()
	// Snapshot all route lists under the lock, then clear them.
	addedRoutes := m.addedRoutes
	addedRoutes6 := m.addedRoutes6
	bypassRoutes := m.bypassRoutes
	bypassRoutes6 := m.bypassRoutes6
	serverBypass := m.serverBypass
	serverBypass6 := m.serverBypass6
	originalGateway := m.originalGateway
	originalGateway6 := m.originalGateway6
	serverIP := m.serverIP
	serverIP6 := m.serverIP6
	m.addedRoutes = nil
	m.addedRoutes6 = nil
	m.bypassRoutes = nil
	m.bypassRoutes6 = nil
	m.serverBypass = false
	m.serverBypass6 = false
	m.mu.Unlock()

	var errs []string

	// Delete user CIDR routes. Use batch for large lists.
	if len(addedRoutes) > 3 {
		if err := deleteRoutesBatch(addedRoutes, m.cfg.InterfaceName); err != nil {
			errs = append(errs, err.Error())
		}
	} else {
		for _, r := range addedRoutes {
			if err := deleteRoute(r, m.cfg.InterfaceName); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(addedRoutes6) > 3 {
		if err := deleteRoutes6Batch(addedRoutes6, m.cfg.InterfaceName); err != nil {
			errs = append(errs, err.Error())
		}
	} else {
		for _, r := range addedRoutes6 {
			if err := deleteRoute6(r, m.cfg.InterfaceName); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}

	// Delete bypass routes via gateway.
	if len(bypassRoutes) > 3 {
		if err := deleteRoutesViaBatch(bypassRoutes, originalGateway); err != nil {
			errs = append(errs, err.Error())
		}
	} else {
		for _, r := range bypassRoutes {
			if err := deleteRouteVia(r, originalGateway); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(bypassRoutes6) > 3 {
		if err := deleteRoutesVia6Batch(bypassRoutes6, originalGateway6); err != nil {
			errs = append(errs, err.Error())
		}
	} else {
		for _, r := range bypassRoutes6 {
			if err := deleteRouteVia6(r, originalGateway6); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}

	// Delete server bypass routes.
	if serverBypass && serverIP != "" {
		if err := deleteRouteVia(serverIP+"/32", originalGateway); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if serverBypass6 && serverIP6 != "" && originalGateway6 != "" {
		if err := deleteRouteVia6(serverIP6+"/128", originalGateway6); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// captureGatewaysAndBypass resolves the server host and adds bypass
// routes for the server's public IPs via the original gateway. This
// is shared by applyFull and applyBypassEssential.
func (m *Manager) captureGatewaysAndBypass() error {
	// Capture the current default gateways before modifying routes.
	gw, err := defaultGateway()
	if err != nil {
		return fmt.Errorf("get default gateway: %w", err)
	}
	m.originalGateway = gw

	// IPv6 default gateway is best-effort.
	gw6, _ := defaultGateway6()
	m.originalGateway6 = gw6

	// Resolve server host to v4 + v6 IPs for bypass routes.
	v4, v6, err := resolveHosts(m.cfg.ServerHost)
	if err != nil {
		return fmt.Errorf("resolve server host %s: %w", m.cfg.ServerHost, err)
	}
	m.serverIP = v4
	m.serverIP6 = v6

	// Bypass: server's public IPv4 via the original gateway.
	if v4 != "" {
		bypassSpec := v4 + "/32"
		if err := addRouteVia(bypassSpec, gw); err != nil {
			return fmt.Errorf("add server bypass route: %w", err)
		}
		m.serverBypass = true
	}

	// Bypass: server's public IPv6 via the original v6 gateway.
	if v6 != "" && gw6 != "" {
		bypassSpec := v6 + "/128"
		if err := addRouteVia6(bypassSpec, gw6); err != nil {
			m.serverBypass6 = false
		} else {
			m.serverBypass6 = true
		}
	}
	return nil
}

func (m *Manager) applyFull() error {
	if err := m.captureGatewaysAndBypass(); err != nil {
		return err
	}

	// Two /1 routes cover the entire IPv4 space.
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := addRoute(cidr, m.cfg.InterfaceName); err != nil {
			return fmt.Errorf("add route %s: %w", cidr, err)
		}
		m.addedRoutes = append(m.addedRoutes, cidr)
	}

	// IPv6 full tunnel cover routes.
	if m.cfg.VPNIP6 != "" {
		for _, cidr := range []string{"::/1", "8000::/1"} {
			if err := addRoute6(cidr, m.cfg.InterfaceName); err != nil {
				return fmt.Errorf("add route6 %s: %w", cidr, err)
			}
			m.addedRoutes6 = append(m.addedRoutes6, cidr)
		}
	}
	return nil
}

// applyProxyEssential does nothing - proxy mode has no essential
// routes. All user CIDR routes are deferred.
func (m *Manager) applyProxyEssential() error {
	return nil
}

// applyProxyDeferred adds all user CIDR routes via the TUN interface
// using batch execution. CIDRs are merged to minimize route count.
func (m *Manager) applyProxyDeferred() error {
	merged := mergeCIDRs(m.cfg.CIDRs)
	logRouteMerge(len(m.cfg.CIDRs), len(merged))

	var v4, v6 []string
	for _, cidr := range merged {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if isIPv6CIDR(cidr) {
			v6 = append(v6, cidr)
		} else {
			v4 = append(v4, cidr)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	if len(v4) > 0 {
		if err := addRoutesBatch(v4, m.cfg.InterfaceName); err != nil {
			errs = append(errs, err.Error())
		} else {
			m.addedRoutes = append(m.addedRoutes, v4...)
		}
	}
	if len(v6) > 0 {
		if err := addRoutes6Batch(v6, m.cfg.InterfaceName); err != nil {
			errs = append(errs, err.Error())
		} else {
			m.addedRoutes6 = append(m.addedRoutes6, v6...)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("proxy deferred: %s", strings.Join(errs, "; "))
	}
	return nil
}

// applyBypassEssential adds server bypass + /1 cover routes (full
// tunnel effect). User bypass CIDRs are deferred to allow the "ready"
// signal to be sent promptly.
func (m *Manager) applyBypassEssential() error {
	if err := m.captureGatewaysAndBypass(); err != nil {
		return err
	}

	// Two /1 routes cover the entire IPv4 space (full tunnel).
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := addRoute(cidr, m.cfg.InterfaceName); err != nil {
			return fmt.Errorf("add route %s: %w", cidr, err)
		}
		m.addedRoutes = append(m.addedRoutes, cidr)
	}

	// IPv6 full tunnel cover routes.
	if m.cfg.VPNIP6 != "" {
		for _, cidr := range []string{"::/1", "8000::/1"} {
			if err := addRoute6(cidr, m.cfg.InterfaceName); err != nil {
				return fmt.Errorf("add route6 %s: %w", cidr, err)
			}
			m.addedRoutes6 = append(m.addedRoutes6, cidr)
		}
	}
	return nil
}

// applyBypassDeferred adds user bypass CIDR routes via the original
// gateway using batch execution. CIDRs are merged to minimize route
// count.
func (m *Manager) applyBypassDeferred() error {
	merged := mergeCIDRs(m.cfg.CIDRs)
	logRouteMerge(len(m.cfg.CIDRs), len(merged))

	var v4, v6 []string
	for _, cidr := range merged {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if isIPv6CIDR(cidr) {
			if m.originalGateway6 != "" {
				v6 = append(v6, cidr)
			}
		} else {
			v4 = append(v4, cidr)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	if len(v4) > 0 {
		if err := addRoutesViaBatch(v4, m.originalGateway); err != nil {
			errs = append(errs, err.Error())
		} else {
			m.bypassRoutes = append(m.bypassRoutes, v4...)
		}
	}
	if len(v6) > 0 {
		if err := addRoutesVia6Batch(v6, m.originalGateway6); err != nil {
			errs = append(errs, err.Error())
		} else {
			m.bypassRoutes6 = append(m.bypassRoutes6, v6...)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bypass deferred: %s", strings.Join(errs, "; "))
	}
	return nil
}

// resolveHosts resolves a hostname to its first IPv4 and IPv6 addresses.
// If host is already an IP literal, it is returned directly. The DNS
// lookup is bounded to 5 seconds.
func resolveHosts(host string) (v4, v6 string, err error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return ip.String(), "", nil
		}
		return "", ip.String(), nil
	}
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return "", "", fmt.Errorf("lookup %s: %w", host, err)
	}
	for _, addr := range addrs {
		if v4 == "" && addr.IP.To4() != nil {
			v4 = addr.IP.String()
		} else if v6 == "" && addr.IP.To4() == nil {
			v6 = addr.IP.String()
		}
	}
	if v4 == "" && v6 == "" {
		return "", "", fmt.Errorf("lookup %s: no addresses", host)
	}
	return v4, v6, nil
}

// isIPv6CIDR reports whether the CIDR string is IPv6.
func isIPv6CIDR(cidr string) bool {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipNet.IP.To4() == nil
}

// logRouteMerge logs CIDR merge statistics if any reduction occurred.
func logRouteMerge(before, after int) {
	if before > after {
		log.L().Info("CIDR merge", "before", before, "after", after, "reduced", before-after)
	}
}
