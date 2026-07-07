//go:build windows

package keychain

import (
	"errors"
	"fmt"

	"github.com/danieljoos/wincred"
)

// keyPrefixes distinguish password vs token entries in the credential
// store, mirroring the macOS implementation.
const (
	passwordAccountPrefix = "password:"
	tokenAccountPrefix    = "token:"
)

// WinStore implements Store using the Windows Credential Manager.
type WinStore struct{}

// New returns a Windows Credential Manager-backed Store.
func New() Store {
	return WinStore{}
}

// targetName builds the credential TargetName from the account prefix
// and profile name: "<BundleID>/<prefix><profile>".
func targetName(account string) string {
	return ServiceName + "/" + account
}

func (WinStore) setItem(account, secret string) error {
	cred := wincred.NewGenericCredential(targetName(account))
	cred.CredentialBlob = []byte(secret)
	if err := cred.Write(); err != nil {
		return fmt.Errorf("wincred write %s: %w", account, err)
	}
	return nil
}

func (WinStore) getItem(account string) (string, error) {
	cred, err := wincred.GetGenericCredential(targetName(account))
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("wincred read %s: %w", account, err)
	}
	return string(cred.CredentialBlob), nil
}

func (WinStore) deleteItem(account string) error {
	cred, err := wincred.GetGenericCredential(targetName(account))
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil // already gone — treat as success
		}
		return fmt.Errorf("wincred read %s: %w", account, err)
	}
	if err := cred.Delete(); err != nil {
		return fmt.Errorf("wincred delete %s: %w", account, err)
	}
	return nil
}

func (s WinStore) SetPassword(profileName, password string) error {
	return s.setItem(passwordAccountPrefix+profileName, password)
}

func (s WinStore) GetPassword(profileName string) (string, error) {
	return s.getItem(passwordAccountPrefix + profileName)
}

func (s WinStore) DeletePassword(profileName string) error {
	return s.deleteItem(passwordAccountPrefix + profileName)
}

func (s WinStore) SetToken(profileName, token string) error {
	return s.setItem(tokenAccountPrefix+profileName, token)
}

func (s WinStore) GetToken(profileName string) (string, error) {
	return s.getItem(tokenAccountPrefix + profileName)
}

func (s WinStore) DeleteToken(profileName string) error {
	return s.deleteItem(tokenAccountPrefix + profileName)
}

func (s WinStore) DeleteAll(profileName string) error {
	_ = s.DeletePassword(profileName)
	return s.DeleteToken(profileName)
}
