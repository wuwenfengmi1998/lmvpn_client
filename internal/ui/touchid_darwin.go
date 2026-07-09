//go:build darwin

package ui

import (
	"lmvpn/internal/i18n"
	"lmvpn/internal/keychain"
)

// setTouchIDPromptFromI18n configures the localized Touch ID prompt
// text on the keychain store for the current language.
func setTouchIDPromptFromI18n() {
	keychain.SetTouchIDPrompt(i18n.T("TouchIDPrompt"))
}
