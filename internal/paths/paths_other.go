//go:build !darwin && !windows

package paths

import (
	"os"
	"path/filepath"
)

// IPCSocketPath returns the path to the unix domain socket used for
// GUI <-> daemon communication.
func IPCSocketPath() string { return "/tmp/lmvpn.sock" }

func init() {
	home, _ := os.UserHomeDir()
	recomputePaths(home)
}

// recomputePaths sets Paths based on the given home directory.
// Uses XDG-style layout on Linux/other platforms.
func recomputePaths(home string) {
	Paths = Dirs{
		Data:  filepath.Join(home, ".local", "share", BundleID),
		Cache: filepath.Join(home, ".cache", BundleID),
		Log:   filepath.Join(home, ".local", "state", BundleID, "log"),
	}
}
