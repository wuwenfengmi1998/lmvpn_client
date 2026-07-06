// Command lmvpn is the LMVPN client application. It runs in two modes:
//
//   lmvpn           — GUI mode (default): Fyne desktop application
//   lmvpn daemon    — privileged daemon: owns the TUN device and
//                     WebSocket transport, launched as root by the GUI
//
// The split architecture keeps the GUI running as the user (with
// Keychain access) while the daemon runs as root (with TUN/route
// privileges). They communicate over a unix domain socket.
package main

import (
	"fmt"
	"os"

	"lmvpn/internal/daemon"
	"lmvpn/internal/ui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		if err := daemon.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ui.Run()
}
