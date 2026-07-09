package transport

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// TestRaceDialNoDeadlock is a regression test for a select race
// condition that caused raceDial to deadlock ~50% of the time when
// multiple IPs were raced and the first succeeded. The bug was a
// select with two simultaneously-ready cases (send result vs
// <-raceCtx.Done()); Go's random selection could skip the send,
// leaving the drain loop blocked forever.
//
// We run many iterations because the bug was probabilistic.
func TestRaceDialNoDeadlock(t *testing.T) {
	const iterations = 200

	for n := 0; n < iterations; n++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		// Accept both connections in background (both IPs dial the
		// same listener since raceDial uses a single port).
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()

		port := portOf(ln.Addr())
		ips := []string{"127.0.0.1", "127.0.0.1"}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, err := raceDial(ctx, "tcp", ips, port)
		cancel()
		if err == nil && conn != nil {
			conn.Close()
		}

		ln.Close()

		// If we get here without the 5s timeout, the iteration passed.
		// A deadlock would trigger the test-wide 60s timeout.
	}

	// If we reach here, no iteration deadlocked.
}

// TestRaceDialAllFail verifies that when all dials fail, raceDial
// returns an error instead of blocking.
func TestRaceDialAllFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Port 1: connection refused on most systems.
	ips := []string{"127.0.0.1", "127.0.0.1"}
	_, err := raceDial(ctx, "tcp", ips, "1")
	if err == nil {
		t.Fatal("expected error when all dials fail")
	}
}

// TestRaceDialSingleIP verifies the single-IP path still works.
func TestRaceDialSingleIP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, _ := ln.Accept()
		if c != nil {
			c.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := raceDial(ctx, "tcp", []string{"127.0.0.1"}, portOf(ln.Addr()))
	if err != nil {
		t.Fatalf("raceDial single IP: %v", err)
	}
	conn.Close()
	wg.Wait()
}

// TestRaceDialContextCancelled ensures raceDial returns promptly when
// the parent context is cancelled while dials are in flight.
func TestRaceDialContextCancelled(t *testing.T) {
	// Dial a non-routable address so the dial hangs until context
	// cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := raceDial(ctx, "tcp", []string{"10.255.255.1", "10.255.255.2"}, "80")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on cancelled context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("raceDial did not return after context cancellation")
	}
}

// portOf extracts the port from a net.Addr.
func portOf(addr net.Addr) string {
	_, port, _ := net.SplitHostPort(addr.String())
	return port
}
