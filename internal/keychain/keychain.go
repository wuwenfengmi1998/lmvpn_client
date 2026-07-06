// Package keychain stores and retrieves secrets (passwords, JWT tokens)
// in the macOS Keychain. On non-darwin platforms it falls back to an
// in-memory store (to be replaced with a platform-appropriate backend).
//
// Secrets are keyed by the server profile name. The Keychain service
// name is the application bundle identifier.
package keychain

import (
	"fmt"

	"lmvpn/internal/paths"
)

// ServiceName is the Keychain service under which all lmvpn secrets
// are stored.
const ServiceName = paths.BundleID

// ErrNotFound is returned when a secret is not present in the store.
var ErrNotFound = fmt.Errorf("secret not found")

// Store is the secret storage interface.
type Store interface {
	SetPassword(profileName, password string) error
	GetPassword(profileName string) (string, error)
	DeletePassword(profileName string) error
	SetToken(profileName, token string) error
	GetToken(profileName string) (string, error)
	DeleteToken(profileName string) error
	DeleteAll(profileName string) error
}
