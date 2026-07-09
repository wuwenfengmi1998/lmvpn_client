//go:build windows

package route

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// cidrToNetworkMask splits a CIDR string into network address and
// dotted-decimal mask, as required by the Windows `route` command.
func cidrToNetworkMask(cidr string) (network, mask string, err error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	return ipNet.IP.String(), net.IP(ipNet.Mask).String(), nil
}

// ifaceIndex resolves a Windows interface name to its index.
func ifaceIndex(name string) (int, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("resolve interface %s: %w", name, err)
	}
	return ifc.Index, nil
}

// --- IPv4 ---

func addRoute(cidr, iface string) error {
	network, mask, err := cidrToNetworkMask(cidr)
	if err != nil {
		return err
	}
	idx, err := ifaceIndex(iface)
	if err != nil {
		return err
	}
	return runCmd("route", "add", network, "mask", mask, "0.0.0.0",
		"if", fmt.Sprintf("%d", idx), "metric", "1")
}

func deleteRoute(cidr, iface string) error {
	network, mask, err := cidrToNetworkMask(cidr)
	if err != nil {
		return err
	}
	return runCmd("route", "delete", network, "mask", mask)
}

func addRouteVia(cidr, gateway string) error {
	network, mask, err := cidrToNetworkMask(cidr)
	if err != nil {
		return err
	}
	return runCmd("route", "add", network, "mask", mask, gateway, "metric", "1")
}

func deleteRouteVia(cidr, gateway string) error {
	network, mask, err := cidrToNetworkMask(cidr)
	if err != nil {
		return err
	}
	return runCmd("route", "delete", network, "mask", mask, gateway)
}

// --- IPv6 ---

// cidrToV6Prefix splits an IPv6 CIDR into network and prefix length.
func cidrToV6Prefix(cidr string) (network, prefix string, err error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", err
	}
	ones, _ := ipNet.Mask.Size()
	return ipNet.IP.String(), fmt.Sprintf("%d", ones), nil
}

func addRoute6(cidr, iface string) error {
	network, prefix, err := cidrToV6Prefix(cidr)
	if err != nil {
		return err
	}
	idx, err := ifaceIndex(iface)
	if err != nil {
		return err
	}
	return runCmd("route", "-6", "add", network+"/"+prefix, "::",
		"if", fmt.Sprintf("%d", idx), "metric", "1")
}

func deleteRoute6(cidr, iface string) error {
	network, prefix, err := cidrToV6Prefix(cidr)
	if err != nil {
		return err
	}
	return runCmd("route", "-6", "delete", network+"/"+prefix, "::")
}

func addRouteVia6(cidr, gateway string) error {
	network, prefix, err := cidrToV6Prefix(cidr)
	if err != nil {
		return err
	}
	return runCmd("route", "-6", "add", network+"/"+prefix, gateway, "metric", "1")
}

func deleteRouteVia6(cidr, gateway string) error {
	network, prefix, err := cidrToV6Prefix(cidr)
	if err != nil {
		return err
	}
	return runCmd("route", "-6", "delete", network+"/"+prefix, gateway)
}

// --- Default gateway ---

func defaultGateway() (string, error) {
	out, err := exec.Command("route", "print", "0.0.0.0").Output()
	if err != nil {
		return "", err
	}
	return parseRouteGateway(string(out))
}

func defaultGateway6() (string, error) {
	out, err := exec.Command("route", "-6", "print").Output()
	if err != nil {
		return "", err
	}
	return parseRoute6Gateway(string(out))
}

// parseRouteGateway extracts the IPv4 default gateway from `route print`
// output. Looks for the line with Network Destination 0.0.0.0 and
// Netmask 0.0.0.0, and returns the Gateway column.
func parseRouteGateway(out string) (string, error) {
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			return fields[2], nil
		}
	}
	return "", fmt.Errorf("no default gateway found")
}

// parseRoute6Gateway extracts the IPv6 default gateway from
// `route -6 print` output. Looks for ::/0 route and returns the
// Gateway column.
func parseRoute6Gateway(out string) (string, error) {
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "::/0" && i+1 < len(fields) {
				return fields[i+1], nil
			}
		}
	}
	return "", fmt.Errorf("no IPv6 default gateway found")
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// --- Batch functions ---

// runBatchScript writes commands to temporary .bat files and executes
// them in parallel (up to 4 concurrent scripts).
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
	f, err := os.CreateTemp("", "lmvpn-routes-*.bat")
	if err != nil {
		return fmt.Errorf("create batch script: %w", err)
	}
	tmpFile := f.Name()
	defer os.Remove(tmpFile)

	for _, line := range lines {
		if _, err := fmt.Fprintln(f, line); err != nil {
			f.Close()
			return fmt.Errorf("write batch script: %w", err)
		}
	}
	f.Close()

	cmd := exec.Command("cmd", "/c", tmpFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("batch: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func addRoutesBatch(cidrs []string, iface string) error {
	idx, err := ifaceIndex(iface)
	if err != nil {
		return err
	}
	var lines []string
	for _, cidr := range cidrs {
		network, mask, err := cidrToNetworkMask(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"route add %s mask %s 0.0.0.0 if %d metric 1", network, mask, idx))
	}
	return runBatchScript(lines)
}

func deleteRoutesBatch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		network, mask, err := cidrToNetworkMask(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("route delete %s mask %s", network, mask))
	}
	return runBatchScript(lines)
}

func addRoutes6Batch(cidrs []string, iface string) error {
	idx, err := ifaceIndex(iface)
	if err != nil {
		return err
	}
	var lines []string
	for _, cidr := range cidrs {
		network, prefix, err := cidrToV6Prefix(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"route -6 add %s/%s :: if %d metric 1", network, prefix, idx))
	}
	return runBatchScript(lines)
}

func deleteRoutes6Batch(cidrs []string, iface string) error {
	var lines []string
	for _, cidr := range cidrs {
		network, prefix, err := cidrToV6Prefix(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("route -6 delete %s/%s ::", network, prefix))
	}
	return runBatchScript(lines)
}

func addRoutesViaBatch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		network, mask, err := cidrToNetworkMask(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"route add %s mask %s %s metric 1", network, mask, gateway))
	}
	return runBatchScript(lines)
}

func deleteRoutesViaBatch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		network, mask, err := cidrToNetworkMask(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("route delete %s mask %s %s", network, mask, gateway))
	}
	return runBatchScript(lines)
}

func addRoutesVia6Batch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		network, prefix, err := cidrToV6Prefix(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"route -6 add %s/%s %s metric 1", network, prefix, gateway))
	}
	return runBatchScript(lines)
}

func deleteRoutesVia6Batch(cidrs []string, gateway string) error {
	var lines []string
	for _, cidr := range cidrs {
		network, prefix, err := cidrToV6Prefix(cidr)
		if err != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("route -6 delete %s/%s %s", network, prefix, gateway))
	}
	return runBatchScript(lines)
}
