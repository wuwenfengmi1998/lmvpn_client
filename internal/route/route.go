// Package route manages VPN routing on the client. It supports three
// modes:
//
//   - Full tunnel:   all traffic (0.0.0.0/0 and ::/0) via the TUN
//     interface, with bypass routes for the server's
//     public IP (v4 and v6) so the WebSocket connection
//     stays on the physical NIC
//   - Split tunnel:  only the VPN virtual subnet (v4 and v6) via TUN
//   - Custom:        user-specified CIDRs via the TUN interface
//
// IPv6 routes are applied automatically when the server assigned an
// IPv6 address (Config.VPNIP6 != ""). All routes are tracked so they
// can be cleanly removed on disconnect.
package route

import (
	"fmt"
	"net"
	"strings"
)

// Mode selects which traffic goes through the VPN tunnel.
type Mode string

const (
	ModeFull   Mode = "full"
	ModeSplit  Mode = "split"
	ModeCustom Mode = "custom"
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
	CustomCIDRs   []string // for ModeCustom
}

// Manager applies and removes routes. It tracks all added routes so
// they can be cleaned up deterministically.
type Manager struct {
	cfg              Config
	addedRoutes      []string // v4 route specs added, for deletion
	addedRoutes6     []string // v6 route specs added, for deletion
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

// Apply adds routes according to the configured mode.
func (m *Manager) Apply() error {
	switch m.cfg.Mode {
	case ModeFull:
		return m.applyFull()
	case ModeSplit:
		return m.applySplit()
	case ModeCustom:
		return m.applyCustom()
	default:
		return fmt.Errorf("unknown routing mode: %s", m.cfg.Mode)
	}
}

// Cleanup removes all routes that were added by Apply.
func (m *Manager) Cleanup() error {
	var errs []string
	for _, r := range m.addedRoutes {
		if err := deleteRoute(r, m.cfg.InterfaceName); err != nil {
			errs = append(errs, err.Error())
		}
	}
	m.addedRoutes = nil
	for _, r := range m.addedRoutes6 {
		if err := deleteRoute6(r, m.cfg.InterfaceName); err != nil {
			errs = append(errs, err.Error())
		}
	}
	m.addedRoutes6 = nil
	if m.serverBypass {
		if err := m.deleteServerBypass(); err != nil {
			errs = append(errs, err.Error())
		}
		m.serverBypass = false
	}
	if m.serverBypass6 {
		if err := m.deleteServerBypass6(); err != nil {
			errs = append(errs, err.Error())
		}
		m.serverBypass6 = false
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) applyFull() error {
	// Capture the current default gateways before modifying routes.
	gw, err := defaultGateway()
	if err != nil {
		return fmt.Errorf("get default gateway: %w", err)
	}
	m.originalGateway = gw

	// IPv6 default gateway is best-effort: it may be absent on v4-only
	// networks, in which case v6 bypass/routing is skipped.
	gw6, _ := defaultGateway6()
	m.originalGateway6 = gw6

	// Resolve server host to v4 + v6 IPs for bypass routes.
	v4, v6, err := resolveHosts(m.cfg.ServerHost)
	if err != nil {
		return fmt.Errorf("resolve server host %s: %w", m.cfg.ServerHost, err)
	}
	m.serverIP = v4
	m.serverIP6 = v6

	// Bypass: server's public IPv4 via the original gateway (so the WS
	// connection doesn't loop through the tunnel).
	if v4 != "" {
		bypassSpec := v4 + "/32"
		if err := addRouteVia(bypassSpec, gw); err != nil {
			return fmt.Errorf("add server bypass route: %w", err)
		}
		m.serverBypass = true
	}

	// Bypass: server's public IPv6 via the original v6 gateway.
	// Non-fatal: if this fails, full-tunnel routes are still added.
	if v6 != "" && gw6 != "" {
		bypassSpec := v6 + "/128"
		if err := addRouteVia6(bypassSpec, gw6); err != nil {
			m.serverBypass6 = false
		} else {
			m.serverBypass6 = true
		}
	}

	// Two /1 routes cover the entire IPv4 space and are more specific
	// than the default route (0.0.0.0/0), so they take precedence
	// without removing the original default.
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := addRoute(cidr, m.cfg.InterfaceName); err != nil {
			return fmt.Errorf("add route %s: %w", cidr, err)
		}
		m.addedRoutes = append(m.addedRoutes, cidr)
	}

	// IPv6 full tunnel: ::/1 + 8000::/1 cover the entire IPv6 space,
	// more specific than ::/0. Only applied when the server assigned
	// an IPv6 address.
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

func (m *Manager) applySplit() error {
	subnet := vpnSubnet(m.cfg.VPNIP, m.cfg.VPNPrefix, false)
	if err := addRoute(subnet, m.cfg.InterfaceName); err != nil {
		return fmt.Errorf("add split route %s: %w", subnet, err)
	}
	m.addedRoutes = append(m.addedRoutes, subnet)

	if m.cfg.VPNIP6 != "" {
		subnet6 := vpnSubnet(m.cfg.VPNIP6, m.cfg.VPNPrefix6, true)
		if err := addRoute6(subnet6, m.cfg.InterfaceName); err != nil {
			return fmt.Errorf("add split route6 %s: %w", subnet6, err)
		}
		m.addedRoutes6 = append(m.addedRoutes6, subnet6)
	}
	return nil
}

func (m *Manager) applyCustom() error {
	for _, cidr := range m.cfg.CustomCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if isIPv6CIDR(cidr) {
			if err := addRoute6(cidr, m.cfg.InterfaceName); err != nil {
				return fmt.Errorf("add custom route6 %s: %w", cidr, err)
			}
			m.addedRoutes6 = append(m.addedRoutes6, cidr)
		} else {
			if err := addRoute(cidr, m.cfg.InterfaceName); err != nil {
				return fmt.Errorf("add custom route %s: %w", cidr, err)
			}
			m.addedRoutes = append(m.addedRoutes, cidr)
		}
	}
	return nil
}

func (m *Manager) deleteServerBypass() error {
	if m.serverIP == "" {
		return nil
	}
	return deleteRouteVia(m.serverIP+"/32", m.originalGateway)
}

func (m *Manager) deleteServerBypass6() error {
	if m.serverIP6 == "" || m.originalGateway6 == "" {
		return nil
	}
	return deleteRouteVia6(m.serverIP6+"/128", m.originalGateway6)
}

// vpnSubnet computes the network CIDR from an IP and prefix.
func vpnSubnet(ipStr string, prefix int, ipv6 bool) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr + "/" + fmt.Sprint(prefix)
	}
	bits := 32
	if ipv6 {
		bits = 128
	}
	mask := net.CIDRMask(prefix, bits)
	network := ip.Mask(mask)
	return fmt.Sprintf("%s/%d", network.String(), prefix)
}

// resolveHosts resolves a hostname to its first IPv4 and IPv6 addresses.
// If host is already an IP literal, it is returned directly. Either
// result may be empty if no address of that family is available.
func resolveHosts(host string) (v4, v6 string, err error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return ip.String(), "", nil
		}
		return "", ip.String(), nil
	}
	// Strip port if present.
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", "", fmt.Errorf("lookup %s: %w", host, err)
	}
	for _, ip := range ips {
		if v4 == "" && ip.To4() != nil {
			v4 = ip.String()
		} else if v6 == "" && ip.To4() == nil {
			v6 = ip.String()
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
