//go:build darwin

package route

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// addRoute adds a route via a network interface (macOS route command).
//
//	route add -inet <cidr> -interface <iface>
func addRoute(cidr, iface string) error {
	return runRoute("add", "-inet", cidr, "-interface", iface)
}

// deleteRoute removes a route via a network interface.
func deleteRoute(cidr, iface string) error {
	return runRoute("delete", "-inet", cidr, "-interface", iface)
}

// addRouteVia adds a route via a gateway IP.
func addRouteVia(cidr, gateway string) error {
	return runRoute("add", "-inet", cidr, gateway)
}

// deleteRouteVia removes a route via a gateway IP.
func deleteRouteVia(cidr, gateway string) error {
	return runRoute("delete", "-inet", cidr, gateway)
}

// --- IPv6 variants ---

func addRoute6(cidr, iface string) error {
	return runRoute("add", "-inet6", cidr, "-interface", iface)
}

func deleteRoute6(cidr, iface string) error {
	return runRoute("delete", "-inet6", cidr, "-interface", iface)
}

func addRouteVia6(cidr, gateway string) error {
	return runRoute("add", "-inet6", cidr, gateway)
}

func deleteRouteVia6(cidr, gateway string) error {
	return runRoute("delete", "-inet6", cidr, gateway)
}

// defaultGateway returns the current IPv4 default gateway IP.
func defaultGateway() (string, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return "", err
	}
	return parseGateway(string(out))
}

// defaultGateway6 returns the current IPv6 default gateway IP.
func defaultGateway6() (string, error) {
	out, err := exec.Command("route", "-n", "get", "-inet6", "default").Output()
	if err != nil {
		return "", err
	}
	return parseGateway(string(out))
}

func parseGateway(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gateway:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				gw := strings.TrimSpace(parts[1])
				if gw != "" {
					return gw, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no default gateway found")
}

func runRoute(args ...string) error {
	cmd := exec.Command("route", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("route %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// --- Batch functions ---

// runBatchScript writes commands to temporary shell scripts and
// executes them in parallel (up to 4 concurrent scripts). Each line
// uses "|| true" so that individual route failures don't abort the
// batch.
func runBatchScript(lines []string) error {
	if len(lines) == 0 {
		return nil
	}

	// Split lines into up to 4 chunks for parallel execution.
	const maxChunks = 4
	chunkSize := (len(lines) + maxChunks - 1) / maxChunks
	var chunks [][]string
	for i := 0; i < len(lines); i += chunkSize {
		end := i + chunkSize
		if end > len(lines) {
			end = len(lines)
		}
		chunks = append(chunks, lines[i:end])
	}

	var wg sync.WaitGroup
	errs := make([]error, len(chunks))
	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, c []string) {
			defer wg.Done()
			errs[idx] = runSingleScript(c)
		}(i, chunk)
	}
	wg.Wait()

	var errStrs []string
	for _, e := range errs {
		if e != nil {
			errStrs = append(errStrs, e.Error())
		}
	}
	if len(errStrs) > 0 {
		return fmt.Errorf("batch script: %s", strings.Join(errStrs, "; "))
	}
	return nil
}

func runSingleScript(lines []string) error {
	f, err := os.CreateTemp("", "lmvpn-routes-*.sh")
	if err != nil {
		return fmt.Errorf("create batch script: %w", err)
	}
	tmpFile := f.Name()
	defer os.Remove(tmpFile)

	for _, line := range lines {
		if _, err := fmt.Fprintln(f, line, "|| true"); err != nil {
			f.Close()
			return fmt.Errorf("write batch script: %w", err)
		}
	}
	f.Close()

	cmd := exec.Command("sh", tmpFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("batch: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func addRoutesBatch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route add -inet %s -interface %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func deleteRoutesBatch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route delete -inet %s -interface %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func addRoutes6Batch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route add -inet6 %s -interface %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func deleteRoutes6Batch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route delete -inet6 %s -interface %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func addRoutesViaBatch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route add -inet %s %s", cidr, gateway))
	}
	return runBatchScript(lines)
}

func deleteRoutesViaBatch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route delete -inet %s %s", cidr, gateway))
	}
	return runBatchScript(lines)
}

func addRoutesVia6Batch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route add -inet6 %s %s", cidr, gateway))
	}
	return runBatchScript(lines)
}

func deleteRoutesVia6Batch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("route delete -inet6 %s %s", cidr, gateway))
	}
	return runBatchScript(lines)
}
