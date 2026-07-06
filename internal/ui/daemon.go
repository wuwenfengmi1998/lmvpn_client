package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"lmvpn/internal/ipc"
	"lmvpn/internal/log"
	"lmvpn/internal/paths"
)

// ensureDaemon checks if the daemon is running and launches it (as
// root via osascript) if not. It blocks until the daemon is reachable
// or times out.
//
// The daemon is launched via the `daemon-launch` subcommand of the GUI
// binary, which uses Go's syscall.SysProcAttr{Setsid: true} to fork
// the actual daemon binary (lmvpnd) into a new session. This is the
// robust replacement for shell-level `nohup ... &` which fails inside
// osascript's `do shell script` (no TTY → ioctl error).
//
// The --daemon-bin flag tells daemon-launch where to find lmvpnd. If
// not given, daemon-launch auto-detects it relative to its own path.
func ensureDaemon() (*ipc.Client, error) {
	// Fast path: daemon already running.
	if c, err := ipc.Dial(); err == nil {
		return c, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable: %w", err)
	}
	// Resolve symlinks (e.g. .app bundle's MacOS/lmvpn).
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, fmt.Errorf("resolve symlink: %w", err)
	}

	// Compute the daemon binary path: same directory as the GUI binary.
	daemonBin := filepath.Join(filepath.Dir(exe), "lmvpnd")
	if _, err := os.Stat(daemonBin); err != nil {
		return nil, fmt.Errorf("daemon binary not found at %s: %w", daemonBin, err)
	}

	home, _ := os.UserHomeDir()
	uid := os.Getuid()
	gid := os.Getgid()

	// Launch the daemon via osascript (prompts for admin password).
	// The `daemon-launch` subcommand forks lmvpnd with Setsid and exits
	// immediately, so osascript returns right away.
	script := fmt.Sprintf(
		`do shell script %q with administrator privileges`,
		fmt.Sprintf("%s daemon-launch --user-home %s --uid %d --gid %d --daemon-bin %s",
			shellQuote(exe), shellQuote(home), uid, gid, shellQuote(daemonBin)),
	)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("launch daemon: %w (output: %s)", err, string(out))
	}
	log.L().Info("daemon launched via osascript",
		"uid", uid, "gid", gid, "home", home, "daemon_bin", daemonBin)

	// Wait for the daemon to become reachable.
	logFile := paths.DaemonLogFile()
	for i := 0; i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		if c, err := ipc.Dial(); err == nil {
			log.L().Info("daemon connected", "socket", paths.IPCSocketPath())
			return c, nil
		}
	}
	return nil, fmt.Errorf("daemon did not become reachable (check %s)", logFile)
}

// shellQuote wraps a string in single quotes for shell safety.
// Embedded single quotes are escaped using the '\'' pattern.
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
