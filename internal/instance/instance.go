// Package instance implements single-instance enforcement for the GUI
// process. The first instance to start acquires the lock by listening
// on a dedicated endpoint; subsequent instances dial the endpoint
// (signalling "bring to front") and exit immediately.
//
// The lock is self-cleaning: a crashed process releases the listener
// automatically (the kernel closes the socket). On unix a stale socket
// file may remain; Acquire probes it with a dial before removing.
package instance

import (
	"errors"
	"fmt"
	"net"
	"os"

	"lmvpn/internal/paths"
)

// ErrAlreadyRunning is returned by Acquire when another GUI instance
// is already running. The caller should exit silently.
var ErrAlreadyRunning = errors.New("another instance is already running")

// Acquire attempts to become the sole GUI instance. On success it
// returns a channel that receives a value every time another instance
// signals (the caller should bring its window to the front). The
// listener is closed when the channel is closed, which happens never
// during normal operation - the process simply exits and the OS
// releases the socket.
//
// On failure it returns ErrAlreadyRunning (the caller should exit) or
// another error (the caller may continue without single-instance
// protection, logging the issue).
func Acquire() (<-chan struct{}, error) {
	netType := paths.GUILockNetwork()
	addr := paths.GUILockAddress()

	l, err := net.Listen(netType, addr)
	if err == nil {
		ch := make(chan struct{}, 1)
		go acceptLoop(l, ch)
		return ch, nil
	}

	// Listen failed - check whether another instance is alive.
	if c, derr := net.Dial(netType, addr); derr == nil {
		c.Close()
		return nil, ErrAlreadyRunning
	}

	// Nobody is listening. On unix this is likely a stale socket file
	// left by a crashed process; remove it and retry once.
	if netType == "unix" {
		_ = os.Remove(addr)
		l, err = net.Listen(netType, addr)
		if err == nil {
			ch := make(chan struct{}, 1)
			go acceptLoop(l, ch)
			return ch, nil
		}
	}

	return nil, fmt.Errorf("acquire instance lock: %w", err)
}

// acceptLoop accepts connections from second instances. Each connection
// is a "focus" signal; the handler closes it immediately and notifies
// the caller via ch.
func acceptLoop(l net.Listener, ch chan<- struct{}) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		conn.Close()
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
