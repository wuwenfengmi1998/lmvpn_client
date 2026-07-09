// Package stats provides atomic counters for VPN session statistics.
package stats

import (
	"sync/atomic"
	"time"
)

// State represents the current VPN session state.
type State string

const (
	StateDisconnected State = "disconnected"
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateReconnecting State = "reconnecting"
	StateError        State = "error"
)

// Stats holds live session statistics. Counters are atomic for
// lock-free reads from the UI/IPC layer.
//
// Counters are split by IP address family (v4/v6) at the recording
// site via AddRx/AddTx, which inspect the IP version nibble. The
// combined RxBytes/TxBytes are derived as the sum of the per-family
// counters in Snapshot, so callers that only need the total still
// work.
type Stats struct {
	RxBytesV4    atomic.Int64
	RxBytesV6    atomic.Int64
	TxBytesV4    atomic.Int64
	TxBytesV6    atomic.Int64
	ConnectedAt  atomic.Int64 // unix timestamp, 0 = not connected
	state        atomic.Value // State
	assignedIP   atomic.Value // string (IPv4)
	assignedIP6  atomic.Value // string (IPv6, may be empty)
	serverHost   atomic.Value // string (server hostname or IP from URL)
	connectedIP  atomic.Value // string (actual remote IP of the connection)
	routeLoading atomic.Bool  // true while deferred routes are being applied
	connectStep  atomic.Value // string (human-readable connection step)
	cidrError    atomic.Value // string (CIDR fetch error message, empty = no error)
}

// AddRx records a downloaded packet (WebSocket → TUN) of length n,
// routing the bytes to the v4 or v6 counter based on the IP version
// nibble of the packet. Packets too short to contain a version byte
// or with an unknown version are ignored.
func (s *Stats) AddRx(p []byte) {
	if len(p) == 0 {
		return
	}
	n := int64(len(p))
	switch p[0] >> 4 {
	case 4:
		s.RxBytesV4.Add(n)
	case 6:
		s.RxBytesV6.Add(n)
	}
}

// AddTx records an uploaded packet (TUN → WebSocket) of length n,
// routing the bytes to the v4 or v6 counter based on the IP version
// nibble of the packet. Packets too short to contain a version byte
// or with an unknown version are ignored.
func (s *Stats) AddTx(p []byte) {
	if len(p) == 0 {
		return
	}
	n := int64(len(p))
	switch p[0] >> 4 {
	case 4:
		s.TxBytesV4.Add(n)
	case 6:
		s.TxBytesV6.Add(n)
	}
}

// New creates a Stats instance initialised to the disconnected state.
func New() *Stats {
	s := &Stats{}
	s.state.Store(StateDisconnected)
	s.assignedIP.Store("")
	s.assignedIP6.Store("")
	s.serverHost.Store("")
	s.connectedIP.Store("")
	s.connectStep.Store("")
	s.cidrError.Store("")
	return s
}

// SetState updates the current state atomically.
func (s *Stats) SetState(st State) { s.state.Store(st) }

// State returns the current state.
func (s *Stats) State() State { return s.state.Load().(State) }

// SetConnected marks the session as connected, recording the time,
// assigned IP addresses, and server connection info. ip6 may be empty
// for an IPv4-only server. serverHost is the hostname/IP from the
// server URL; connectedIP is the actual remote IP of the connection.
func (s *Stats) SetConnected(ip, ip6, serverHost, connectedIP string) {
	s.ConnectedAt.Store(time.Now().Unix())
	s.assignedIP.Store(ip)
	s.assignedIP6.Store(ip6)
	s.serverHost.Store(serverHost)
	s.connectedIP.Store(connectedIP)
	s.state.Store(StateConnected)
}

// SetDisconnected clears the connection metadata.
func (s *Stats) SetDisconnected() {
	s.ConnectedAt.Store(0)
	s.assignedIP.Store("")
	s.assignedIP6.Store("")
	s.serverHost.Store("")
	s.connectedIP.Store("")
	s.state.Store(StateDisconnected)
	s.routeLoading.Store(false)
	s.connectStep.Store("")
	s.cidrError.Store("")
}

// SetRouteLoading sets whether deferred routes are currently being
// applied (e.g. thousands of CIDR bypass routes being added in the
// background after the tunnel is up).
func (s *Stats) SetRouteLoading(loading bool) { s.routeLoading.Store(loading) }

// RouteLoading returns whether deferred routes are being applied.
func (s *Stats) RouteLoading() bool { return s.routeLoading.Load() }

// SetConnectStep sets a human-readable description of the current
// connection step (e.g. "fetching CIDR lists"). Empty clears it.
func (s *Stats) SetConnectStep(step string) { s.connectStep.Store(step) }

// ConnectStep returns the current connection step description.
func (s *Stats) ConnectStep() string {
	v := s.connectStep.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// SetCIDRError sets a CIDR fetch error message (empty = no error).
func (s *Stats) SetCIDRError(msg string) { s.cidrError.Store(msg) }

// CIDRError returns the current CIDR fetch error message.
func (s *Stats) CIDRError() string {
	v := s.cidrError.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// AssignedIP returns the server-assigned tunnel IPv4.
func (s *Stats) AssignedIP() string { return s.assignedIP.Load().(string) }

// AssignedIP6 returns the server-assigned tunnel IPv6 (may be empty).
func (s *Stats) AssignedIP6() string { return s.assignedIP6.Load().(string) }

// ServerHost returns the server hostname/IP from the connection URL.
func (s *Stats) ServerHost() string { return s.serverHost.Load().(string) }

// ConnectedIP returns the actual remote IP of the connection.
func (s *Stats) ConnectedIP() string { return s.connectedIP.Load().(string) }

// Snapshot returns a point-in-time copy of all counters.
//
// Per-family byte counters are read directly. The combined RxBytes/
// TxBytes are derived as v4+v6. Speed fields (RxSpeedV4 etc.) are
// left zero here; the SessionManager fills them in by computing
// per-tick deltas with EWMA smoothing.
type Snapshot struct {
	RxBytesV4 int64
	RxBytesV6 int64
	TxBytesV4 int64
	TxBytesV6 int64
	RxBytes   int64 // combined (v4+v6)
	TxBytes   int64 // combined (v4+v6)

	// Speeds in bytes/sec (EWMA-smoothed). The UI converts to bits/sec.
	RxSpeedV4 int64
	TxSpeedV4 int64
	RxSpeedV6 int64
	TxSpeedV6 int64
	RxSpeed   int64 // combined
	TxSpeed   int64 // combined

	ConnectedAt time.Time
	AssignedIP  string
	AssignedIP6 string
	ServerHost  string `json:"server_host,omitempty"`  // server hostname/IP from URL
	ConnectedIP string `json:"connected_ip,omitempty"` // actual remote IP of the connection
	State       State
	Uptime      time.Duration

	// Routing info (filled by SessionManager.reportStats).
	RoutingMode  string `json:"routing_mode,omitempty"` // "full", "proxy", "bypass"
	CIDRV4Total  int    `json:"cidr_v4_total,omitempty"`
	CIDRV4Hits   int    `json:"cidr_v4_hits,omitempty"`
	CIDRV6Total  int    `json:"cidr_v6_total,omitempty"`
	CIDRV6Hits   int    `json:"cidr_v6_hits,omitempty"`
	RouteLoading bool   `json:"route_loading,omitempty"` // deferred routes being applied
	ConnectStep  string `json:"connect_step,omitempty"`  // current connection step
	CIDRError    string `json:"cidr_error,omitempty"`    // CIDR fetch error message
}

// Snapshot returns a point-in-time copy of the statistics.
func (s *Stats) Snapshot() Snapshot {
	rxv4 := s.RxBytesV4.Load()
	rxv6 := s.RxBytesV6.Load()
	txv4 := s.TxBytesV4.Load()
	txv6 := s.TxBytesV6.Load()
	snap := Snapshot{
		RxBytesV4:   rxv4,
		RxBytesV6:   rxv6,
		TxBytesV4:   txv4,
		TxBytesV6:   txv6,
		RxBytes:     rxv4 + rxv6,
		TxBytes:     txv4 + txv6,
		AssignedIP:  s.AssignedIP(),
		AssignedIP6: s.AssignedIP6(),
		ServerHost:  s.ServerHost(),
		ConnectedIP: s.ConnectedIP(),
		State:       s.State(),
	}
	ts := s.ConnectedAt.Load()
	if ts > 0 {
		snap.ConnectedAt = time.Unix(ts, 0)
		snap.Uptime = time.Since(snap.ConnectedAt)
	}
	snap.RouteLoading = s.routeLoading.Load()
	snap.ConnectStep = s.ConnectStep()
	snap.CIDRError = s.CIDRError()
	return snap
}
