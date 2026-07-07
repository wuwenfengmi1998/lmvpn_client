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
type Stats struct {
	RxBytes     atomic.Int64
	TxBytes     atomic.Int64
	ConnectedAt atomic.Int64 // unix timestamp, 0 = not connected
	state       atomic.Value // State
	assignedIP  atomic.Value // string (IPv4)
	assignedIP6 atomic.Value // string (IPv6, may be empty)
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
type Snapshot struct {
	RxBytes     int64
	TxBytes     int64
	ConnectedAt time.Time
	AssignedIP  string
	AssignedIP6 string
	State       State
	Uptime      time.Duration
}

// Snapshot returns a point-in-time copy of the statistics.
func (s *Stats) Snapshot() Snapshot {
	snap := Snapshot{
		RxBytes:     s.RxBytes.Load(),
		TxBytes:     s.TxBytes.Load(),
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
