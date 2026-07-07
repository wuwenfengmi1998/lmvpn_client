//go:build !darwin && !windows

package ui

import (
	"fmt"
	"os/exec"

	"lmvpn/internal/log"
)

const daemonBinaryName = "lmvpnd"

// launchElevated launches the daemon-launch subcommand with admin
// privileges. On Linux, pkexec is used (common on desktop Linux).
func launchElevated(exe, daemonBin, home string, uid, gid int) error {
	cmd := exec.Command("pkexec", exe, "daemon-launch",
		"--user-home", home,
		"--uid", fmt.Sprintf("%d", uid),
		"--gid", fmt.Sprintf("%d", gid),
		"--daemon-bin", daemonBin)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launch daemon: %w (output: %s)", err, string(out))
	}
	log.L().Info("daemon launched via pkexec",
		"uid", uid, "gid", gid, "home", home, "daemon_bin", daemonBin)
	return nil
}
