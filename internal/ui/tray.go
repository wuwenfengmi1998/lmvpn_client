package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// setupTray configures the system tray menu (desktop only).
func (a *App) setupTray() {
	deskApp, ok := a.fyneApp.(desktop.App)
	if !ok {
		return // not a desktop app, skip tray
	}
	menu := fyne.NewMenu("LMVPN",
		fyne.NewMenuItem("Show Window", func() {
			a.window.Show()
			a.window.RequestFocus()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Connect", func() {
			a.onConnect()
		}),
		fyne.NewMenuItem("Disconnect", func() {
			a.onDisconnect()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			a.fyneApp.Quit()
		}),
	)
	deskApp.SetSystemTrayMenu(menu)
}
