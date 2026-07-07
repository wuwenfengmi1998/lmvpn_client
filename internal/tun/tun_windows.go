//go:build windows

// Package tun — Windows implementation using WinTun.
//
// WinTun is a userspace TUN driver for Windows developed by the
// WireGuard project. It requires only a single wintun.dll (MIT
// licensed) placed next to the executable — no driver installation
// is needed.
//
// The DLL is embedded at compile time via //go:embed and extracted
// to the executable's directory on first use.
package tun

import (
	_ "embed"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

//go:embed wintun.dll
var wintunDLL []byte

type wintunDevice struct {
	adapter  *wintun.Adapter
	session  wintun.Session
	readWait windows.Handle
	name     string
}

func createTUN(name string) (Device, error) {
	if err := ensureWintunDLL(); err != nil {
		return nil, fmt.Errorf("extract wintun.dll: %w", err)
	}
	if name == "" {
		name = "LMVPN"
	}
	adapter, err := wintun.CreateAdapter(name, "Wintun", nil)
	if err != nil {
		return nil, fmt.Errorf("create wintun adapter: %w", err)
	}
	session, err := adapter.StartSession(0x800000) // 8 MiB ring
	if err != nil {
		adapter.Close()
		return nil, fmt.Errorf("start wintun session: %w", err)
	}
	return &wintunDevice{
		adapter:  adapter,
		session:  session,
		readWait: session.ReadWaitEvent(),
		name:     name,
	}, nil
}

func (d *wintunDevice) Name() string                { return d.name }
func (d *wintunDevice) Close() error                { d.session.End(); return d.adapter.Close() }

func (d *wintunDevice) Read(p []byte) (int, error) {
	for {
		packet, err := d.session.ReceivePacket()
		if err == nil {
			n := copy(p, packet)
			d.session.ReleaseReceivePacket(packet)
			return n, nil
		}
		if err == windows.ERROR_NO_MORE_ITEMS {
			windows.WaitForSingleObject(d.readWait, windows.INFINITE)
			continue
		}
		if err == windows.ERROR_HANDLE_EOF {
			return 0, fmt.Errorf("wintun closed")
		}
		return 0, fmt.Errorf("wintun read: %w", err)
	}
}

func (d *wintunDevice) Write(p []byte) (int, error) {
	buf, err := d.session.AllocateSendPacket(len(p))
	if err != nil {
		if err == windows.ERROR_HANDLE_EOF {
			return 0, fmt.Errorf("wintun closed")
		}
		return 0, fmt.Errorf("wintun allocate: %w", err)
	}
	copy(buf, p)
	d.session.SendPacket(buf)
	return len(p), nil
}

func (d *wintunDevice) Configure(localIP net.IP, prefix int, peerIP net.IP) error {
	if localIP == nil {
		return execCmd("netsh", "interface", "ip", "set", "address", "name="+d.name, "source=dhcp")
	}
	if localIP.To4() == nil {
		return d.configureIPv6Addr(localIP, prefix)
	}
	mask := net.IP(net.CIDRMask(prefix, 32))
	return execCmd("netsh", "interface", "ip", "set", "address",
		"name="+d.name, "source=static",
		"addr="+localIP.String(), "mask="+mask.String())
}

func (d *wintunDevice) ConfigureIPv6(localIP6 net.IP, prefix6 int) error {
	if localIP6 == nil {
		return nil
	}
	return d.configureIPv6Addr(localIP6, prefix6)
}

func (d *wintunDevice) configureIPv6Addr(ip net.IP, prefix int) error {
	addr := fmt.Sprintf("%s/%d", ip.String(), prefix)
	return execCmd("netsh", "interface", "ipv6", "add", "address",
		"interface="+d.name, "address="+addr)
}

func (d *wintunDevice) SetMTU(mtu int) error {
	if err := execCmd("netsh", "interface", "ipv4", "set", "subinterface",
		"name="+d.name, fmt.Sprintf("mtu=%d", mtu)); err != nil {
		return err
	}
	return execCmd("netsh", "interface", "ipv6", "set", "subinterface",
		"interface="+d.name, fmt.Sprintf("mtu=%d", mtu))
}

// ensureWintunDLL extracts the embedded wintun.dll to the executable's
// directory if it does not already exist. The wintun Go package loads
// the DLL via LoadLibrary which searches the exe's directory first.
func ensureWintunDLL() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, wintunDLL, 0o644)
}

// execCmd runs a command, forwarding stdout/stderr.
func execCmd(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(arg, " "), err)
	}
	return nil
}


