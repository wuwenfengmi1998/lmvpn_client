// Package cidrsource fetches CIDR lists from URLs. CIDR lists can be
// fetched before routing is applied (via a direct connection) or after
// the tunnel is established (via the tunnel itself).
//
// The expected format is one CIDR per line. Empty lines and lines
// starting with '#' are treated as comments and skipped. Invalid CIDR
// entries are silently ignored (with a debug log entry).
package cidrsource

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"lmvpn/internal/log"
	"lmvpn/internal/model"
)

// fetchTimeout is the total timeout for all before-proxy fetches
// combined. It must be well under the server's 30s ReadyTimeout since
// before-proxy fetching happens before the WS handshake begins.
const fetchTimeout = 5 * time.Second

// FetchBeforeProxy fetches all CIDR URL sources with FetchTiming ==
// "before". These are fetched via the system's direct connection
// (before routing is applied), so no special proxy handling is needed.
// The context allows cancellation (e.g. session teardown).
//
// All matching sources are fetched concurrently with a shared 5s
// deadline so a single slow URL does not block the entire session.
func FetchBeforeProxy(ctx context.Context, sources []model.CIDRURLSource) ([]string, error) {
	return fetchSources(ctx, sources, model.FetchBefore)
}

// FetchAfterProxy fetches all CIDR URL sources with FetchTiming ==
// "after". These are fetched via the tunnel after the data plane is up.
// The HTTP client uses the default dialer, which respects the system
// routing table - so when full-tunnel or bypass routes are in effect,
// the request goes through the TUN interface.
//
// Sources are fetched concurrently with a 15s deadline (the tunnel is
// already up, so there is no ReadyTimeout pressure).
func FetchAfterProxy(ctx context.Context, sources []model.CIDRURLSource) ([]string, error) {
	return fetchSources(ctx, sources, model.FetchAfter)
}

// fetchSources fetches all sources matching the given timing
// concurrently, bounded by a shared deadline derived from fetchTimeout
// (for "before") or 15s (for "after").
func fetchSources(ctx context.Context, sources []model.CIDRURLSource, timing model.FetchTiming) ([]string, error) {
	// Collect matching sources.
	var matching []model.CIDRURLSource
	for _, src := range sources {
		if src.FetchTiming == timing {
			matching = append(matching, src)
		}
	}
	if len(matching) == 0 {
		return nil, nil
	}

	// Shared deadline for all fetches.
	deadline := fetchTimeout
	if timing == model.FetchAfter {
		deadline = 15 * time.Second
	}
	fetchCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	type result struct {
		cidrs []string
		err   error
		url   string
	}
	results := make(chan result, len(matching))

	for _, src := range matching {
		go func(s model.CIDRURLSource) {
			cidrs, err := fetchOne(fetchCtx, s.URL)
			results <- result{cidrs: cidrs, err: err, url: s.URL}
		}(src)
	}

	var allCIDRs []string
	var errs []string
	for range matching {
		r := <-results
		if r.err != nil {
			label := "before-proxy"
			if timing == model.FetchAfter {
				label = "after-proxy"
			}
			log.L().Error("fetch "+label+" CIDR list failed (continuing)",
				"url", r.url, "error", r.err)
			errs = append(errs, fmt.Sprintf("%s: %v", r.url, r.err))
			continue
		}
		label := "before-proxy"
		if timing == model.FetchAfter {
			label = "after-proxy"
		}
		log.L().Info("fetched "+label+" CIDR list",
			"url", r.url, "count", len(r.cidrs))
		allCIDRs = append(allCIDRs, r.cidrs...)
	}

	if len(allCIDRs) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("all CIDR URL fetches failed: %s", strings.Join(errs, "; "))
	}
	return allCIDRs, nil
}

// fetchOne fetches a single URL and parses the response as a CIDR list.
func fetchOne(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return ParseCIDRList(string(body)), nil
}

// ParseCIDRList parses text into a list of valid CIDR strings. Each
// line is treated as a separate CIDR entry. Empty lines and lines
// starting with '#' are skipped. Invalid CIDR entries are silently
// ignored.
func ParseCIDRList(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		c := strings.TrimSpace(line)
		if c == "" || strings.HasPrefix(c, "#") {
			continue
		}
		_, _, err := net.ParseCIDR(c)
		if err != nil {
			log.L().Debug("ignoring invalid CIDR entry", "cidr", c)
			continue
		}
		out = append(out, c)
	}
	return out
}

// ClassifyCIDRs splits a list of CIDR strings into IPv4 and IPv6 lists.
func ClassifyCIDRs(cidrs []string) (v4, v6 []string) {
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.IP.To4() != nil {
			v4 = append(v4, cidr)
		} else {
			v6 = append(v6, cidr)
		}
	}
	return v4, v6
}
