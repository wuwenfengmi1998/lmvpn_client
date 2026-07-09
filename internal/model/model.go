// Package model defines the data structures persisted in SQLite and
// exchanged between application layers.
package model

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// AuthMode selects how the client authenticates to a server.
type AuthMode string

const (
	AuthModeBoth     AuthMode = "both"     // try JWT, fall back to password
	AuthModeJWT      AuthMode = "jwt"      // HTTP login then ?token=
	AuthModePassword AuthMode = "password" // {type:auth} first message
)

// RoutingMode selects which traffic goes through the VPN tunnel.
type RoutingMode string

const (
	RoutingFull   RoutingMode = "full"   // 全隧道: all traffic via tunnel
	RoutingProxy  RoutingMode = "proxy"  // 代理CIDR: only specified CIDRs via tunnel
	RoutingBypass RoutingMode = "bypass" // 绕过CIDR: all traffic via tunnel except specified CIDRs
)

// FetchTiming specifies when a CIDR URL source is fetched.
type FetchTiming string

const (
	FetchBefore FetchTiming = "before" // before proxy: fetched via direct connection before routing is applied
	FetchAfter  FetchTiming = "after"  // after proxy: fetched via the tunnel after the data plane is up
)

// CIDRURLSource describes a URL that provides a CIDR list. The list
// is fetched at FetchTiming and merged into the routing configuration.
type CIDRURLSource struct {
	URL         string      `json:"url"`
	FetchTiming FetchTiming `json:"fetch_timing"` // "before" or "after"
}

// ServerProfile is a saved VPN server configuration.
type ServerProfile struct {
	ID              int64       `json:"id"`
	Name            string      `json:"name"`
	Protocol        string      `json:"protocol"`   // "wss" (default) or "ws"
	Host            string      `json:"host"`       // hostname for SNI, e.g. vpn.example.com
	ServerIPs       string      `json:"server_ips"` // comma-separated CDN IPs, first used by default
	Port            int         `json:"port"`       // default 443
	Path            string      `json:"path"`       // default "/ws"
	Username        string      `json:"username"`
	AuthMode        AuthMode    `json:"auth_mode"`
	RoutingMode     RoutingMode `json:"routing_mode"`
	CIDRV4          string      `json:"cidr_v4"`      // comma-separated static IPv4 CIDRs
	CIDRV6          string      `json:"cidr_v6"`      // comma-separated static IPv6 CIDRs
	CIDRV4URLs      string      `json:"cidr_v4_urls"` // JSON array of CIDRURLSource for IPv4
	CIDRV6URLs      string      `json:"cidr_v6_urls"` // JSON array of CIDRURLSource for IPv6
	MTUOverride     int         `json:"mtu_override"` // 0 = use server MTU
	AutoConnect     bool        `json:"auto_connect"`
	TLSCACert       string      `json:"tls_ca_cert"`      // inline CA certificate PEM (wss only)
	TLSCAPath       string      `json:"tls_ca_path"`      // path to CA certificate file (wss only)
	TLSInsecure     bool        `json:"tls_insecure"`     // skip certificate verification (wss only)
	TLSPinnedHash   string      `json:"tls_pinned_hash"`  // SHA-256 fingerprint of server leaf cert (wss only)
	CreatedAt       time.Time   `json:"created_at"`
	LastConnectedAt *time.Time  `json:"last_connected_at"`
}

// BuildServerURL constructs the WebSocket URL from the profile fields.
// If ip is provided, it is used as the host portion instead of Host
// (for CDN edge IP connections).
// Default ports are omitted from the URL (443 for wss, 80 for ws).
func (p *ServerProfile) BuildServerURL(ip ...string) string {
	protocol := p.Protocol
	if protocol == "" {
		protocol = "wss"
	}

	host := p.Host
	if len(ip) > 0 && ip[0] != "" {
		host = ip[0]
	}

	port := p.Port
	if port == 0 {
		port = 443
	}

	path := p.Path
	if path == "" {
		path = "/ws"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	isDefaultPort := (protocol == "wss" && port == 443) || (protocol == "ws" && port == 80)

	if isDefaultPort {
		return fmt.Sprintf("%s://%s%s", protocol, host, path)
	}
	return fmt.Sprintf("%s://%s:%d%s", protocol, host, port, path)
}

// ValidateServerIPs parses ServerIPs, returning valid IP addresses
// and any invalid entries (for UI error reporting).
func (p *ServerProfile) ValidateServerIPs() (valid []string, invalid []string) {
	if p.ServerIPs == "" {
		return nil, nil
	}
	for _, part := range strings.Split(p.ServerIPs, ",") {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		if net.ParseIP(s) != nil {
			valid = append(valid, s)
		} else {
			invalid = append(invalid, s)
		}
	}
	return
}

// GetServerIPList returns only valid IP addresses from ServerIPs,
// silently filtering out any malformed entries.
func (p *ServerProfile) GetServerIPList() []string {
	valid, _ := p.ValidateServerIPs()
	return valid
}

// ParseCIDRURLs decodes a JSON-encoded CIDRURLSource array. Returns an
// empty slice if the string is empty or unparseable.
func ParseCIDRURLs(jsonStr string) []CIDRURLSource {
	if jsonStr == "" {
		return nil
	}
	var sources []CIDRURLSource
	if err := json.Unmarshal([]byte(jsonStr), &sources); err != nil {
		return nil
	}
	return sources
}

// SplitCIDRs splits a comma-separated CIDR string into a slice,
// trimming whitespace from each entry and skipping empty ones.
func SplitCIDRs(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		c := strings.TrimSpace(part)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// ConnectionStatus records the outcome of a connection attempt.
type ConnectionStatus string

const (
	StatusConnected    ConnectionStatus = "connected"
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusError        ConnectionStatus = "error"
)

// ConnectionLog records a single VPN session.
type ConnectionLog struct {
	ID          int64            `json:"id"`
	ProfileID   int64            `json:"profile_id"`
	StartedAt   time.Time        `json:"started_at"`
	EndedAt     *time.Time       `json:"ended_at"`
	AssignedIP  string           `json:"assigned_ip"`
	AssignedIP6 string           `json:"assigned_ip6"`
	RxBytes     int64            `json:"rx_bytes"`
	TxBytes     int64            `json:"tx_bytes"`
	Status      ConnectionStatus `json:"status"`
	ErrorMsg    string           `json:"error_msg"`
}
