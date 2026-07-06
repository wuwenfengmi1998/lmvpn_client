// Package transport implements the LMVPN WebSocket client transport.
//
// It handles the full connection lifecycle:
//  1. Dial the WebSocket (no Origin header, per server's allow-empty rule)
//  2. Authenticate — JWT via ?token= query param, or password via first
//     text message {type:auth}
//  3. Receive the {type:init} message with tunnel parameters
//  4. Call the OnInit callback (TUN configuration)
//  5. Send {type:ready}
//  6. Provide ReadPacket/WritePacket for raw IP binary frames
//
// Reconnection with exponential backoff is handled by the vpn package's
// SessionManager, not here. This type represents a single connection.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"lmvpn/internal/protocol"

	"github.com/gorilla/websocket"
)

// HandshakeConfig configures a single connection attempt.
type HandshakeConfig struct {
	ServerURL string // e.g. wss://vpn.example.com/ws
	Token     string // JWT; if non-empty, used via ?token= (method A)
	Username  string // for password auth (method B), or fallback
	Password  string // for password auth (method B), or fallback
	OnInit    func(protocol.InitMessage) error // configure TUN; nil = auto-ready
}

// Conn is an established VPN tunnel connection.
type Conn struct {
	ws     *websocket.Conn
	init   protocol.InitMessage
	writeMu sync.Mutex
	closed  bool
	mu      sync.Mutex
}

// Connect dials, authenticates, and completes the tunnel handshake.
// It blocks until the connection is ready for data transfer or an
// error occurs.
func Connect(ctx context.Context, cfg HandshakeConfig) (*Conn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		ReadBufferSize:   4096, // match server (handler.go:17)
		WriteBufferSize:  4096, // match server (handler.go:18)
	}

	// Build URL: append ?token= for JWT auth.
	url := cfg.ServerURL
	if cfg.Token != "" {
		url = appendQuery(url, "token", cfg.Token)
	}

	// Omit Origin header (server allows empty Origin for non-browser
	// clients — handler.go:19-29).
	header := http.Header{}
	header.Set("Origin", "")

	ws, resp, err := dialer.DialContext(ctx, url, header)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	defer resp.Body.Close()

	ws.SetReadLimit(protocol.MaxMessageSize)

	conn := &Conn{ws: ws}

	// Step 2: authenticate (only for password mode; JWT is validated
	// during the WS upgrade).
	if cfg.Token == "" {
		if err := conn.passwordAuth(cfg.Username, cfg.Password); err != nil {
			ws.Close()
			return nil, err
		}
	}

	// Step 3: receive init (or error/auth_err).
	initMsg, err := conn.readInit()
	if err != nil {
		ws.Close()
		return nil, err
	}
	conn.init = initMsg

	// Step 4: configure TUN via callback.
	if cfg.OnInit != nil {
		if err := cfg.OnInit(initMsg); err != nil {
			ws.Close()
			return nil, fmt.Errorf("tun configure: %w", err)
		}
	}

	// Step 5: send ready.
	if err := conn.sendReady(); err != nil {
		ws.Close()
		return nil, fmt.Errorf("send ready: %w", err)
	}

	// Step 6: set up keepalive — reset read deadline on each server
	// ping, and auto-respond with pong (allowed via WriteControl in
	// handler — see gorilla/websocket docs).
	ws.SetReadDeadline(time.Now().Add(protocol.ReadTimeout))
	ws.SetPingHandler(func(appData string) error {
		ws.SetReadDeadline(time.Now().Add(protocol.ReadTimeout))
		return ws.WriteControl(websocket.PongMessage, []byte(appData),
			time.Now().Add(protocol.WriteTimeout))
	})

	return conn, nil
}

// passwordAuth sends the {type:auth} message and waits for auth_ok.
func (c *Conn) passwordAuth(username, password string) error {
	c.ws.SetReadDeadline(time.Now().Add(protocol.ReadTimeout))
	msg := protocol.AuthMessage{
		Type:     protocol.TypeAuth,
		Username: username,
		Password: password,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if err := c.writeText(data); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	// Read auth response.
	_, respData, err := c.ws.ReadMessage()
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}
	var resp protocol.AuthResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("parse auth response: %w", err)
	}
	if resp.Type == protocol.TypeAuthErr {
		return &AuthError{Message: resp.Message}
	}
	if resp.Type != protocol.TypeAuthOK {
		return fmt.Errorf("unexpected auth response type: %s", resp.Type)
	}
	return nil
}

// readInit reads the init message (or error/auth_err).
func (c *Conn) readInit() (protocol.InitMessage, error) {
	c.ws.SetReadDeadline(time.Now().Add(protocol.ReadTimeout))
	_, data, err := c.ws.ReadMessage()
	if err != nil {
		return protocol.InitMessage{}, fmt.Errorf("read init: %w", err)
	}

	// Try init first (most common path).
	var initMsg protocol.InitMessage
	if err := json.Unmarshal(data, &initMsg); err == nil && initMsg.Type == protocol.TypeInit {
		return initMsg, nil
	}

	// Otherwise it's an error or auth_err control message.
	var ctrl protocol.ControlMessage
	if err := json.Unmarshal(data, &ctrl); err != nil {
		return protocol.InitMessage{}, fmt.Errorf("parse init/error: %w (raw: %s)", err, data)
	}
	if ctrl.Type == protocol.TypeError || ctrl.Type == protocol.TypeAuthErr {
		return protocol.InitMessage{}, &ServerError{Type: ctrl.Type, Message: ctrl.Message}
	}
	return protocol.InitMessage{}, fmt.Errorf("unexpected message type: %s", ctrl.Type)
}

// sendReady sends the {type:ready} control message.
func (c *Conn) sendReady() error {
	data, _ := json.Marshal(protocol.ControlMessage{Type: protocol.TypeReady})
	return c.writeText(data)
}

// writeText sends a text frame with the write deadline.
func (c *Conn) writeText(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(protocol.WriteTimeout))
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

// ReadPacket reads the next binary IP packet from the tunnel.
// It blocks until a packet is available or the connection breaks.
func (c *Conn) ReadPacket() ([]byte, error) {
	for {
		msgType, data, err := c.ws.ReadMessage()
		if err != nil {
			return nil, err
		}
		if msgType == websocket.BinaryMessage {
			return data, nil
		}
		// Text messages after handshake are ignored (server shouldn't
		// send any, but be resilient).
	}
}

// WritePacket sends a raw IP packet as a binary frame.
func (c *Conn) WritePacket(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(protocol.WriteTimeout))
	return c.ws.WriteMessage(websocket.BinaryMessage, data)
}

// Init returns the init message received during handshake.
func (c *Conn) Init() protocol.InitMessage { return c.init }

// AssignedIP returns the IP assigned by the server.
func (c *Conn) AssignedIP() string { return c.init.IP }

// Close terminates the connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return c.ws.Close()
}

// AuthError indicates authentication failure (auth_err from server).
type AuthError struct{ Message string }

func (e *AuthError) Error() string { return "auth failed: " + e.Message }

// ServerError indicates a server-side rejection (error or auth_err).
type ServerError struct {
	Type    string
	Message string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("server %s: %s", e.Type, e.Message)
}

// appendQuery appends a key=value parameter to a URL.
func appendQuery(url, key, value string) string {
	sep := "?"
	if contains(url, "?") {
		sep = "&"
	}
	return url + sep + key + "=" + value
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
