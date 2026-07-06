package ui

import (
	"fmt"
	"time"

	"lmvpn/internal/i18n"
	"lmvpn/internal/ipc"
	"lmvpn/internal/stats"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// showError displays a titled error dialog.
func showError(title, message string, parent fyne.Window) {
	dialog.NewCustom(title, i18n.T("BtnOK"), widget.NewLabel(message), parent).Show()
}

// buildMainWindow creates the main application window layout.
func (a *App) buildMainWindow() fyne.CanvasObject {
	// Profile selector.
	a.profileSelect = widget.NewSelect(a.profileNames(), func(sel string) {
		a.selectProfileByName(sel)
	})

	// Status display.
	a.stateLabel = widget.NewLabel(i18n.T("StateDisconnected"))
	a.stateLabel.TextStyle = fyne.TextStyle{Bold: true}
	a.ipLabel = widget.NewLabel(i18n.T("IpNone"))
	a.uptimeLabel = widget.NewLabel(i18n.T("UptimeNone"))
	a.rxLabel = widget.NewLabel(i18n.T("RxZero"))
	a.txLabel = widget.NewLabel(i18n.T("TxZero"))

	statusCard := widget.NewCard(i18n.T("StatusLabel"), "", container.NewVBox(
		a.stateLabel,
		a.ipLabel,
		a.uptimeLabel,
		container.NewHBox(a.rxLabel, a.txLabel),
	))

	// Buttons.
	a.connectBtn = widget.NewButton(i18n.T("BtnConnect"), a.onConnect)
	a.connectBtn.Importance = widget.HighImportance
	a.disconnectBtn = widget.NewButton(i18n.T("BtnDisconnect"), a.onDisconnect)
	a.disconnectBtn.Disable()

	addBtn := widget.NewButton(i18n.T("BtnAddProfile"), a.onAddProfile)
	editBtn := widget.NewButton(i18n.T("BtnEdit"), a.onEditProfile)
	deleteBtn := widget.NewButton(i18n.T("BtnDelete"), a.onDeleteProfile)

	resetDBBtn := widget.NewButton(i18n.T("BtnResetDB"), a.onResetDB)

	buttons := container.NewGridWithColumns(2,
		a.connectBtn, a.disconnectBtn,
	)
	profileButtons := container.NewGridWithColumns(3,
		addBtn, editBtn, deleteBtn,
	)

	return container.NewVBox(
		widget.NewLabel(i18n.T("ProfileLabel")),
		a.profileSelect,
		buttons,
		profileButtons,
		resetDBBtn,
		statusCard,
	)
}

// onConnect handles the Connect button click.
func (a *App) onConnect() {
	if a.currentProfile == nil {
		showError(i18n.T("DlgNoProfileTitle"), i18n.T("DlgNoProfileConnectMsg"), a.window)
		return
	}

	a.connectBtn.Disable()
	a.disconnectBtn.Enable()
	a.stateLabel.SetText(i18n.T("StateConnecting"))

	go func() {
		// Ensure daemon is running.
		client, err := ensureDaemon()
		if err != nil {
			fyne.Do(func() {
				showError(i18n.T("DlgDaemonError"), err.Error(), a.window)
				a.stateLabel.SetText(i18n.T("StateDisconnected"))
				a.connectBtn.Enable()
				a.disconnectBtn.Disable()
			})
			return
		}

		a.mu.Lock()
		a.ipcClient = client
		a.mu.Unlock()

		// Get password from keychain.
		password, err := a.kc.GetPassword(a.currentProfile.Name)
		if err != nil {
			fyne.Do(func() {
				showError(i18n.T("DlgCredentialError"),
					i18n.T("DlgCredentialErrorMsg"),
					a.window)
				a.stateLabel.SetText(i18n.T("StateDisconnected"))
				a.connectBtn.Enable()
				a.disconnectBtn.Disable()
			})
			return
		}

		p := a.currentProfile
		serverURL := p.BuildServerURL()
		sniHost := ""
		serverIPs := p.GetServerIPList()
		if len(serverIPs) > 0 {
			sniHost = p.Host
		}

		// Build and send the start command.
		cfg := ipc.ClientConfig{
			ServerURL:   serverURL,
			SNIHost:     sniHost,
			ServerIPs:   serverIPs,
			Username:    p.Username,
			Password:    password,
			AuthMode:    string(p.AuthMode),
			RoutingMode: string(p.RoutingMode),
			CustomCIDRs: splitCIDRs(p.CustomCIDRs),
			MTUOverride: p.MTUOverride,
		}
		if err := ipc.SendStart(client, cfg); err != nil {
			fyne.Do(func() {
				showError(i18n.T("DlgIPCError"), err.Error(), a.window)
				a.stateLabel.SetText(i18n.T("StateDisconnected"))
				a.connectBtn.Enable()
				a.disconnectBtn.Disable()
			})
			return
		}

		// Start the event listener.
		go a.eventLoop()
	}()
}

// onDisconnect handles the Disconnect button click.
func (a *App) onDisconnect() {
	a.mu.Lock()
	client := a.ipcClient
	a.mu.Unlock()
	if client == nil {
		return
	}
	_ = ipc.SendStop(client)
	a.disconnectBtn.Disable()
	a.connectBtn.Enable()
	a.stateLabel.SetText(i18n.T("StateDisconnected"))
}

// eventLoop reads IPC events from the daemon and updates the UI.
func (a *App) eventLoop() {
	a.mu.Lock()
	client := a.ipcClient
	a.mu.Unlock()
	if client == nil {
		return
	}

	for {
		ev, err := client.Recv()
		if err != nil {
			fyne.Do(func() {
				a.mu.Lock()
				current := a.ipcClient
				a.mu.Unlock()
				if current == client {
					a.stateLabel.SetText(i18n.T("StateDisconnected"))
					a.ipLabel.SetText(i18n.T("IpNone"))
					a.uptimeLabel.SetText(i18n.T("UptimeNone"))
					a.rxLabel.SetText(i18n.T("RxZero"))
					a.txLabel.SetText(i18n.T("TxZero"))
					a.connectBtn.Enable()
					a.disconnectBtn.Disable()
				}
			})
			return
		}
		switch ev.Event {
		case ipc.EvState:
			fyne.Do(func() {
				a.applyState(ev.State)
			})
		case ipc.EvStats:
			if ev.Stats != nil {
				s := *ev.Stats
				fyne.Do(func() {
					a.applyStats(s)
				})
			}
		case ipc.EvError:
			fyne.Do(func() {
				if ev.Message != "" {
					showError(i18n.T("DlgVPNError"), ev.Message, a.window)
				}
			})
		}
	}
}

// applyState updates UI elements for a state change.
func (a *App) applyState(state string) {
	switch stats.State(state) {
	case stats.StateConnected:
		a.stateLabel.SetText(i18n.T("StateConnected"))
		a.connectBtn.Disable()
		a.disconnectBtn.Enable()
	case stats.StateConnecting:
		a.stateLabel.SetText(i18n.T("StateConnecting"))
		a.connectBtn.Disable()
		a.disconnectBtn.Enable()
	case stats.StateReconnecting:
		a.stateLabel.SetText(i18n.T("StateReconnecting"))
		a.connectBtn.Disable()
		a.disconnectBtn.Enable()
	case stats.StateDisconnected:
		a.stateLabel.SetText(i18n.T("StateDisconnected"))
		a.ipLabel.SetText(i18n.T("IpNone"))
		a.connectBtn.Enable()
		a.disconnectBtn.Disable()
	case stats.StateError:
		a.stateLabel.SetText(i18n.T("StateError"))
		a.connectBtn.Enable()
		a.disconnectBtn.Disable()
	}
}

// applyStats updates the stats display.
func (a *App) applyStats(s stats.Snapshot) {
	if s.AssignedIP != "" {
		a.ipLabel.SetText(i18n.T("IpLabel", map[string]interface{}{"ip": s.AssignedIP}))
	}
	if s.State == stats.StateConnected {
		a.stateLabel.SetText(i18n.T("StateConnected"))
	}
	a.rxLabel.SetText(i18n.T("RxLabel", map[string]interface{}{"bytes": formatBytes(s.RxBytes)}))
	a.txLabel.SetText(i18n.T("TxLabel", map[string]interface{}{"bytes": formatBytes(s.TxBytes)}))
	if s.Uptime > 0 {
		a.uptimeLabel.SetText(i18n.T("UptimeLabel", map[string]interface{}{"uptime": formatDuration(s.Uptime)}))
	}
}

// formatBytes formats a byte count human-readably.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatDuration formats an uptime duration.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// splitCIDRs splits a comma-separated CIDR string into a slice.
func splitCIDRs(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}
