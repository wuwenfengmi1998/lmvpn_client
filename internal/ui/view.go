package ui

import (
	"fmt"
	"time"

	"lmvpn/internal/ipc"
	"lmvpn/internal/stats"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// showError displays a titled error dialog.
func showError(title, message string, parent fyne.Window) {
	dialog.NewCustom(title, "OK", widget.NewLabel(message), parent).Show()
}

// buildMainWindow creates the main application window layout.
func (a *App) buildMainWindow() fyne.CanvasObject {
	// Profile selector.
	a.profileSelect = widget.NewSelect(a.profileNames(), func(sel string) {
		a.selectProfileByName(sel)
	})

	// Status display.
	a.stateLabel = widget.NewLabel("Disconnected")
	a.stateLabel.TextStyle = fyne.TextStyle{Bold: true}
	a.ipLabel = widget.NewLabel("IP: —")
	a.uptimeLabel = widget.NewLabel("Uptime: —")
	a.rxLabel = widget.NewLabel("↓ 0 B")
	a.txLabel = widget.NewLabel("↑ 0 B")

	statusCard := widget.NewCard("Status", "", container.NewVBox(
		a.stateLabel,
		a.ipLabel,
		a.uptimeLabel,
		container.NewHBox(a.rxLabel, a.txLabel),
	))

	// Buttons.
	a.connectBtn = widget.NewButton("Connect", a.onConnect)
	a.connectBtn.Importance = widget.HighImportance
	a.disconnectBtn = widget.NewButton("Disconnect", a.onDisconnect)
	a.disconnectBtn.Disable()

	addBtn := widget.NewButton("Add Profile", a.onAddProfile)
	editBtn := widget.NewButton("Edit", a.onEditProfile)
	deleteBtn := widget.NewButton("Delete", a.onDeleteProfile)

	buttons := container.NewGridWithColumns(2,
		a.connectBtn, a.disconnectBtn,
	)
	profileButtons := container.NewGridWithColumns(3,
		addBtn, editBtn, deleteBtn,
	)

	return container.NewVBox(
		widget.NewLabel("Profile"),
		a.profileSelect,
		buttons,
		profileButtons,
		statusCard,
	)
}

// onConnect handles the Connect button click.
func (a *App) onConnect() {
	if a.currentProfile == nil {
		showError("No Profile", "Please select or create a profile first.", a.window)
		return
	}

	a.connectBtn.Disable()
	a.stateLabel.SetText("Connecting...")

	go func() {
		// Ensure daemon is running.
		client, err := ensureDaemon()
		if err != nil {
			fyne.Do(func() {
				showError("Daemon Error", err.Error(), a.window)
				a.stateLabel.SetText("Disconnected")
				a.connectBtn.Enable()
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
				showError("Credential Error",
					"No password stored for this profile. Edit the profile to set it.",
					a.window)
				a.stateLabel.SetText("Disconnected")
				a.connectBtn.Enable()
			})
			return
		}

		// Build and send the start command.
		cfg := ipc.ClientConfig{
			ServerURL:   a.currentProfile.ServerURL,
			Username:    a.currentProfile.Username,
			Password:    password,
			AuthMode:    string(a.currentProfile.AuthMode),
			RoutingMode: string(a.currentProfile.RoutingMode),
			CustomCIDRs: splitCIDRs(a.currentProfile.CustomCIDRs),
			MTUOverride: a.currentProfile.MTUOverride,
		}
		if err := ipc.SendStart(client, cfg); err != nil {
			fyne.Do(func() {
				showError("IPC Error", err.Error(), a.window)
				a.stateLabel.SetText("Disconnected")
				a.connectBtn.Enable()
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
	a.stateLabel.SetText("Disconnected")
}

// eventLoop reads IPC events from the daemon and updates the UI.
func (a *App) eventLoop() {
	for {
		a.mu.Lock()
		client := a.ipcClient
		a.mu.Unlock()
		if client == nil {
			return
		}
		ev, err := client.Recv()
		if err != nil {
			fyne.Do(func() {
				a.stateLabel.SetText("Disconnected")
				a.ipLabel.SetText("IP: —")
				a.uptimeLabel.SetText("Uptime: —")
				a.rxLabel.SetText("↓ 0 B")
				a.txLabel.SetText("↑ 0 B")
				a.connectBtn.Enable()
				a.disconnectBtn.Disable()
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
					showError("VPN Error", ev.Message, a.window)
				}
			})
		}
	}
}

// applyState updates UI elements for a state change.
func (a *App) applyState(state string) {
	switch stats.State(state) {
	case stats.StateConnected:
		a.stateLabel.SetText("Connected")
		a.connectBtn.Disable()
		a.disconnectBtn.Enable()
	case stats.StateConnecting:
		a.stateLabel.SetText("Connecting...")
		a.connectBtn.Disable()
		a.disconnectBtn.Enable()
	case stats.StateReconnecting:
		a.stateLabel.SetText("Reconnecting...")
		a.disconnectBtn.Enable()
	case stats.StateDisconnected:
		a.stateLabel.SetText("Disconnected")
		a.ipLabel.SetText("IP: —")
		a.connectBtn.Enable()
		a.disconnectBtn.Disable()
	case stats.StateError:
		a.stateLabel.SetText("Error")
		a.connectBtn.Enable()
		a.disconnectBtn.Disable()
	}
}

// applyStats updates the stats display.
func (a *App) applyStats(s stats.Snapshot) {
	if s.AssignedIP != "" {
		a.ipLabel.SetText("IP: " + s.AssignedIP)
	}
	if s.State == stats.StateConnected {
		a.stateLabel.SetText("Connected")
	}
	a.rxLabel.SetText("↓ " + formatBytes(s.RxBytes))
	a.txLabel.SetText("↑ " + formatBytes(s.TxBytes))
	if s.Uptime > 0 {
		a.uptimeLabel.SetText("Uptime: " + formatDuration(s.Uptime))
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
