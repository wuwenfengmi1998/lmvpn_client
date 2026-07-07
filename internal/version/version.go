// Package version holds the build version string shared by the GUI
// (lmvpn) and daemon (lmvpnd) binaries. The value is injected at link
// time via ldflags:
//
//	-X lmvpn/internal/version.Version=$(VERSION)
//
// Both binaries are built with the same LDFLAGS, so a version mismatch
// between the running daemon and the GUI indicates a stale daemon
// process that predates the current build.
package version

// Version is the application version. Overridden at link time; "dev"
// when running an untagged build (e.g. go run / go build without
// ldflags).
var Version = "dev"
