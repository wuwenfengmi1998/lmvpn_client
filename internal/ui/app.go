package ui

import (
	"os"
	"sync"

	"lmvpn/internal/config"
	"lmvpn/internal/db"
	"lmvpn/internal/i18n"
	"lmvpn/internal/ipc"
	"lmvpn/internal/keychain"
	"lmvpn/internal/log"
	"lmvpn/internal/model"
	"lmvpn/internal/paths"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// App is the GUI application controller.
type App struct {
	fyneApp       fyne.App
	db            *db.Store
	kc            keychain.Store
	window        fyne.Window
	profileWindow fyne.Window

	// UI widgets
	profileSelect *widget.Select
	stateLabel    *widget.Label
	ipLabel       *widget.Label
	ip6Label      *widget.Label
	uptimeLabel   *widget.Label
	rxLabel       *widget.Label
	txLabel       *widget.Label
	connectBtn    *widget.Button
	disconnectBtn *widget.Button

	// State
	mu             sync.Mutex
	ipcClient      *ipc.Client
	profiles       []model.ServerProfile
	currentProfile *model.ServerProfile
	langSetting    string
}

// Run initialises and starts the GUI application.
func Run() {
	// Ensure platform directories exist.
	if err := paths.EnsureDirs(); err != nil {
		log.L().Error("ensure dirs", "error", err)
	}

	// Logging.
	log.Init(log.RoleGUI, paths.LogFile())

	// Database.
	store, err := db.Open()
	if err != nil {
		log.L().Error("open db", "error", err)
	}

	// Load app config.
	cfg, _ := config.Load()

	// Initialise i18n (detect system locale when unset).
	if err := i18n.Init(cfg.Language); err != nil {
		log.L().Error("init i18n", "error", err)
	}

	a := &App{
		fyneApp:     app.NewWithID(paths.BundleID),
		db:          store,
		kc:          keychain.New(),
		langSetting: cfg.Language,
	}

	a.window = a.fyneApp.NewWindow(i18n.T("WindowTitle"))
	a.window.SetContent(a.buildMainWindow())
	a.window.Resize(fyne.NewSize(420, 480))

	// Load profiles.
	if store != nil {
		a.loadProfiles()
	}

	// System tray.
	a.setupTray()

	// Auto-connect if configured.
	if cfg.AutoConnect && a.currentProfile != nil {
		go func() {
			// Small delay to let window render.
			a.onConnect()
		}()
	}

	a.window.SetCloseIntercept(func() {
		if cfg.CloseToTray {
			hideDockIcon()
			a.window.Hide()
		} else {
			a.fyneApp.Quit()
		}
	})

	a.window.ShowAndRun()
}

// loadProfiles loads all profiles from the database and populates the
// selector.
func (a *App) loadProfiles() {
	profiles, err := a.db.ListProfiles()
	if err != nil {
		log.L().Error("list profiles", "error", err)
		return
	}
	a.profiles = profiles
	names := a.profileNames()
	a.profileSelect.Options = names
	if len(names) > 0 {
		a.profileSelect.SetSelectedIndex(0)
		a.selectProfileByName(names[0])
	} else {
		a.currentProfile = nil
		a.profileSelect.SetSelected("")
		a.profileSelect.Refresh()
	}
}

// profileNames returns the names of all loaded profiles.
func (a *App) profileNames() []string {
	names := make([]string, len(a.profiles))
	for i, p := range a.profiles {
		names[i] = p.Name
	}
	return names
}

// selectProfileByName sets the current profile by name.
func (a *App) selectProfileByName(name string) {
	for i := range a.profiles {
		if a.profiles[i].Name == name {
			a.currentProfile = &a.profiles[i]
			return
		}
	}
}

// onAddProfile shows a dialog to create a new profile.
func (a *App) onAddProfile() {
	a.showProfileDialog(nil)
}

// onEditProfile shows a dialog to edit the current profile.
func (a *App) onEditProfile() {
	if a.currentProfile == nil {
		dialog.NewCustom(i18n.T("DlgNoProfileTitle"), i18n.T("BtnOK"),
			widget.NewLabel(i18n.T("DlgNoProfileEditMsg")), a.window).Show()
		return
	}
	p := *a.currentProfile
	a.showProfileDialog(&p)
}

// onDeleteProfile deletes the current profile after confirmation.
func (a *App) onDeleteProfile() {
	if a.currentProfile == nil {
		return
	}
	name := a.currentProfile.Name
	dialog.NewCustomConfirm(i18n.T("DlgDeleteProfileTitle"),
		i18n.T("BtnDelete"), i18n.T("BtnCancel"),
		widget.NewLabel(i18n.T("DlgDeleteProfileMsg", map[string]interface{}{"name": name})),
		func(ok bool) {
			if !ok {
				return
			}
			if err := a.db.DeleteProfile(a.currentProfile.ID); err != nil {
				showError(i18n.T("DlgError"), err.Error(), a.window)
				return
			}
			_ = a.kc.DeleteAll(name)
			a.loadProfiles()
		}, a.window).Show()
}

// onResetDB deletes the SQLite database file after confirmation,
// then re-creates it. All profiles, credentials, and logs are lost.
func (a *App) onResetDB() {
	dialog.NewCustomConfirm(i18n.T("DlgResetDBTitle"),
		i18n.T("BtnDelete"), i18n.T("BtnCancel"),
		widget.NewLabel(i18n.T("DlgResetDBMsg")),
		func(ok bool) {
			if !ok {
				return
			}

			// Disconnect if connected.
			a.mu.Lock()
			client := a.ipcClient
			a.mu.Unlock()
			if client != nil {
				_ = ipc.SendStop(client)
			}

			// Clear keychain entries for all profiles.
			for _, p := range a.profiles {
				_ = a.kc.DeleteAll(p.Name)
			}

			// Close and delete database.
			if a.db != nil {
				a.db.Close()
			}
			if err := os.Remove(paths.DBPath()); err != nil {
				showError(i18n.T("DlgError"), err.Error(), a.window)
				return
			}

			// Re-open (auto-creates new database).
			store, err := db.Open()
			if err != nil {
				showError(i18n.T("DlgError"), err.Error(), a.window)
				return
			}
			a.db = store

			// Reset UI state.
			a.currentProfile = nil
			a.loadProfiles()
			a.stateLabel.SetText(i18n.T("StateDisconnected"))
			a.ipLabel.SetText(i18n.T("IpNone"))
			a.ip6Label.SetText(i18n.T("Ip6None"))
			a.uptimeLabel.SetText(i18n.T("UptimeNone"))
			a.rxLabel.SetText(i18n.T("RxZero"))
			a.txLabel.SetText(i18n.T("TxZero"))
			a.connectBtn.Enable()
			a.disconnectBtn.Disable()
		}, a.window).Show()
}

// changeLanguage switches the active language, persists the choice to
// the config file, and rebuilds the UI so the new strings take effect
// immediately.
func (a *App) changeLanguage(lang string) {
	a.langSetting = lang

	// Persist the choice.
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	cfg.Language = lang
	if err := config.Save(cfg); err != nil {
		log.L().Error("save config", "error", err)
	}

	// Switch the active localizer.
	i18n.SetLanguage(lang)

	// Rebuild everything that holds cached strings.
	a.rebuildUI()
}

// rebuildUI recreates the main window content and system tray menu so
// that all labels pick up the currently active language. It preserves
// the selected profile and connection state across the rebuild.
func (a *App) rebuildUI() {
	// Snapshot the state we need to restore.
	a.mu.Lock()
	wasConnected := a.ipcClient != nil
	a.mu.Unlock()

	selectedName := ""
	if a.currentProfile != nil {
		selectedName = a.currentProfile.Name
	}

	// Rebuild window content (creates fresh widgets with new strings).
	a.window.SetTitle(i18n.T("WindowTitle"))
	a.window.SetContent(a.buildMainWindow())

	// Restore profiles and the previous selection.
	if a.db != nil {
		a.loadProfiles()
		if selectedName != "" {
			for _, name := range a.profileSelect.Options {
				if name == selectedName {
					a.profileSelect.SetSelected(selectedName)
					a.selectProfileByName(selectedName)
					break
				}
			}
		}
	}

	// Restore connection state.
	if wasConnected {
		a.connectBtn.Disable()
		a.disconnectBtn.Enable()
		a.stateLabel.SetText(i18n.T("StateConnected"))
	} else {
		a.connectBtn.Enable()
		a.disconnectBtn.Disable()
		a.stateLabel.SetText(i18n.T("StateDisconnected"))
	}

	// Rebuild the tray menu (new labels + checked language item).
	a.setupTray()
}
