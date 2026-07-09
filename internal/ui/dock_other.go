//go:build !darwin

package ui

var onAppActive func()

func activateApp() {}

func registerReopenHandler() {}
