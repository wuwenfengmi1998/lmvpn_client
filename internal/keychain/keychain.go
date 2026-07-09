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

// ErrSecMissingEntitlement is returned when the biometric keychain path
// fails because the app lacks the required code-signing entitlements
// (errSecMissingEntitlement, OSStatus -34018). Callers should fall back
// to the non-biometric keychain path when this error is encountered.
var ErrSecMissingEntitlement = fmt.Errorf("missing keychain entitlement")

// ErrUserCanceled is returned when the user cancels a biometric
// authentication prompt (e.g. Touch ID) or the system cannot complete
// authentication.
var ErrUserCanceled = fmt.Errorf("user canceled authentication")

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
