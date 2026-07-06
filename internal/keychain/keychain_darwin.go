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

// DarwinStore implements Store using the macOS Keychain.
type DarwinStore struct{}

// New returns a macOS Keychain-backed Store.
func New() Store {
	return DarwinStore{}
}

func (DarwinStore) setItem(account, secret string) error {
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
