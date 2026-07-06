//go:build !darwin

package paths

import (
	"os"
	"path/filepath"
)

func init() {
	home, _ := os.UserHomeDir()
	Paths = Dirs{
		Data:  filepath.Join(home, ".local", "share", BundleID),
		Cache: filepath.Join(home, ".cache", BundleID),
		Log:   filepath.Join(home, ".local", "state", BundleID, "log"),
	}
}
