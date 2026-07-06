// Package ui contains the Fyne desktop GUI. It runs as the user,
// manages profiles and credentials (SQLite + Keychain), and controls
// the privileged daemon over IPC.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"lmvpn/internal/ipc"
	"lmvpn/internal/log"
	"lmvpn/internal/paths"
)

// ensureDaemon checks if the daemon is running and launches it (as
// root via osascript) if not. It blocks until the daemon is reachable
// or times out.
func ensureDaemon() (*ipc.Client, error) {
	// Fast path: daemon already running.
	if c, err := ipc.Dial(); err == nil {
		return c, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}

	// Launch daemon as root via osascript administrator prompt.
	script := fmt.Sprintf(
		`do shell script "nohup %s daemon > /dev/null 2>&1 &" with administrator privileges`,
		exe)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("launch daemon: %w (%s)", err, string(out))
	}
	log.L().Info("daemon launched via osascript")

	// Wait for the daemon to become reachable.
	for i := 0; i < 20; i++ {
		time.Sleep(250 * time.Millisecond)
		if c, err := ipc.Dial(); err == nil {
			log.L().Info("daemon connected", "socket", paths.IPCSocketPath())
			return c, nil
		}
	}
	return nil, fmt.Errorf("daemon did not become reachable")
}
