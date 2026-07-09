package transport

import (
	"context"
	"fmt"
	"net"
	"time"
)

// NewRaceDialer returns a dial function suitable for use as a
// websocket.Dialer.NetDialContext or http.Transport.DialContext.
//
// When the host in addr is a domain name, it resolves all A/AAAA
// records and dials them concurrently, returning the first successful
// connection (true parallel racing, not Happy Eyeballs staggered start).
//
// preference controls which address families participate:
//   - "auto": race all resolved addresses (v4 + v6)
//   - "v4":   only IPv4 addresses
//   - "v6":   only IPv6 addresses
//
// When the host is an IP literal (CDN failover or direct IP mode), it
// dials directly without racing — there is only one target.
func NewRaceDialer(preference string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split host port: %w", err)
		}

		// IP literal: dial directly, no racing.
		if ip := net.ParseIP(host); ip != nil {
			d := &net.Dialer{}
			return d.DialContext(ctx, network, addr)
		}

		// Domain name: resolve and race.
		ips, err := resolveAndFilter(ctx, host, preference)
		if err != nil {
			return nil, err
		}

		if len(ips) == 1 {
			d := &net.Dialer{}
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		}

		return raceDial(ctx, network, ips, port)
	}
}

// resolveAndFilter resolves a hostname and filters by preference.
func resolveAndFilter(ctx context.Context, host, preference string) ([]string, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	addrs, err := net.DefaultResolver.LookupIPAddr(resolveCtx, host)
	if err != nil {
		return nil, fmt.Errorf("lookup %s: %w", host, err)
	}

	var ips []string
	for _, a := range addrs {
		isV4 := a.IP.To4() != nil
		switch preference {
		case "v4":
			if isV4 {
				ips = append(ips, a.IP.String())
			}
		case "v6":
			if !isV4 {
				ips = append(ips, a.IP.String())
			}
		default: // "auto" or unspecified
			ips = append(ips, a.IP.String())
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("lookup %s: no addresses for preference %q", host, preference)
	}
	return ips, nil
}

// raceDial dials all IPs concurrently and returns the first successful
// connection. Losing connections are closed and their errors discarded.
func raceDial(ctx context.Context, network string, ips []string, port string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	resultCh := make(chan result, len(ips))

	for _, ip := range ips {
		go func(target string) {
			d := &net.Dialer{}
			c, err := d.DialContext(raceCtx, network, net.JoinHostPort(target, port))
			select {
			case resultCh <- result{conn: c, err: err}:
			case <-raceCtx.Done():
				if c != nil {
					c.Close()
				}
			}
		}(ip)
	}

	var firstErr error
	for i := 0; i < len(ips); i++ {
		r := <-resultCh
		if r.err == nil {
			// Winner. Cancel remaining dialers; drain and close
			// any late successful connections.
			cancel()
			for j := i + 1; j < len(ips); j++ {
				if late := <-resultCh; late.conn != nil {
					late.conn.Close()
				}
			}
			return r.conn, nil
		}
		if firstErr == nil {
			firstErr = r.err
		}
	}

	if firstErr == nil {
		firstErr = fmt.Errorf("all dial attempts failed")
	}
	return nil, firstErr
}
