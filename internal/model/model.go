// Package model defines the data structures persisted in SQLite and
// exchanged between application layers.
package model

import "time"

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
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	ServerURL       string     `json:"server_url"`        // e.g. wss://vpn.example.com/ws
	Username        string     `json:"username"`
	AuthMode        AuthMode   `json:"auth_mode"`
	RoutingMode     RoutingMode `json:"routing_mode"`
	CustomCIDRs     string     `json:"custom_cidrs"`      // comma-separated, for RoutingCustom
	MTUOverride     int        `json:"mtu_override"`       // 0 = use server MTU
	AutoConnect     bool       `json:"auto_connect"`
	CreatedAt       time.Time  `json:"created_at"`
	LastConnectedAt *time.Time `json:"last_connected_at"`
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
	ID         int64             `json:"id"`
	ProfileID  int64             `json:"profile_id"`
	StartedAt  time.Time         `json:"started_at"`
	EndedAt    *time.Time        `json:"ended_at"`
	AssignedIP string            `json:"assigned_ip"`
	RxBytes    int64             `json:"rx_bytes"`
	TxBytes    int64             `json:"tx_bytes"`
	Status     ConnectionStatus  `json:"status"`
	ErrorMsg   string            `json:"error_msg"`
}
