//go:build !darwin && !windows

package paths

import (
	"os"
	"path/filepath"
)

// IPCNetwork returns the transport network for GUI <-> daemon IPC.
func IPCNetwork() string { return "unix" }

// IPCAddress returns the listen/dial address for GUI <-> daemon IPC.
func IPCAddress() string { return "/tmp/lmvpn.sock" }

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
