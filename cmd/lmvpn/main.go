// Command lmvpn is the LMVPN GUI client application.
//
// It runs in two modes:
//
//	lmvpn              — GUI mode (default): Fyne desktop application
//	lmvpn daemon-launch — launcher: forks the privileged daemon (lmvpnd)
//	                     into a new session (Setsid) and exits immediately.
//	                     Invoked by osascript as root; replaces fragile
//	                     `nohup ... &` shell tricks that fail without a TTY.
//
// The privileged daemon itself is a separate binary, lmvpnd, to avoid
// loading Fyne (and its locale/font initialisation) in the root process.
package main

import (
	"flag"
	"fmt"
	"os"

	"lmvpn/internal/daemon"
	"lmvpn/internal/ui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "daemon-launch" {
		runDaemonLaunch()
		return
	}

	ui.Run()
}

// runDaemonLaunch forks the daemon binary (lmvpnd) into a new session
// and exits immediately. Invoked by osascript (as root).
func runDaemonLaunch() {
	fs := flag.NewFlagSet("daemon-launch", flag.ExitOnError)
	userHome := fs.String("user-home", "", "invoking user's home directory")
	uid := fs.Int("uid", -1, "invoking user's UID for socket/log ownership")
	gid := fs.Int("gid", -1, "invoking user's GID for socket/log ownership")
	daemonBin := fs.String("daemon-bin", "", "path to the lmvpnd binary (auto-detected if empty)")
	fs.Parse(os.Args[2:])

	if err := daemon.Launch(*userHome, *uid, *gid, *daemonBin); err != nil {
		fmt.Fprintf(os.Stderr, "daemon-launch: %v\n", err)
		os.Exit(1)
	}
}
