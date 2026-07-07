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
	RxBytesV4   atomic.Int64
	RxBytesV6   atomic.Int64
	TxBytesV4   atomic.Int64
	TxBytesV6   atomic.Int64
	ConnectedAt atomic.Int64 // unix timestamp, 0 = not connected
	state       atomic.Value // State
	assignedIP  atomic.Value // string (IPv4)
	assignedIP6 atomic.Value // string (IPv6, may be empty)
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
	return s
}

// SetState updates the current state atomically.
func (s *Stats) SetState(st State) { s.state.Store(st) }

// State returns the current state.
func (s *Stats) State() State { return s.state.Load().(State) }

// SetConnected marks the session as connected, recording the time and
// assigned IP addresses. ip6 may be empty for an IPv4-only server.
func (s *Stats) SetConnected(ip, ip6 string) {
	s.ConnectedAt.Store(time.Now().Unix())
	s.assignedIP.Store(ip)
	s.assignedIP6.Store(ip6)
	s.state.Store(StateConnected)
}

// SetDisconnected clears the connection metadata.
func (s *Stats) SetDisconnected() {
	s.ConnectedAt.Store(0)
	s.assignedIP.Store("")
	s.assignedIP6.Store("")
	s.state.Store(StateDisconnected)
}

// AssignedIP returns the server-assigned tunnel IPv4.
func (s *Stats) AssignedIP() string { return s.assignedIP.Load().(string) }

// AssignedIP6 returns the server-assigned tunnel IPv6 (may be empty).
func (s *Stats) AssignedIP6() string { return s.assignedIP6.Load().(string) }

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
	State       State
	Uptime      time.Duration
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
		State:       s.State(),
	}
	ts := s.ConnectedAt.Load()
	if ts > 0 {
		snap.ConnectedAt = time.Unix(ts, 0)
		snap.Uptime = time.Since(snap.ConnectedAt)
	}
	return snap
}
