//go:build darwin

package ui

import "C"

// onAppActive is called from the Cocoa main thread when the app
// becomes active (e.g. the user re-opens the .app bundle while the
// process is still running).  It is set once in Run() before the
// event loop starts, so no synchronisation is needed.
var onAppActive func()

//export cmAppBecameActive
func cmAppBecameActive() {
	if onAppActive != nil {
		onAppActive()
	}
}
