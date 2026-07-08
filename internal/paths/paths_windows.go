//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

// IPCNetwork returns the transport network for GUI <-> daemon IPC.
// Windows uses TCP because AF_UNIX sockets enforce mandatory
// integrity-level checks: a socket created by the elevated daemon
// (High Integrity) cannot be connected to by the non-elevated GUI
// (Medium Integrity). TCP on 127.0.0.1 has no such restriction.
func IPCNetwork() string { return "tcp" }

// IPCAddress returns the listen/dial address for GUI <-> daemon IPC.
// On Windows this is a TCP address on localhost.
const ipcPort = "18923"

func IPCAddress() string { return "127.0.0.1:" + ipcPort }

// GUILockNetwork returns the transport for the GUI single-instance lock.
// Windows uses TCP (same reason as IPC: AF_UNIX integrity-level checks).
func GUILockNetwork() string { return "tcp" }

// GUILockAddress returns the address for the GUI single-instance lock.
func GUILockAddress() string { return "127.0.0.1:18924" }

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
