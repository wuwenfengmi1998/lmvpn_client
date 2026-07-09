//go:build !darwin

package ui

// setTouchIDPromptFromI18n is a no-op on non-darwin platforms where
// Touch ID is not available.
func setTouchIDPromptFromI18n() {}
