// Package model defines the data structures persisted in SQLite and
// exchanged between application layers.
package model

import (
	"fmt"
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
	RoutingFull   RoutingMode = "full"   // 0.0.0.0/0 via tunnel
	RoutingSplit  RoutingMode = "split"  // only VPN subnet via tunnel
	RoutingCustom RoutingMode = "custom" // user-specified CIDRs
)

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
	CustomCIDRs     string      `json:"custom_cidrs"` // comma-separated, for RoutingCustom
	MTUOverride     int         `json:"mtu_override"` // 0 = use server MTU
	AutoConnect     bool        `json:"auto_connect"`
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

// GetServerIPList parses ServerIPs into a string slice.
func (p *ServerProfile) GetServerIPList() []string {
	if p.ServerIPs == "" {
		return nil
	}
	parts := strings.Split(p.ServerIPs, ",")
	var out []string
	for _, part := range parts {
		s := strings.TrimSpace(part)
		if s != "" {
			out = append(out, s)
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
