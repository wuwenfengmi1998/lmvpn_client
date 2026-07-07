package ui

import (
	_ "embed"
	"fmt"
	"net/url"
	"time"

	"lmvpn/internal/i18n"
	"lmvpn/internal/ipc"
	"lmvpn/internal/stats"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed github.svg
var githubIconBytes []byte

// showError displays a titled error dialog.
func showError(title, message string, parent fyne.Window) {
	dialog.NewCustom(title, i18n.T("BtnOK"), widget.NewLabel(message), parent).Show()
}

// setConnButtons enables/disables the connect and disconnect buttons
// and refreshes the tray menu to match.
func (a *App) setConnButtons(connectEnabled, disconnectEnabled bool) {
	if connectEnabled {
		a.connectBtn.Enable()
	} else {
		a.connectBtn.Disable()
	}
	if disconnectEnabled {
		a.disconnectBtn.Enable()
	} else {
		a.disconnectBtn.Disable()
	}
	a.setupTray()
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
	a.ip6Label = widget.NewLabel(i18n.T("Ip6None"))
	a.uptimeLabel = widget.NewLabel(i18n.T("UptimeNone"))
	a.rxV4Label = widget.NewLabel(i18n.T("RxV4Zero"))
	a.txV4Label = widget.NewLabel(i18n.T("TxV4Zero"))
	a.rxV6Label = widget.NewLabel(i18n.T("RxV6Zero"))
	a.txV6Label = widget.NewLabel(i18n.T("TxV6Zero"))
	a.rxTotalLabel = widget.NewLabel(i18n.T("RxTotalZero"))
	a.txTotalLabel = widget.NewLabel(i18n.T("TxTotalZero"))

	statusCard := widget.NewCard(i18n.T("StatusLabel"), "", container.NewVBox(
		a.stateLabel,
		a.ipLabel,
		a.ip6Label,
		a.uptimeLabel,
		container.NewHBox(a.rxV4Label, a.txV4Label),
		container.NewHBox(a.rxV6Label, a.txV6Label),
		container.NewHBox(a.rxTotalLabel, a.txTotalLabel),
	))

	// Buttons.
	a.connectBtn = widget.NewButton(i18n.T("BtnConnect"), a.onConnect)
	a.connectBtn.Importance = widget.HighImportance
	a.disconnectBtn = widget.NewButton(i18n.T("BtnDisconnect"), a.onDisconnect)
	a.disconnectBtn.Disable()

	profileListBtn := widget.NewButton(i18n.T("BtnProfileList"), a.showProfileListWindow)

	buttons := container.NewGridWithColumns(2,
		a.connectBtn, a.disconnectBtn,
	)

	githubBtn := widget.NewButtonWithIcon("", theme.NewThemedResource(fyne.NewStaticResource("github.svg", githubIconBytes)), func() {
		u, err := url.Parse("https://github.com/wuwenfengmi1998/lmvpn_client")
		if err != nil {
			return
		}
		_ = a.fyneApp.OpenURL(u)
	})

	content := container.NewVBox(
		widget.NewLabel(i18n.T("ProfileLabel")),
		a.profileSelect,
		buttons,
		profileListBtn,
		statusCard,
	)

	bottomBar := container.NewHBox(
		widget.NewLabel(fmt.Sprintf("v%s", Version)),
		layout.NewSpacer(),
		githubBtn,
	)

	return container.NewBorder(nil, bottomBar, nil, nil, content)
}

// onConnect handles the Connect button click.
func (a *App) onConnect() {
	if a.currentProfile == nil {
		showError(i18n.T("DlgNoProfileTitle"), i18n.T("DlgNoProfileConnectMsg"), a.window)
		return
	}

	a.setConnButtons(false, true)
	a.stateLabel.SetText(i18n.T("StateConnecting"))

	go func() {
		// Ensure daemon is running.
		client, err := ensureDaemon()
		if err != nil {
			fyne.Do(func() {
				showError(i18n.T("DlgDaemonError"), err.Error(), a.window)
				a.stateLabel.SetText(i18n.T("StateDisconnected"))
				a.setConnButtons(true, false)
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
				a.setConnButtons(true, false)
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
				a.setConnButtons(true, false)
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
	a.setConnButtons(true, false)
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
					a.ip6Label.SetText(i18n.T("Ip6None"))
					a.uptimeLabel.SetText(i18n.T("UptimeNone"))
					a.rxV4Label.SetText(i18n.T("RxV4Zero"))
					a.txV4Label.SetText(i18n.T("TxV4Zero"))
					a.rxV6Label.SetText(i18n.T("RxV6Zero"))
					a.txV6Label.SetText(i18n.T("TxV6Zero"))
					a.rxTotalLabel.SetText(i18n.T("RxTotalZero"))
					a.txTotalLabel.SetText(i18n.T("TxTotalZero"))
					a.setConnButtons(true, false)
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
				msg := authErrorMessage(ev.Code, ev.Message)
				if msg != "" {
					showError(i18n.T("DlgAuthError"), msg, a.window)
				}
			})
		}
	}
}

// authErrorMessage maps a stable auth-error code (carried by the EvError
// IPC event from the daemon) to a localized user-facing string. For an
// unknown or empty code it falls back to the raw server message.
func authErrorMessage(code, fallback string) string {
	switch code {
	case "wrong_credentials":
		return i18n.T("AuthErrWrongCredentials")
	case "user_disabled":
		return i18n.T("AuthErrUserDisabled")
	case "token_invalid":
		return i18n.T("AuthErrTokenInvalid")
	case "rate_limited":
		return i18n.T("AuthErrRateLimited")
	case "malformed":
		return i18n.T("AuthErrMalformed")
	default:
		return fallback
	}
}

// applyState updates UI elements for a state change.
func (a *App) applyState(state string) {
	switch stats.State(state) {
	case stats.StateConnected:
		a.stateLabel.SetText(i18n.T("StateConnected"))
		a.setConnButtons(false, true)
	case stats.StateConnecting:
		a.stateLabel.SetText(i18n.T("StateConnecting"))
		a.setConnButtons(false, true)
	case stats.StateReconnecting:
		a.stateLabel.SetText(i18n.T("StateReconnecting"))
		a.setConnButtons(false, true)
	case stats.StateDisconnected:
		a.stateLabel.SetText(i18n.T("StateDisconnected"))
		a.ipLabel.SetText(i18n.T("IpNone"))
		a.ip6Label.SetText(i18n.T("Ip6None"))
		a.setConnButtons(true, false)
	case stats.StateError:
		a.stateLabel.SetText(i18n.T("StateError"))
		a.setConnButtons(true, false)
	}
}

// applyStats updates the stats display.
func (a *App) applyStats(s stats.Snapshot) {
	if s.AssignedIP != "" {
		a.ipLabel.SetText(i18n.T("IpLabel", map[string]interface{}{"ip": s.AssignedIP}))
	}
	if s.AssignedIP6 != "" {
		a.ip6Label.SetText(i18n.T("Ip6Label", map[string]interface{}{"ip": s.AssignedIP6}))
	} else {
		a.ip6Label.SetText(i18n.T("Ip6None"))
	}
	if s.State == stats.StateConnected {
		a.stateLabel.SetText(i18n.T("StateConnected"))
	}
	a.rxV4Label.SetText(i18n.T("RxV4Label", map[string]interface{}{
		"bytes": formatBytes(s.RxBytesV4), "speed": formatSpeed(s.RxSpeedV4),
	}))
	a.txV4Label.SetText(i18n.T("TxV4Label", map[string]interface{}{
		"bytes": formatBytes(s.TxBytesV4), "speed": formatSpeed(s.TxSpeedV4),
	}))
	a.rxV6Label.SetText(i18n.T("RxV6Label", map[string]interface{}{
		"bytes": formatBytes(s.RxBytesV6), "speed": formatSpeed(s.RxSpeedV6),
	}))
	a.txV6Label.SetText(i18n.T("TxV6Label", map[string]interface{}{
		"bytes": formatBytes(s.TxBytesV6), "speed": formatSpeed(s.TxSpeedV6),
	}))
	a.rxTotalLabel.SetText(i18n.T("RxTotalLabel", map[string]interface{}{
		"bytes": formatBytes(s.RxBytes), "speed": formatSpeed(s.RxSpeed),
	}))
	a.txTotalLabel.SetText(i18n.T("TxTotalLabel", map[string]interface{}{
		"bytes": formatBytes(s.TxBytes), "speed": formatSpeed(s.TxSpeed),
	}))
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

// formatSpeed formats a bytes/sec rate as a decimal bits/sec rate
// (kbps / Mbps / Gbps). bps is the byte rate; it is converted to bits
// by multiplying by 8 and scaled by 1000 (decimal, network convention).
func formatSpeed(bps int64) string {
	const unit = 1000
	bits := bps * 8
	if bits < unit {
		return fmt.Sprintf("%d bps", bits)
	}
	div, exp := int64(unit), 0
	for n := bits / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cbps", float64(bits)/float64(div), "kMGTPE"[exp])
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
