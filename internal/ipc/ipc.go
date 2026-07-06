// Package ipc implements the communication protocol between the GUI
// process (user) and the privileged daemon (root) over a unix domain
// socket.
//
// Protocol: newline-delimited JSON. Each message is one JSON object
// followed by '\n'.
//
//   GUI → daemon:  Request  (start, stop, shutdown, stats)
//   daemon → GUI:  Event    (state, stats, error)
package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"lmvpn/internal/paths"
	"lmvpn/internal/route"
	"lmvpn/internal/stats"
)

// Command types sent from GUI to daemon.
const (
	CmdStart    = "start"
	CmdStop     = "stop"
	CmdShutdown = "shutdown"
	CmdStats    = "stats"
)

// Event types sent from daemon to GUI.
const (
	EvState = "state"
	EvStats = "stats"
	EvError = "error"
)

// Request is a command from the GUI to the daemon.
type Request struct {
	Cmd    string        `json:"cmd"`
	Config *ClientConfig `json:"config,omitempty"`
}

// ClientConfig is the session configuration sent over IPC. It mirrors
// vpn.SessionConfig but is kept separate to avoid importing the vpn
// package (which needs root-only TUN) into the GUI.
type ClientConfig struct {
	ServerURL   string   `json:"server_url"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Token       string   `json:"token"`
	AuthMode    string   `json:"auth_mode"`
	RoutingMode string   `json:"routing_mode"`
	CustomCIDRs []string `json:"custom_cidrs"`
	MTUOverride int      `json:"mtu_override"`
}

// Event is a notification from the daemon to the GUI.
type Event struct {
	Event   string          `json:"event"`
	State   string          `json:"state,omitempty"`
	Stats   *stats.Snapshot `json:"stats,omitempty"`
	Message string          `json:"message,omitempty"`
}

// --- Wire helpers ---

func writeMsg(w io.Writer, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func readMsg(r *bufio.Reader, v interface{}) error {
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(line), v)
}

// --- Server (daemon side) ---

// Server listens on the IPC socket and manages connected clients.
type Server struct {
	listener net.Listener
	mu       sync.Mutex
	clients  map[net.Conn]bool
}

// NewServer creates (but does not start) the IPC server. It removes
// any stale socket file first.
func NewServer() (*Server, error) {
	_ = os.Remove(paths.IPCSocketPath())
	l, err := net.Listen("unix", paths.IPCSocketPath())
	if err != nil {
		return nil, fmt.Errorf("listen ipc: %w", err)
	}
	// Mode 0660 so group members (admin) can connect.
	_ = os.Chmod(paths.IPCSocketPath(), 0o660)
	return &Server{listener: l, clients: make(map[net.Conn]bool)}, nil
}

// Accept runs the accept loop. Each connection is handled in a
// goroutine; the handler callback receives each Request.
func (s *Server) Accept(handle func(net.Conn, Request)) error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.clients[conn] = true
		s.mu.Unlock()
		go s.serve(conn, handle)
	}
}

func (s *Server) serve(conn net.Conn, handle func(net.Conn, Request)) {
	defer func() {
		s.mu.Lock()
		delete(s.clients, conn)
		s.mu.Unlock()
		conn.Close()
	}()
	r := bufio.NewReader(conn)
	for {
		var req Request
		if err := readMsg(r, &req); err != nil {
			return
		}
		handle(conn, req)
	}
}

// Broadcast sends an event to all connected clients.
func (s *Server) Broadcast(ev Event) {
	data, _ := json.Marshal(ev)
	data = append(data, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		_, _ = c.Write(data)
	}
}

// Close stops the server and closes all connections.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		c.Close()
	}
	s.clients = nil
	if s.listener != nil {
		s.listener.Close()
	}
}

// --- Client (GUI side) ---

// Client connects to the daemon's IPC socket.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
}

// Dial connects to the daemon.
func Dial() (*Client, error) {
	conn, err := net.Dial("unix", paths.IPCSocketPath())
	if err != nil {
		return nil, fmt.Errorf("dial daemon: %w", err)
	}
	return &Client{conn: conn, r: bufio.NewReader(conn)}, nil
}

// Send sends a request to the daemon.
func (c *Client) Send(req Request) error {
	return writeMsg(c.conn, &req)
}

// Recv reads the next event from the daemon. Blocks until an event is
// available or the connection breaks.
func (c *Client) Recv() (Event, error) {
	var ev Event
	if err := readMsg(c.r, &ev); err != nil {
		return ev, err
	}
	return ev, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Response is a simple ack/error reply to a command.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// WriteOK sends a success response to a connection.
func WriteOK(conn net.Conn) error {
	return writeMsg(conn, Response{OK: true})
}

// WriteErr sends an error response to a connection.
func WriteErr(conn net.Conn, msg string) error {
	return writeMsg(conn, Response{OK: false, Error: msg})
}

// SendStart is a convenience helper for sending a start command.
func SendStart(c *Client, cfg ClientConfig) error {
	return c.Send(Request{Cmd: CmdStart, Config: &cfg})
}

// SendStop is a convenience helper for sending a stop command.
func SendStop(c *Client) error {
	return c.Send(Request{Cmd: CmdStop})
}

// SendShutdown is a convenience helper for sending a shutdown command.
func SendShutdown(c *Client) error {
	return c.Send(Request{Cmd: CmdShutdown})
}

// RoutingModeFromIPC converts an IPC routing mode string to route.Mode.
func RoutingModeFromIPC(s string) route.Mode {
	switch s {
	case "full":
		return route.ModeFull
	case "split":
		return route.ModeSplit
	case "custom":
		return route.ModeCustom
	default:
		return route.ModeFull
	}
}
