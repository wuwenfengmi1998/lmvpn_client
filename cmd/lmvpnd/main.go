// Command lmvpnd is the privileged LMVPN daemon. It owns the WebSocket
// transport, TUN device, and routing. It receives commands from the GUI
// over an IPC unix socket and broadcasts state/stats events back.
//
// This is a separate binary from the GUI (lmvpn) to avoid loading Fyne
// (and its locale/font initialisation) in the root process.
//
// Daemon flags:
//
//	--user-home <path>  invoking user's home dir (for log/data paths)
//	--uid <int>         invoking user's UID (chown IPC socket + logs)
//	--gid <int>         invoking user's GID
package main

import (
	"flag"
	"fmt"
	"os"

	"lmvpn/internal/daemon"
)

func main() {
	fs := flag.NewFlagSet("lmvpnd", flag.ExitOnError)
	userHome := fs.String("user-home", "", "invoking user's home directory")
	uid := fs.Int("uid", -1, "invoking user's UID for socket/log ownership")
	gid := fs.Int("gid", -1, "invoking user's GID for socket/log ownership")
	fs.Parse(os.Args[1:])

	if err := daemon.Run(*userHome, *uid, *gid); err != nil {
		fmt.Fprintf(os.Stderr, "lmvpnd: %v\n", err)
		os.Exit(1)
	}
}
