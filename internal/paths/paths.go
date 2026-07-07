// Package paths resolves platform-specific application directories.
//
// Layout follows each platform's conventions. On macOS:
//
//	~/Library/Application Support/com.lmvpn.client/  user data, db, config
//	~/Library/Caches/com.lmvpn.client/              caches
//	~/Library/Logs/com.lmvpn.client/                logs
package paths

import "os"

// BundleID is the application bundle identifier used as the per-app
// subdirectory name under the platform library folders.
const BundleID = "com.lmvpn.client"

// AppName is the human-readable application name.
const AppName = "LMVPN"

// Dirs describes the resolved application directories.
type Dirs struct {
	Data  string // persistent user data (db, config)
	Cache string // recreatable caches
	Log   string // log files
}

// Paths is the resolved directory set for the current platform.
var Paths Dirs

// SetUserHome overrides the home directory used to compute Paths.
// This is used by the daemon (which runs as root) to write logs and
// data to the invoking user's Library folders instead of /var/root.
func SetUserHome(home string) {
	if home != "" {
		recomputePaths(home)
	}
}

// EnsureDirs creates the application directories if they do not exist.
func EnsureDirs() error {
	for _, d := range []string{Paths.Data, Paths.Cache, Paths.Log} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// DBPath returns the path to the SQLite database file.
func DBPath() string { return Paths.Data + "/lmvpn.db" }

// ConfigPath returns the path to the application config file.
func ConfigPath() string { return Paths.Data + "/config.yml" }

// LogFile returns the path to the GUI log file.
func LogFile() string { return Paths.Log + "/lmvpn.log" }

// DaemonLogFile returns the path to the daemon log file.
func DaemonLogFile() string { return Paths.Log + "/lmvpn-daemon.log" }


