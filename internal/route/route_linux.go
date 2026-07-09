//go:build !darwin && !windows

package route

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

func addRoute(cidr, iface string) error {
	return runCmd("ip", "route", "add", cidr, "dev", iface)
}

func deleteRoute(cidr, iface string) error {
	return runCmd("ip", "route", "del", cidr, "dev", iface)
}

func addRouteVia(cidr, gateway string) error {
	return runCmd("ip", "route", "add", cidr, "via", gateway)
}

func deleteRouteVia(cidr, gateway string) error {
	return runCmd("ip", "route", "del", cidr, "via", gateway)
}

// --- IPv6 variants ---

func addRoute6(cidr, iface string) error {
	return runCmd("ip", "-6", "route", "add", cidr, "dev", iface)
}

func deleteRoute6(cidr, iface string) error {
	return runCmd("ip", "-6", "route", "del", cidr, "dev", iface)
}

func addRouteVia6(cidr, gateway string) error {
	return runCmd("ip", "-6", "route", "add", cidr, "via", gateway)
}

func deleteRouteVia6(cidr, gateway string) error {
	return runCmd("ip", "-6", "route", "del", cidr, "via", gateway)
}

func defaultGateway() (string, error) {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", err
	}
	return parseViaGateway(string(out))
}

func defaultGateway6() (string, error) {
	out, err := exec.Command("ip", "-6", "route", "show", "default").Output()
	if err != nil {
		return "", err
	}
	return parseViaGateway(string(out))
}

func parseViaGateway(out string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("no default gateway found")
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// --- Batch functions ---

// runBatchScript writes commands to temporary shell scripts and
// executes them in parallel (up to 4 concurrent scripts).
func runBatchScript(lines []string) error {
	if len(lines) == 0 {
		return nil
	}

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
		lines = append(lines, fmt.Sprintf("ip route add %s dev %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func deleteRoutesBatch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("ip route del %s dev %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func addRoutes6Batch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("ip -6 route add %s dev %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func deleteRoutes6Batch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("ip -6 route del %s dev %s", cidr, iface))
	}
	return runBatchScript(lines)
}

func addRoutesViaBatch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("ip route add %s via %s", cidr, gateway))
	}
	return runBatchScript(lines)
}

func deleteRoutesViaBatch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("ip route del %s via %s", cidr, gateway))
	}
	return runBatchScript(lines)
}

func addRoutesVia6Batch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("ip -6 route add %s via %s", cidr, gateway))
	}
	return runBatchScript(lines)
}

func deleteRoutesVia6Batch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		lines = append(lines, fmt.Sprintf("ip -6 route del %s via %s", cidr, gateway))
	}
	return runBatchScript(lines)
}
