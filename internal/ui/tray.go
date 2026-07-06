package ui

import (
	"lmvpn/internal/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
)

// setupTray configures the system tray menu (desktop only).
func (a *App) setupTray() {
	deskApp, ok := a.fyneApp.(desktop.App)
	if !ok {
		return // not a desktop app, skip tray
	}

	// Language submenu — labels for English and 中文 are always shown in
	// their native script so the user can recognise them regardless of
	// the active language.
	autoItem := fyne.NewMenuItem(i18n.T("TrayLanguageAuto"), func() {
		a.changeLanguage(i18n.LangAuto)
	})
	enItem := fyne.NewMenuItem("English", func() {
		a.changeLanguage(i18n.LangEn)
	})
	zhItem := fyne.NewMenuItem("中文", func() {
		a.changeLanguage(i18n.LangZhHans)
	})

	// Mark the currently selected option.
	switch {
	case a.langSetting == "" || a.langSetting == i18n.LangAuto:
		autoItem.Checked = true
	case a.langSetting == i18n.LangEn:
		enItem.Checked = true
	case a.langSetting == i18n.LangZhHans:
		zhItem.Checked = true
	}

	langItem := fyne.NewMenuItem(i18n.T("TrayLanguage"), nil)
	langItem.ChildMenu = fyne.NewMenu(i18n.T("TrayLanguage"),
		autoItem, enItem, zhItem,
	)

	menu := fyne.NewMenu(i18n.T("WindowTitle"),
		fyne.NewMenuItem(i18n.T("TrayShowWindow"), func() {
			a.window.Show()
			a.window.RequestFocus()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("TrayConnect"), func() {
			a.onConnect()
		}),
		fyne.NewMenuItem(i18n.T("TrayDisconnect"), func() {
			a.onDisconnect()
		}),
		fyne.NewMenuItemSeparator(),
		langItem,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem(i18n.T("TrayQuit"), func() {
			a.fyneApp.Quit()
		}),
	)
	deskApp.SetSystemTrayMenu(menu)
}
