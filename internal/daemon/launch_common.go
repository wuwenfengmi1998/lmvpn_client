package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

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
	candidate := filepath.Join(dir, daemonBinaryName)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("could not find %s near %s", daemonBinaryName, exe)
}
