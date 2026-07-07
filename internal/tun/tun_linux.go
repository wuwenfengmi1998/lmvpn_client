//go:build !darwin && !windows

package tun

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/songgao/water"
)

type linuxDevice struct {
	ifce *water.Interface
}

func createTUN(name string) (Device, error) {
	cfg := water.Config{DeviceType: water.TUN}
	cfg.Name = name
	ifce, err := water.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create tun: %w", err)
	}
	return &linuxDevice{ifce: ifce}, nil
}

func (d *linuxDevice) Name() string                { return d.ifce.Name() }
func (d *linuxDevice) Read(p []byte) (int, error)  { return d.ifce.Read(p) }
func (d *linuxDevice) Write(p []byte) (int, error) { return d.ifce.Write(p) }
func (d *linuxDevice) Close() error                { return d.ifce.Close() }

func (d *linuxDevice) Configure(localIP net.IP, prefix int, peerIP net.IP) error {
	if err := execCmd("ip", "link", "set", "dev", d.Name(), "up"); err != nil {
		return err
	}
	if localIP == nil {
		return nil
	}
	localCidr := fmt.Sprintf("%s/%d", localIP.String(), prefix)
	if err := execCmd("ip", "addr", "add", "dev", d.Name(), localCidr, "peer", peerIP.String()); err != nil {
		// Fall back without peer (some kernels).
		return execCmd("ip", "addr", "add", "dev", d.Name(), localCidr)
	}
	return nil
}

func (d *linuxDevice) ConfigureIPv6(localIP6 net.IP, prefix6 int) error {
	if localIP6 == nil {
		return nil
	}
	localCidr := fmt.Sprintf("%s/%d", localIP6.String(), prefix6)
	return execCmd("ip", "-6", "addr", "add", "dev", d.Name(), localCidr)
}

func (d *linuxDevice) SetMTU(mtu int) error {
	return execCmd("ip", "link", "set", "dev", d.Name(), "mtu", fmt.Sprintf("%d", mtu))
}

func execCmd(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(arg, " "), err)
	}
	return nil
}
