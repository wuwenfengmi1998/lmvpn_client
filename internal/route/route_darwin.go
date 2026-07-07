//go:build darwin

package route

import (
	"fmt"
	"os/exec"
	"strings"
)

// addRoute adds a route via a network interface (macOS route command).
//
//	route add -inet -net <cidr> -interface <iface>
func addRoute(cidr, iface string) error {
	return runRoute("add", "-inet", "-net", cidr, "-interface", iface)
}

// deleteRoute removes a route via a network interface.
func deleteRoute(cidr, iface string) error {
	return runRoute("delete", "-inet", "-net", cidr, "-interface", iface)
}

// addRouteVia adds a route via a gateway IP.
func addRouteVia(cidr, gateway string) error {
	return runRoute("add", "-inet", "-net", cidr, gateway)
}

// deleteRouteVia removes a route via a gateway IP.
func deleteRouteVia(cidr, gateway string) error {
	return runRoute("delete", "-inet", "-net", cidr, gateway)
}

// --- IPv6 variants ---

func addRoute6(cidr, iface string) error {
	return runRoute("add", "-inet6", "-net", cidr, "-interface", iface)
}

func deleteRoute6(cidr, iface string) error {
	return runRoute("delete", "-inet6", "-net", cidr, "-interface", iface)
}

func addRouteVia6(cidr, gateway string) error {
	return runRoute("add", "-inet6", "-net", cidr, gateway)
}

func deleteRouteVia6(cidr, gateway string) error {
	return runRoute("delete", "-inet6", "-net", cidr, gateway)
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
