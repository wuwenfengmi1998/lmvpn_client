//go:build darwin

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
