// Package route manages VPN routing on the client. It supports three
// modes:
//
//   - Full tunnel:   all traffic (0.0.0.0/0) via the TUN interface,
//                    with a bypass route for the server's public IP so
//                    the WebSocket connection stays on the physical NIC
//   - Split tunnel:  only the VPN virtual subnet via the TUN interface
//   - Custom:        user-specified CIDRs via the TUN interface
//
// All routes are tracked so they can be cleanly removed on disconnect.
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
	VPNIP         string   // assigned tunnel IP, e.g. "192.168.77.5"
	VPNPrefix     int      // subnet prefix, e.g. 24
	ServerHost    string   // server hostname/IP (for full-tunnel bypass)
	CustomCIDRs   []string // for ModeCustom
}

// Manager applies and removes routes. It tracks all added routes so
// they can be cleaned up deterministically.
type Manager struct {
	cfg             Config
	addedRoutes     []string // route specs added, for deletion
	serverBypass    bool
	originalGateway string
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
	if m.serverBypass {
		if err := m.deleteServerBypass(); err != nil {
			errs = append(errs, err.Error())
		}
		m.serverBypass = false
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) applyFull() error {
	// Capture the current default gateway before modifying routes.
	gw, err := defaultGateway()
	if err != nil {
		return fmt.Errorf("get default gateway: %w", err)
	}
	m.originalGateway = gw

	// Resolve server host to an IP for the bypass route.
	serverIP, err := resolveHost(m.cfg.ServerHost)
	if err != nil {
		return fmt.Errorf("resolve server host %s: %w", m.cfg.ServerHost, err)
	}

	// Bypass: server's public IP via the original gateway (so the WS
	// connection doesn't loop through the tunnel).
	bypassSpec := serverIP + "/32"
	if err := addRouteVia(bypassSpec, gw); err != nil {
		return fmt.Errorf("add server bypass route: %w", err)
	}
	m.serverBypass = true

	// Two /1 routes cover the entire IPv4 space and are more specific
	// than the default route (0.0.0.0/0), so they take precedence
	// without removing the original default.
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if err := addRoute(cidr, m.cfg.InterfaceName); err != nil {
			return fmt.Errorf("add route %s: %w", cidr, err)
		}
		m.addedRoutes = append(m.addedRoutes, cidr)
	}
	return nil
}

func (m *Manager) applySplit() error {
	subnet := vpnSubnet(m.cfg.VPNIP, m.cfg.VPNPrefix)
	if err := addRoute(subnet, m.cfg.InterfaceName); err != nil {
		return fmt.Errorf("add split route %s: %w", subnet, err)
	}
	m.addedRoutes = append(m.addedRoutes, subnet)
	return nil
}

func (m *Manager) applyCustom() error {
	for _, cidr := range m.cfg.CustomCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		if err := addRoute(cidr, m.cfg.InterfaceName); err != nil {
			return fmt.Errorf("add custom route %s: %w", cidr, err)
		}
		m.addedRoutes = append(m.addedRoutes, cidr)
	}
	return nil
}

func (m *Manager) deleteServerBypass() error {
	serverIP, err := resolveHost(m.cfg.ServerHost)
	if err != nil {
		return nil // best-effort
	}
	return deleteRouteVia(serverIP+"/32", m.originalGateway)
}

// vpnSubnet computes the network CIDR from an IP and prefix.
func vpnSubnet(ipStr string, prefix int) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr + "/" + fmt.Sprint(prefix)
	}
	mask := net.CIDRMask(prefix, 32)
	network := ip.Mask(mask)
	return fmt.Sprintf("%s/%d", network.String(), prefix)
}

// resolveHost resolves a hostname to an IP address. If already an IP,
// returns it directly.
func resolveHost(host string) (string, error) {
	if net.ParseIP(host) != nil {
		return host, nil
	}
	// Strip port if present.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("lookup %s: %w", host, err)
	}
	return ips[0].String(), nil
}
