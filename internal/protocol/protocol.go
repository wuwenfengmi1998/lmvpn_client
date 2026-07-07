// Package protocol defines the LMVPN wire protocol structures and
// constants. These mirror the server's definitions in
// lmvpn_server/internal/vpn/{protocol,auth,tunnel}.go exactly.
//
// Wire format:
//   - Text frames   = UTF-8 JSON control messages (with "type" field)
//   - Binary frames = raw IPv4/IPv6 packets (no encapsulation header)
package protocol

import "time"

// Message type strings exchanged over the WebSocket.
const (
	TypeAuth    = "auth"     // C→S: password credentials
	TypeAuthOK  = "auth_ok"  // S→C: password auth succeeded
	TypeAuthErr = "auth_err" // S→C: auth failed (then close)
	TypeInit    = "init"     // S→C: tunnel parameters
	TypeReady   = "ready"    // C→S: TUN configured, data plane ready
	TypeError   = "error"    // S→C: handshake failure (then close)
)

// Timeout and limit constants matching the server (tunnel.go:16-23).
const (
	ReadTimeout     = 60 * time.Second // post-ready read deadline
	WriteTimeout    = 10 * time.Second // per-write deadline
	ReadyTimeout    = 30 * time.Second // client must send ready within this
	PingPeriod      = 30 * time.Second // server ping interval
	MaxMessageSize  = 1 << 20          // 1 MiB max WebSocket message
	MaxConnsPerUser = 3                // per-user concurrent connection cap
	TokenExpiry     = 24 * time.Hour   // JWT validity
)

// InitMessage is sent by the server after auth + pre-checks pass.
// (server: protocol.go:3-10, tunnel.go:134-145)
type InitMessage struct {
	Type      string `json:"type"`
	IP        string `json:"ip"`                   // assigned client IPv4 (dotted-quad)
	Prefix    int    `json:"prefix"`               // IPv4 subnet prefix length (e.g. 24)
	MTU       int    `json:"mtu"`                  // TUN device MTU (e.g. 1420)
	ServerIP  string `json:"server_ip"`            // server's tunnel IPv4 (peer/gateway)
	IP6       string `json:"ip6,omitempty"`        // assigned client IPv6 (only when server has Subnet6)
	Prefix6   int    `json:"prefix6,omitempty"`    // IPv6 subnet prefix length
	ServerIP6 string `json:"server_ip6,omitempty"` // server's tunnel IPv6
}

// ControlMessage is the generic text control message.
// (server: protocol.go:11-13)
type ControlMessage struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// AuthMessage is sent by the client for password authentication.
// (server: auth.go:17-21)
type AuthMessage struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResponse is the server's reply to an AuthMessage.
// (server: auth.go:23-26)
type AuthResponse struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// IsError reports whether a control message type indicates failure.
func IsError(msgType string) bool {
	return msgType == TypeAuthErr || msgType == TypeError
}
