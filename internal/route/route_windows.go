//go:build windows

package route

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
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
