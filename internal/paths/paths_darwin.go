//go:build darwin

package paths

import (
	"os"
	"path/filepath"
)

func init() {
	home, _ := os.UserHomeDir()
	lib := filepath.Join(home, "Library")
	Paths = Dirs{
		Data:  filepath.Join(lib, "Application Support", BundleID),
		Cache: filepath.Join(lib, "Caches", BundleID),
		Log:   filepath.Join(lib, "Logs", BundleID),
	}
}
