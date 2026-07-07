//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

// IPCSocketPath returns the path to the unix domain socket used for
// GUI <-> daemon communication. On Windows 10 1803+ AF_UNIX is
// supported; the socket is placed in the user's temp directory.
func IPCSocketPath() string {
	return filepath.Join(os.TempDir(), "lmvpn.sock")
}

func init() {
	home, _ := os.UserHomeDir()
	recomputePaths(home)
}

// recomputePaths sets Paths based on the given home directory.
// On Windows the layout follows platform conventions:
//
//	%APPDATA%\<BundleID>\      data (db, config)
//	%LOCALAPPDATA%\<BundleID>\ cache
//	%LOCALAPPDATA%\<BundleID>\Logs\  logs
func recomputePaths(home string) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	Paths = Dirs{
		Data:  filepath.Join(appData, BundleID),
		Cache: filepath.Join(localAppData, BundleID),
		Log:   filepath.Join(localAppData, BundleID, "Logs"),
	}
}
