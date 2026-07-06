package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"lmvpn/internal/paths"
)

// Launch forks the daemon binary (lmvpnd) into a new session (detached
// from any controlling terminal) and returns immediately.
//
// This is the robust replacement for shell-level `nohup ... &` which
// fails inside osascript's `do shell script` (no TTY → ioctl error).
//
// The launcher itself is invoked by osascript as root. It:
//  1. Computes the daemon log path using the user's home directory
//  2. Opens the log file (append) and /dev/null
//  3. Starts the lmvpnd binary, redirecting the child's stdio to the
//     log file and /dev/null
//  4. Sets SysProcAttr.Setsid = true so the child starts a new session
//     (no controlling terminal, survives parent exit, no SIGHUP)
//  5. Returns immediately — the child continues reparented to PID 1
//
// userHome/uid/gid are forwarded to the daemon so it can chown the IPC
// socket and place logs in the user's Library.
//
// daemonBin is the path to the lmvpnd binary. If empty, it is
// auto-detected relative to the launcher's executable path.
func Launch(userHome string, uid, gid int, daemonBin string) error {
	// Use the user's home directory for log path computation.
	paths.SetUserHome(userHome)
	if err := paths.EnsureDirs(); err != nil {
		// Non-fatal — root can usually create these.
		fmt.Fprintf(os.Stderr, "ensure dirs: %v\n", err)
	}

	// Resolve the daemon binary path if not given explicitly.
	if daemonBin == "" {
		var err error
		daemonBin, err = resolveDaemonBinary()
		if err != nil {
			return err
		}
	}

	// Convert to absolute path — the child process runs with Dir="/"
	// so a relative path would not resolve.
	absBin, err := filepath.Abs(daemonBin)
	if err != nil {
		return fmt.Errorf("resolve absolute path for %s: %w", daemonBin, err)
	}
	daemonBin = absBin

	// Verify the daemon binary exists.
	if _, err := os.Stat(daemonBin); err != nil {
		return fmt.Errorf("daemon binary not found at %s: %w", daemonBin, err)
	}

	logPath := paths.DaemonLogFile()
	// Pre-create/truncate-open the log file so the child inherits it.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open daemon log %s: %w", logPath, err)
	}
	// Chown so the user can read it.
	if uid >= 0 {
		_ = os.Chown(logPath, uid, gid)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		logFile.Close()
		return fmt.Errorf("open /dev/null: %w", err)
	}

	// Build the daemon command with the same flags.
	args := []string{
		"--user-home", userHome,
		"--uid", fmt.Sprintf("%d", uid),
		"--gid", fmt.Sprintf("%d", gid),
	}

	// Write a marker line so we can see the launch happened even if the
	// daemon fails to start.
	fmt.Fprintf(logFile, "=== launcher: starting daemon %s %v ===\n", daemonBin, args)

	// Set up the child process.
	procAttr := &os.ProcAttr{
		Dir: "/",
		Env: append(os.Environ(),
			"LMVPN_DAEMON=1",
		),
		Files: []*os.File{devNull, logFile, logFile},
		Sys: &syscall.SysProcAttr{
			// Setsid creates a new session, detaching the child from the
			// controlling terminal. The child becomes a session leader
			// and will not receive SIGHUP when the launcher exits.
			Setsid: true,
		},
	}

	pid, err := os.StartProcess(daemonBin, append([]string{daemonBin}, args...), procAttr)
	// Close our handles to the files; the child has inherited them.
	logFile.Close()
	devNull.Close()

	if err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}

	// Release the child so it doesn't become a zombie when the launcher
	// exits — it will be reparented to PID 1.
	_ = pid.Release()

	// Print success to stderr (visible in osascript output if captured).
	fmt.Fprintf(os.Stderr, "daemon launched (pid %d, bin %s)\n", pid.Pid, daemonBin)
	return nil
}

// resolveDaemonBinary locates the lmvpnd binary relative to the
// launcher's executable path. In a .app bundle, lmvpnd lives in the
// same Contents/MacOS/ directory as lmvpn. In development, it lives
// in the same build directory.
func resolveDaemonBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve launcher executable: %w", err)
	}

	// Resolve any symlinks.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve symlink: %w", err)
	}

	dir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(dir, "lmvpnd"),
		filepath.Join(dir, "lmvpn-daemon"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("could not find lmvpnd binary near %s (tried %v)", exe, candidates)
}
