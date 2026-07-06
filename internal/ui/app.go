package ui

import (
	"sync"

	"lmvpn/internal/config"
	"lmvpn/internal/db"
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
	fyneApp fyne.App
	db      *db.Store
	kc      keychain.Store
	window  fyne.Window

	// UI widgets
	profileSelect *widget.Select
	stateLabel    *widget.Label
	ipLabel       *widget.Label
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

	a := &App{
		fyneApp: app.NewWithID(paths.BundleID),
		db:      store,
		kc:      keychain.New(),
	}

	a.window = a.fyneApp.NewWindow("LMVPN")
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
		dialog.ShowInformation("No Profile", "Select a profile to edit.", a.window)
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
	dialog.ShowConfirm("Delete Profile",
		"Delete profile \""+name+"\" and its stored credentials?",
		func(ok bool) {
			if !ok {
				return
			}
			if err := a.db.DeleteProfile(a.currentProfile.ID); err != nil {
				showError("Error", err.Error(), a.window)
				return
			}
			_ = a.kc.DeleteAll(name)
			a.loadProfiles()
		}, a.window)
}
