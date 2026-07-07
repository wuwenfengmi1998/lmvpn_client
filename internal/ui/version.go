package ui

import "lmvpn/internal/version"

// Version is the application version, sourced from the shared version
// package so the GUI and daemon report the same string.
var Version = version.Version
