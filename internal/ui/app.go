package ui

import (
	"errors"
	"os"
	"sync"

	"lmvpn/internal/config"
	"lmvpn/internal/db"
	"lmvpn/internal/i18n"
	"lmvpn/internal/instance"
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

	// Profile list window.
	profileListWindow fyne.Window
	profileList       *widget.List
	listSelectedIndex int

	// UI widgets
	profileSelect *widget.Select
	stateLabel    *widget.Label
	ipLabel       *widget.Label
	ip6Label      *widget.Label
	uptimeLabel   *widget.Label
	rxV4Label     *widget.Label
	txV4Label     *widget.Label
	rxV6Label     *widget.Label
	txV6Label     *widget.Label
	rxTotalLabel  *widget.Label
	txTotalLabel  *widget.Label
	routingModeLabel *widget.Label
	cidrV4Label      *widget.Label
	cidrV6Label      *widget.Label
	connectBtn    *widget.Button
	disconnectBtn *widget.Button

	// State
	mu              sync.Mutex
	ipcClient       *ipc.Client
	profiles        []model.ServerProfile
	currentProfile  *model.ServerProfile
	defaultProfileID int64
	langSetting     string
	windowHidden    bool
}

// Run initialises and starts the GUI application.
func Run() {
	// Ensure platform directories exist.
	if err := paths.EnsureDirs(); err != nil {
		log.L().Error("ensure dirs", "error", err)
	}

	// Logging.
	log.Init(log.RoleGUI, paths.LogFile())

	// Single-instance enforcement: the first instance holds the lock;
	// a second instance signals "focus" to the first and exits.
	focusCh, err := instance.Acquire()
	if err != nil {
		if errors.Is(err, instance.ErrAlreadyRunning) {
			log.L().Info("another instance is running, exiting")
			os.Exit(0)
		}
		log.L().Warn("instance lock failed, continuing", "error", err)
	}

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
		fyneApp:           app.NewWithID(paths.BundleID),
		db:                store,
		kc:                keychain.New(),
		langSetting:       cfg.Language,
		defaultProfileID:  cfg.DefaultProfileID,
		listSelectedIndex: -1,
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

	// Listen for "focus" signals from second instances and bring the
	// window to the front.
	if focusCh != nil {
		go func() {
			for range focusCh {
				fyne.Do(func() {
					a.windowHidden = false
					activateApp()
					showDockIcon()
					a.window.Show()
					a.window.RequestFocus()
				})
			}
		}()
	}

	// Register a Cocoa handler so that re-opening the .app bundle
	// (which does NOT start a new process on macOS) brings the hidden
	// window back.  On other platforms this is a no-op.  This must be
	// deferred until after the Fyne/GLFW event loop has started,
	// because GLFWApplicationDelegate is not created until glfw.Init()
	// runs inside runGL().
	a.fyneApp.Lifecycle().SetOnStarted(func() {
		onAppActive = func() {
			if a.windowHidden {
				a.windowHidden = false
				activateApp()
				showDockIcon()
				fyne.Do(func() {
					if a.windowHidden {
						return
					}
					a.window.Show()
					a.window.RequestFocus()
				})
			}
		}
		registerReopenHandler()
	})

	// Auto-connect if configured.
	if cfg.AutoConnect && a.currentProfile != nil {
		go func() {
			// Small delay to let window render.
			a.onConnect()
		}()
	}

	a.window.SetCloseIntercept(func() {
		if cfg.CloseToTray {
			a.windowHidden = true
			hideDockIcon()
			a.window.Hide()
		} else {
			a.quit()
		}
	})

	a.window.ShowAndRun()
}

// quit disconnects any active VPN session (via the daemon) and then
// quits the application. The daemon process itself remains running.
func (a *App) quit() {
	a.mu.Lock()
	client := a.ipcClient
	a.ipcClient = nil
	a.mu.Unlock()
	if client != nil {
		_ = ipc.SendStop(client)
	}
	a.fyneApp.Quit()
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
		selected := false
		if a.defaultProfileID > 0 {
			for i := range a.profiles {
				if a.profiles[i].ID == a.defaultProfileID {
					a.profileSelect.SetSelectedIndex(i)
					a.selectProfileByName(a.profiles[i].Name)
					selected = true
					break
				}
			}
		}
		if !selected {
			a.profileSelect.SetSelectedIndex(0)
			a.selectProfileByName(names[0])
		}
	} else {
		a.currentProfile = nil
		a.profileSelect.SetSelected("")
		a.profileSelect.Refresh()
	}
	a.listSelectedIndex = -1
	a.refreshProfileList()
}

// refreshProfileList refreshes the profile list widget if the profile
// list window is currently open.
func (a *App) refreshProfileList() {
	if a.profileList != nil {
		a.profileList.Refresh()
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

// saveDefaultProfile persists the currently selected profile ID to the
// config file so it can be restored on the next launch.
func (a *App) saveDefaultProfile() {
	if a.currentProfile == nil {
		return
	}
	a.defaultProfileID = a.currentProfile.ID
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	if cfg.DefaultProfileID == a.currentProfile.ID {
		return
	}
	cfg.DefaultProfileID = a.currentProfile.ID
	if err := config.Save(cfg); err != nil {
		log.L().Error("save default profile", "error", err)
	}
}

// onAddProfile shows a dialog to create a new profile.
func (a *App) onAddProfile() {
	a.showProfileDialog(nil)
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
		a.rxV4Label.SetText(i18n.T("RxV4Zero"))
		a.txV4Label.SetText(i18n.T("TxV4Zero"))
		a.rxV6Label.SetText(i18n.T("RxV6Zero"))
		a.txV6Label.SetText(i18n.T("TxV6Zero"))
		a.rxTotalLabel.SetText(i18n.T("RxTotalZero"))
		a.txTotalLabel.SetText(i18n.T("TxTotalZero"))
		a.setConnButtons(true, false)
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

	// Close the profile list window if open; it holds cached strings
	// and will be recreated with the new language on next open.
	if a.profileListWindow != nil {
		a.profileListWindow.Close()
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
		a.setConnButtons(false, true)
		a.stateLabel.SetText(i18n.T("StateConnected"))
	} else {
		a.setConnButtons(true, false)
		a.stateLabel.SetText(i18n.T("StateDisconnected"))
	}

	// Rebuild the tray menu (new labels + checked language item).
	a.setupTray()
}
