//go:build darwin

package tun

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/songgao/water"
)

type darwinDevice struct {
	ifce *water.Interface
}

func createTUN(name string) (Device, error) {
	cfg := water.Config{DeviceType: water.TUN}
	cfg.Name = name
	ifce, err := water.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create utun: %w", err)
	}
	return &darwinDevice{ifce: ifce}, nil
}

func (d *darwinDevice) Name() string                  { return d.ifce.Name() }
func (d *darwinDevice) Read(p []byte) (int, error)    { return d.ifce.Read(p) }
func (d *darwinDevice) Write(p []byte) (int, error)   { return d.ifce.Write(p) }
func (d *darwinDevice) Close() error                  { return d.ifce.Close() }

func (d *darwinDevice) Configure(localIP net.IP, prefix int, peerIP net.IP) error {
	if localIP == nil {
		return execCmd("ifconfig", d.Name(), "up")
	}
	inetType := "inet"
	if localIP.To4() == nil {
		inetType = "inet6"
	}
	localCidr := fmt.Sprintf("%s/%d", localIP.String(), prefix)
	// ifconfig utunN inet <ip>/<prefix> <peer_ip> up
	return execCmd("ifconfig", d.Name(), inetType, localCidr, peerIP.String(), "up")
}

func (d *darwinDevice) SetMTU(mtu int) error {
	return execCmd("ifconfig", d.Name(), "mtu", fmt.Sprintf("%d", mtu))
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
