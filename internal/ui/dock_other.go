//go:build !darwin

package ui

var onAppActive func()

func showDockIcon() {}
func hideDockIcon() {}

func activateApp() {}

func registerReopenHandler() {}
