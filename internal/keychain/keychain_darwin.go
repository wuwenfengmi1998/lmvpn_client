//go:build darwin

package keychain

import (
	"fmt"

	"github.com/keybase/go-keychain"
)

// keyPrefixes distinguish password vs token entries in the keychain.
const (
	passwordAccountPrefix = "password:"
	tokenAccountPrefix    = "token:"
)

// touchIDPrompt is the localized prompt shown in the Touch ID dialog.
// It is set by the UI layer at startup via SetTouchIDPrompt.
var touchIDPrompt = "authenticate to access your VPN password"

// SetTouchIDPrompt sets the localized prompt text shown in the Touch ID
// dialog when retrieving secrets from the keychain.
func SetTouchIDPrompt(prompt string) {
	if prompt != "" {
		touchIDPrompt = prompt
	}
}

// DarwinStore implements Store using the macOS Keychain.
type DarwinStore struct{}

// New returns a macOS Keychain-backed Store. On Macs with Touch ID,
// secrets are stored with biometric (Touch ID) protection; on older
// Macs without a biometric sensor, the standard keychain accessibility
// is used as a fallback.
func New() Store {
	return DarwinStore{}
}

func (DarwinStore) setItem(account, secret string) error {
	// Use biometric-protected storage when Touch ID is available.
	if biometricAvailable() {
		return storeBiometricItemGo(ServiceName, account, secret)
	}
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ServiceName)
	item.SetAccount(account)
	item.SetData([]byte(secret))
	item.SetAccessible(keychain.AccessibleAfterFirstUnlock)
	// Delete any existing item first (Add fails on duplicates).
	_ = keychain.DeleteItem(item)
	if err := keychain.AddItem(item); err != nil {
		return fmt.Errorf("keychain add %s: %w", account, err)
	}
	return nil
}

func (DarwinStore) getItem(account string) (string, error) {
	// When Touch ID is available, use the direct CGo path which can
	// show a biometric prompt for protected items.
	if biometricAvailable() {
		return getBiometricItemGo(ServiceName, account, touchIDPrompt)
	}
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ServiceName)
	item.SetAccount(account)
	item.SetMatchLimit(keychain.MatchLimitOne)
	item.SetReturnData(true)
	results, err := keychain.QueryItem(item)
	if err != nil {
		return "", fmt.Errorf("keychain query %s: %w", account, err)
	}
	if len(results) == 0 {
		return "", ErrNotFound
	}
	return string(results[0].Data), nil
}

func (DarwinStore) deleteItem(account string) error {
	// Use the direct CGo delete which works for both biometric and
	// non-biometric items (and doesn't trigger a Touch ID prompt).
	if biometricAvailable() {
		return deleteBiometricItemGo(ServiceName, account)
	}
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ServiceName)
	item.SetAccount(account)
	return keychain.DeleteItem(item)
}

func (s DarwinStore) SetPassword(profileName, password string) error {
	return s.setItem(passwordAccountPrefix+profileName, password)
}

func (s DarwinStore) GetPassword(profileName string) (string, error) {
	return s.getItem(passwordAccountPrefix + profileName)
}

func (s DarwinStore) DeletePassword(profileName string) error {
	return s.deleteItem(passwordAccountPrefix + profileName)
}

func (s DarwinStore) SetToken(profileName, token string) error {
	return s.setItem(tokenAccountPrefix+profileName, token)
}

func (s DarwinStore) GetToken(profileName string) (string, error) {
	return s.getItem(tokenAccountPrefix + profileName)
}

func (s DarwinStore) DeleteToken(profileName string) error {
	return s.deleteItem(tokenAccountPrefix + profileName)
}

func (s DarwinStore) DeleteAll(profileName string) error {
	_ = s.DeletePassword(profileName)
	return s.DeleteToken(profileName)
}
