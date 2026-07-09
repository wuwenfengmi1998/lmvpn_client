//go:build darwin

package keychain

import (
	"errors"
	"fmt"
	"sync"

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

// biometricDisabled tracks whether the biometric keychain path has been
// disabled at runtime. This happens when a biometric operation fails with
// errSecMissingEntitlement (-34018), indicating the app lacks the required
// code-signing entitlements. Once disabled, all subsequent operations use
// the non-biometric file-based keychain path.
var (
	biometricDisabled   bool
	biometricDisabledMu sync.Mutex
)

// shouldUseBiometric reports whether the biometric keychain path should be
// used. It returns false if biometric storage has been disabled at runtime
// (e.g. due to missing entitlements on an ad-hoc signed build).
func shouldUseBiometric() bool {
	if !biometricAvailable() {
		return false
	}
	biometricDisabledMu.Lock()
	defer biometricDisabledMu.Unlock()
	return !biometricDisabled
}

// disableBiometric disables the biometric keychain path for all subsequent
// operations, forcing a fallback to the non-biometric file-based keychain.
func disableBiometric() {
	biometricDisabledMu.Lock()
	defer biometricDisabledMu.Unlock()
	biometricDisabled = true
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

// setItemPlain stores a secret in the file-based keychain (no biometric
// protection) using the keybase/go-keychain library.
func setItemPlain(account, secret string) error {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ServiceName)
	item.SetAccount(account)
	item.SetData([]byte(secret))
	item.SetAccessible(keychain.AccessibleAfterFirstUnlock)
	_ = keychain.DeleteItem(item)
	if err := keychain.AddItem(item); err != nil {
		return fmt.Errorf("keychain add %s: %w", account, err)
	}
	return nil
}

// getItemPlain retrieves a secret from the file-based keychain.
func getItemPlain(account string) (string, error) {
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

// deleteItemPlain deletes a secret from the file-based keychain.
func deleteItemPlain(account string) error {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassGenericPassword)
	item.SetService(ServiceName)
	item.SetAccount(account)
	return keychain.DeleteItem(item)
}

func (DarwinStore) setItem(account, secret string) error {
	if shouldUseBiometric() {
		err := storeBiometricItemGo(ServiceName, account, secret)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrSecMissingEntitlement) {
			disableBiometric()
			// Fall through to non-biometric path.
		} else {
			return err
		}
	}
	return setItemPlain(account, secret)
}

func (DarwinStore) getItem(account string) (string, error) {
	if shouldUseBiometric() {
		secret, err := getBiometricItemGo(ServiceName, account, touchIDPrompt)
		if err == nil {
			return secret, nil
		}
		if errors.Is(err, ErrSecMissingEntitlement) {
			disableBiometric()
			// Fall through to non-biometric path.
		} else if errors.Is(err, ErrNotFound) {
			// Item not in the Data Protection Keychain; it may have
			// been stored via the non-biometric path. Fall through.
		} else {
			return "", err
		}
	}
	return getItemPlain(account)
}

func (DarwinStore) deleteItem(account string) error {
	if shouldUseBiometric() {
		err := deleteBiometricItemGo(ServiceName, account)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrSecMissingEntitlement) {
			disableBiometric()
			// Fall through to non-biometric path.
		} else {
			return err
		}
	}
	return deleteItemPlain(account)
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
