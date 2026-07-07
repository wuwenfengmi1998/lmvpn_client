//go:build windows

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"lmvpn/internal/paths"
)

// Launch starts the daemon binary (lmvpnd.exe) as a detached process
// and returns immediately. On Windows, detachment is achieved via
// CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS creation flags instead
// of the Unix Setsid call.
//
// The launcher itself is invoked by the GUI via ShellExecute "runas"
// (UAC elevation). It:
//  1. Computes the daemon log path using the user's home directory
//  2. Opens the log file (append) and NUL
//  3. Starts the lmvpnd.exe binary, redirecting stdio
//  4. Returns immediately — the child continues running independently
func Launch(userHome string, uid, gid int, daemonBin string) error {
	paths.SetUserHome(userHome)
	if err := paths.EnsureDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "ensure dirs: %v\n", err)
	}

	if daemonBin == "" {
		var err error
		daemonBin, err = resolveDaemonBinary()
		if err != nil {
			return err
		}
	}

	absBin, err := filepath.Abs(daemonBin)
	if err != nil {
		return fmt.Errorf("resolve absolute path for %s: %w", daemonBin, err)
	}
	daemonBin = absBin

	if _, err := os.Stat(daemonBin); err != nil {
		return fmt.Errorf("daemon binary not found at %s: %w", daemonBin, err)
	}

	logPath := paths.DaemonLogFile()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open daemon log %s: %w", logPath, err)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		logFile.Close()
		return fmt.Errorf("open NUL: %w", err)
	}

	args := []string{
		"--user-home", userHome,
		"--uid", fmt.Sprintf("%d", uid),
		"--gid", fmt.Sprintf("%d", gid),
	}

	fmt.Fprintf(logFile, "=== launcher: starting daemon %s %v ===\n", daemonBin, args)

	// CREATE_NEW_PROCESS_GROUP (0x200) | DETACHED_PROCESS (0x8)
	procAttr := &os.ProcAttr{
		Dir: os.TempDir(),
		Env: append(os.Environ(),
			"LMVPN_DAEMON=1",
		),
		Files: []*os.File{devNull, logFile, logFile},
		Sys: &syscall.SysProcAttr{
			CreationFlags: 0x00000200 | 0x00000008,
		},
	}

	pid, err := os.StartProcess(daemonBin, append([]string{daemonBin}, args...), procAttr)
	logFile.Close()
	devNull.Close()

	if err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}

	_ = pid.Release()

	fmt.Fprintf(os.Stderr, "daemon launched (pid %d, bin %s)\n", pid.Pid, daemonBin)
	return nil
}
