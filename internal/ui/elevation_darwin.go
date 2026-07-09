//go:build darwin

package ui

import (
	"fmt"
	"os/exec"

	"lmvpn/internal/log"
)

const daemonBinaryName = "lmvpnd"

// launchElevated launches the daemon-launch subcommand with admin
// privileges via osascript on macOS.
func launchElevated(exe, daemonBin, home string, uid, gid int) error {
	script := fmt.Sprintf(
		`do shell script %q with administrator privileges`,
		fmt.Sprintf("%s daemon-launch --user-home %s --uid %d --gid %d --daemon-bin %s",
			shellQuote(exe), shellQuote(home), uid, gid, shellQuote(daemonBin)),
	)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launch daemon: %w (output: %s)", err, string(out))
	}
	log.L().Info("daemon launched via osascript",
		"uid", uid, "gid", gid, "home", home, "daemon_bin", daemonBin)
	return nil
}

// shellQuote wraps a string in single quotes for shell safety.
// Embedded single quotes are escaped using the '\” pattern.
func shellQuote(s string) string {
	result := "'"
	for _, r := range s {
		if r == '\'' {
			result += "'\\''"
		} else {
			result += string(r)
		}
	}
	result += "'"
	return result
}
