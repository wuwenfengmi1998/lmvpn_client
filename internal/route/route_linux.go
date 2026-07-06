//go:build !darwin

package route

import (
	"fmt"
	"os/exec"
	"strings"
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

func defaultGateway() (string, error) {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
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
