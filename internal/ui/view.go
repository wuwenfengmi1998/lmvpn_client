package ui

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"lmvpn/internal/i18n"
	"lmvpn/internal/ipc"
	"lmvpn/internal/keychain"
	"lmvpn/internal/model"
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
	if a.profileSelect != nil {
		if connectEnabled {
			a.profileSelect.Enable()
		} else {
			a.profileSelect.Disable()
		}
	}
	a.setupTray()
}

// buildMainWindow creates the main application window layout.
func (a *App) buildMainWindow() fyne.CanvasObject {
	// Profile selector.
	a.profileSelect = widget.NewSelect(a.profileNames(), func(sel string) {
		a.selectProfileByName(sel)
		a.saveDefaultProfile()
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
	a.routingModeLabel = widget.NewLabel("")
	a.routingModeLabel.Hide()
	a.cidrV4Label = widget.NewLabel("")
	a.cidrV4Label.Hide()
	a.cidrV6Label = widget.NewLabel("")
	a.cidrV6Label.Hide()

	a.refreshCIDRBtn = widget.NewButton(i18n.T("BtnRefreshCIDR"), a.onRefreshCIDR)
	a.refreshCIDRBtn.Hide()

	statusCard := widget.NewCard(i18n.T("StatusLabel"), "", container.NewVBox(
		a.stateLabel,
		a.ipLabel,
		a.ip6Label,
		a.uptimeLabel,
		container.NewHBox(a.rxV4Label, a.txV4Label),
		container.NewHBox(a.rxV6Label, a.txV6Label),
		container.NewHBox(a.rxTotalLabel, a.txTotalLabel),
		a.routingModeLabel,
		a.cidrV4Label,
		a.cidrV6Label,
		a.refreshCIDRBtn,
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
		oldClient := a.ipcClient
		a.ipcClient = client
		a.mu.Unlock()
		if oldClient != nil {
			// Do NOT send SendStop on the old connection. The daemon's
			// startSession already calls stopSessionLocked() which
			// tears down any existing session. Sending SendStop here
			// races with SendStart on the new connection - the daemon
			// processes each IPC connection in a separate goroutine,
			// so a late SendStop could kill the newly started session.
			oldClient.Close()
		}

		// Get password from keychain.
		password, err := a.kc.GetPassword(a.currentProfile.Name)
		if err != nil {
			fyne.Do(func() {
				if errors.Is(err, keychain.ErrUserCanceled) {
					// User canceled the Touch ID / password prompt.
					a.stateLabel.SetText(i18n.T("StateDisconnected"))
					a.setConnButtons(true, false)
				} else {
					showError(i18n.T("DlgCredentialError"),
						i18n.T("DlgCredentialErrorMsg"),
						a.window)
					a.stateLabel.SetText(i18n.T("StateDisconnected"))
					a.setConnButtons(true, false)
				}
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
			ServerURL:     serverURL,
			SNIHost:       sniHost,
			ServerIPs:     serverIPs,
			Username:      p.Username,
			Password:      password,
			AuthMode:      string(p.AuthMode),
			RoutingMode:   string(p.RoutingMode),
			CIDRV4:        model.SplitCIDRs(p.CIDRV4),
			CIDRV6:        model.SplitCIDRs(p.CIDRV6),
			CIDRV4URLs:    parseIPCCIDRURLs(p.CIDRV4URLs),
			CIDRV6URLs:    parseIPCCIDRURLs(p.CIDRV6URLs),
			MTUOverride:   p.MTUOverride,
			TLSCACert:     p.TLSCACert,
			TLSCAPath:     p.TLSCAPath,
			TLSInsecure:   p.TLSInsecure,
			TLSPinnedHash: p.TLSPinnedHash,
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

// onRefreshCIDR handles the Refresh CIDR button click.
func (a *App) onRefreshCIDR() {
	a.mu.Lock()
	client := a.ipcClient
	a.mu.Unlock()
	if client == nil {
		return
	}
	a.refreshCIDRBtn.Disable()
	go func() {
		_ = ipc.SendRefreshCIDR(client)
		time.Sleep(2 * time.Second)
		fyne.Do(func() {
			a.refreshCIDRBtn.Enable()
		})
	}()
}

// onDisconnect handles the Disconnect button click.
func (a *App) onDisconnect() {
	a.mu.Lock()
	client := a.ipcClient
	a.ipcClient = nil
	a.mu.Unlock()
	if client == nil {
		return
	}
	_ = ipc.SendStop(client)
	client.Close()
	a.setConnButtons(true, false)
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
	a.routingModeLabel.Hide()
	a.cidrV4Label.Hide()
	a.cidrV6Label.Hide()
	a.refreshCIDRBtn.Hide()
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
					a.routingModeLabel.Hide()
					a.cidrV4Label.Hide()
					a.cidrV6Label.Hide()
					a.refreshCIDRBtn.Hide()
					a.setConnButtons(true, false)
				}
			})
			return
		}
		switch ev.Event {
		case ipc.EvState:
			fyne.Do(func() {
				a.mu.Lock()
				current := a.ipcClient
				a.mu.Unlock()
				if current == client {
					a.applyStateWithStep(ev.State, ev.ConnectStep)
				}
			})
		case ipc.EvStats:
			if ev.Stats != nil {
				s := *ev.Stats
				fyne.Do(func() {
					a.mu.Lock()
					current := a.ipcClient
					a.mu.Unlock()
					if current == client {
						a.applyStats(s)
					}
				})
			}
		case ipc.EvError:
			fyne.Do(func() {
				a.mu.Lock()
				current := a.ipcClient
				a.mu.Unlock()
				if current != client {
					return
				}
				if ev.Code == "tls_error" {
					showError(i18n.T("DlgTLSError"),
						i18n.T("TLSErrorVerification")+"\n\n"+ev.Message, a.window)
				} else {
					msg := authErrorMessage(ev.Code, ev.Message)
					if msg != "" {
						showError(i18n.T("DlgAuthError"), msg, a.window)
					}
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
	a.applyStateWithStep(state, "")
}

// applyStateWithStep updates UI elements for a state change, optionally
// showing a connection step description (e.g. "fetching CIDR lists").
func (a *App) applyStateWithStep(state, step string) {
	stepLabel := connectStepLabel(step)
	switch stats.State(state) {
	case stats.StateConnected:
		if stepLabel != "" {
			a.stateLabel.SetText(i18n.T("StateConnected") + " (" + stepLabel + ")")
		} else {
			a.stateLabel.SetText(i18n.T("StateConnected"))
		}
		a.setConnButtons(false, true)
	case stats.StateConnecting:
		if stepLabel != "" {
			a.stateLabel.SetText(i18n.T("StateConnecting") + " (" + stepLabel + ")")
		} else {
			a.stateLabel.SetText(i18n.T("StateConnecting"))
		}
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
		stepLabel := connectStepLabel(s.ConnectStep)
		if stepLabel != "" {
			a.stateLabel.SetText(i18n.T("StateConnected") + " (" + stepLabel + ")")
		} else {
			a.stateLabel.SetText(i18n.T("StateConnected"))
		}
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

	// Routing mode + CIDR hit statistics.
	if s.RoutingMode != "" {
		modeLabel := routeModeLabel(s.RoutingMode)
		if s.RouteLoading {
			modeLabel += " (" + i18n.T("RouteLoading") + ")"
		}
		a.routingModeLabel.SetText(i18n.T("StatusRoutingMode", map[string]interface{}{"mode": modeLabel}))
		a.routingModeLabel.Show()

		// Show CIDR error if present.
		if s.CIDRError != "" {
			a.cidrV4Label.SetText("IPv4 CIDR: " + i18n.T("CIDRFetchError"))
			a.cidrV6Label.SetText("IPv6 CIDR: " + i18n.T("CIDRFetchError"))
			a.cidrV4Label.Show()
			a.cidrV6Label.Show()
			a.refreshCIDRBtn.Show()
		} else {
			switch s.RoutingMode {
			case "proxy":
				if s.RouteLoading {
					a.cidrV4Label.SetText(fmt.Sprintf("IPv4 CIDR: %d/%d (%s)",
						s.CIDRV4Hits, s.CIDRV4Total, i18n.T("CIDRLoading")))
					a.cidrV6Label.SetText(fmt.Sprintf("IPv6 CIDR: %d/%d (%s)",
						s.CIDRV6Hits, s.CIDRV6Total, i18n.T("CIDRLoading")))
				} else {
					a.cidrV4Label.SetText(fmt.Sprintf("IPv4 CIDR: %d/%d %s",
						s.CIDRV4Hits, s.CIDRV4Total, i18n.T("CIDRHit")))
					a.cidrV6Label.SetText(fmt.Sprintf("IPv6 CIDR: %d/%d %s",
						s.CIDRV6Hits, s.CIDRV6Total, i18n.T("CIDRHit")))
				}
				a.cidrV4Label.Show()
				a.cidrV6Label.Show()
				a.refreshCIDRBtn.Show()
			case "bypass":
				if s.RouteLoading {
					a.cidrV4Label.SetText(fmt.Sprintf("IPv4 CIDR: %d/%d (%s)",
						s.CIDRV4Hits, s.CIDRV4Total, i18n.T("CIDRLoading")))
					a.cidrV6Label.SetText(fmt.Sprintf("IPv6 CIDR: %d/%d (%s)",
						s.CIDRV6Hits, s.CIDRV6Total, i18n.T("CIDRLoading")))
				} else {
					a.cidrV4Label.SetText(fmt.Sprintf("IPv4 CIDR: %d %s | %s: %d %s",
						s.CIDRV4Total, i18n.T("CIDRConfigured"),
						i18n.T("CIDRUnmatched"), s.CIDRV4Hits, i18n.T("CIDRDestinations")))
					a.cidrV6Label.SetText(fmt.Sprintf("IPv6 CIDR: %d %s | %s: %d %s",
						s.CIDRV6Total, i18n.T("CIDRConfigured"),
						i18n.T("CIDRUnmatched"), s.CIDRV6Hits, i18n.T("CIDRDestinations")))
				}
				a.cidrV4Label.Show()
				a.cidrV6Label.Show()
				a.refreshCIDRBtn.Show()
			default:
				a.cidrV4Label.Hide()
				a.cidrV6Label.Hide()
				a.refreshCIDRBtn.Hide()
			}
		}
	} else {
		a.routingModeLabel.Hide()
		a.cidrV4Label.Hide()
		a.cidrV6Label.Hide()
		a.refreshCIDRBtn.Hide()
	}
}

// routeModeLabel returns the localised display name for a routing mode
// code string.
func routeModeLabel(code string) string {
	switch code {
	case "full":
		return i18n.T("RoutingModeFull")
	case "proxy":
		return i18n.T("RoutingModeProxy")
	case "bypass":
		return i18n.T("RoutingModeBypass")
	default:
		return code
	}
}

// connectStepLabel translates a connection step code to a localised
// display string. Returns "" for unknown/empty steps.
func connectStepLabel(step string) string {
	switch step {
	case "fetch_cidrs":
		return i18n.T("StepFetchCIDRs")
	case "connecting":
		return i18n.T("StepConnecting")
	case "load_routes":
		return i18n.T("StepLoadRoutes")
	default:
		return ""
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

// parseIPCCIDRURLs decodes a JSON-encoded model.CIDRURLSource array
// from the profile string and converts it to the IPC wire type.
func parseIPCCIDRURLs(jsonStr string) []ipc.CIDRURLSource {
	sources := model.ParseCIDRURLs(jsonStr)
	if len(sources) == 0 {
		return nil
	}
	out := make([]ipc.CIDRURLSource, len(sources))
	for i, s := range sources {
		out[i] = ipc.CIDRURLSource{
			URL:         s.URL,
			FetchTiming: string(s.FetchTiming),
		}
	}
	return out
}

// marshalCIDRURLs encodes a slice of CIDRURLSource to a JSON string
// for storage in the database.
func marshalCIDRURLs(sources []model.CIDRURLSource) string {
	if len(sources) == 0 {
		return ""
	}
	data, err := json.Marshal(sources)
	if err != nil {
		return ""
	}
	return string(data)
}
