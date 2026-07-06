// Package tun provides a cross-platform TUN virtual network card
// abstraction. Each platform implements the Device interface.
//
// On macOS the TUN device is a utunN interface created via the
// songgao/water library (same as the server). Configuration uses
// ifconfig/route commands and requires root privileges.
package tun

import "net"

// Device represents a TUN virtual network interface.
type Device interface {
	// Name returns the OS-assigned interface name (e.g. utun4).
	Name() string
	// Read reads one IP packet from the TUN device.
	Read(p []byte) (int, error)
	// Write writes one IP packet to the TUN device.
	Write(p []byte) (int, error)
	// Configure sets the interface address, prefix, and peer IP.
	Configure(localIP net.IP, prefix int, peerIP net.IP) error
	// SetMTU sets the interface MTU.
	SetMTU(mtu int) error
	// Close destroys the TUN device.
	Close() error
}

// Create creates a new TUN device. If name is empty, the OS assigns
// a name (utunN on macOS, tunN on Linux).
func Create(name string) (Device, error) { return createTUN(name) }
