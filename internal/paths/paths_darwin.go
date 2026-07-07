//go:build darwin

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
// On macOS the layout is:
//
//	<home>/Library/Application Support/<BundleID>/  data
//	<home>/Library/Caches/<BundleID>/               cache
//	<home>/Library/Logs/<BundleID>/                 logs
func recomputePaths(home string) {
	lib := filepath.Join(home, "Library")
	Paths = Dirs{
		Data:  filepath.Join(lib, "Application Support", BundleID),
		Cache: filepath.Join(lib, "Caches", BundleID),
		Log:   filepath.Join(lib, "Logs", BundleID),
	}
}
