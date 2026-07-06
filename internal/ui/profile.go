package ui

import (
	"strconv"

	"lmvpn/internal/i18n"
	"lmvpn/internal/model"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// authCodes and routeCodes keep the canonical enum values in a fixed
// order so that dropdown display labels (which are localised) can be
// mapped back to the codes stored in the database.
var (
	authCodes  = []string{string(model.AuthModeBoth), string(model.AuthModeJWT), string(model.AuthModePassword)}
	routeCodes = []string{string(model.RoutingFull), string(model.RoutingSplit), string(model.RoutingCustom)}

	protoCodes  = []string{"wss", "ws"}
)

func authModeLabels() []string {
	return []string{i18n.T("AuthModeBoth"), i18n.T("AuthModeJWT"), i18n.T("AuthModePassword")}
}

func routeModeLabels() []string {
	return []string{i18n.T("RoutingModeFull"), i18n.T("RoutingModeSplit"), i18n.T("RoutingModeCustom")}
}

func protoLabels() []string {
	return []string{"wss", "ws"}
}

// codeIndex returns the position of code in codes, or 0 if not found.
func codeIndex(codes []string, code string) int {
	for i, c := range codes {
		if c == code {
			return i
		}
	}
	return 0
}

// selectedCode returns the enum code for the given dropdown index.
func selectedCode(codes []string, idx int) string {
	if idx < 0 || idx >= len(codes) {
		return codes[0]
	}
	return codes[idx]
}

// showProfileDialog opens a separate window for editing a server profile.
// If editing is nil, a new profile is created. If a profile window is
// already open, it is brought to the front instead of opening a second one.
func (a *App) showProfileDialog(editing *model.ServerProfile) {
	if a.profileWindow != nil {
		a.profileWindow.RequestFocus()
		return
	}

	isNew := editing == nil

	nameEntry := widget.NewEntry()
	nameEntry.Wrapping = fyne.TextWrapOff
	nameEntry.Scroll = container.ScrollNone

	protoSelect := widget.NewSelect(protoLabels(), nil)
	hostEntry := widget.NewEntry()
	hostEntry.Wrapping = fyne.TextWrapOff
	hostEntry.Scroll = container.ScrollNone

	ipsEntry := widget.NewEntry()
	ipsEntry.Wrapping = fyne.TextWrapOff
	ipsEntry.Scroll = container.ScrollNone
	ipsEntry.SetPlaceHolder(i18n.T("PlaceholderServerIPs"))

	portEntry := widget.NewEntry()
	portEntry.Wrapping = fyne.TextWrapOff
	portEntry.Scroll = container.ScrollNone

	pathEntry := widget.NewEntry()
	pathEntry.Wrapping = fyne.TextWrapOff
	pathEntry.Scroll = container.ScrollNone

	userEntry := widget.NewEntry()
	userEntry.Wrapping = fyne.TextWrapOff
	userEntry.Scroll = container.ScrollNone

	passEntry := widget.NewPasswordEntry()
	passEntry.Wrapping = fyne.TextWrapOff
	passEntry.Scroll = container.ScrollNone

	authSelect := widget.NewSelect(authModeLabels(), nil)
	routeSelect := widget.NewSelect(routeModeLabels(), nil)

	cidrEntry := widget.NewMultiLineEntry()
	cidrEntry.Wrapping = fyne.TextWrapOff
	cidrEntry.Scroll = container.ScrollNone
	cidrEntry.SetMinRowsVisible(4)
	cidrEntry.SetPlaceHolder(i18n.T("PlaceholderCIDRs"))

	mtuEntry := widget.NewEntry()
	mtuEntry.Wrapping = fyne.TextWrapOff
	mtuEntry.Scroll = container.ScrollNone
	mtuEntry.SetPlaceHolder(i18n.T("PlaceholderMTU"))

	if !isNew {
		nameEntry.SetText(editing.Name)
		protoSelect.SetSelectedIndex(codeIndex(protoCodes, editing.Protocol))
		hostEntry.SetText(editing.Host)
		ipsEntry.SetText(editing.ServerIPs)
		if editing.Port > 0 {
			portEntry.SetText(fmtInt(editing.Port))
		}
		pathEntry.SetText(editing.Path)
		userEntry.SetText(editing.Username)
		authSelect.SetSelectedIndex(codeIndex(authCodes, string(editing.AuthMode)))
		routeSelect.SetSelectedIndex(codeIndex(routeCodes, string(editing.RoutingMode)))
		cidrEntry.SetText(editing.CustomCIDRs)
		mtuEntry.SetText(fmtInt(editing.MTUOverride))
		passEntry.SetPlaceHolder(i18n.T("PlaceholderPasswordUnchanged"))
	} else {
		protoSelect.SetSelectedIndex(0) // wss
		portEntry.SetText("443")
		pathEntry.SetText("/ws")
		authSelect.SetSelectedIndex(codeIndex(authCodes, string(model.AuthModeBoth)))
		routeSelect.SetSelectedIndex(codeIndex(routeCodes, string(model.RoutingFull)))
		mtuEntry.SetText("0")
	}

	form := container.NewVBox(
		widget.NewLabel(i18n.T("FieldName")),
		nameEntry,

		container.NewBorder(nil, nil,
			container.NewVBox(widget.NewLabel(i18n.T("FieldProtocol")), protoSelect),
			container.NewVBox(widget.NewLabel(i18n.T("FieldPort")), portEntry),
			container.NewVBox(widget.NewLabel(i18n.T("FieldHost")), hostEntry),
		),

		container.NewGridWithColumns(2,
			container.NewVBox(widget.NewLabel(i18n.T("FieldPath")), pathEntry),
			container.NewVBox(widget.NewLabel(i18n.T("FieldServerIPs")), ipsEntry),
		),

		container.NewGridWithColumns(2,
			container.NewVBox(widget.NewLabel(i18n.T("FieldUsername")), userEntry),
			container.NewVBox(widget.NewLabel(i18n.T("FieldPassword")), passEntry),
		),

		container.NewGridWithColumns(2,
			container.NewVBox(widget.NewLabel(i18n.T("FieldAuthMode")), authSelect),
			container.NewVBox(widget.NewLabel(i18n.T("FieldRoutingMode")), routeSelect),
		),

		widget.NewLabel(i18n.T("FieldCustomCIDRs")), cidrEntry,
		widget.NewLabel(i18n.T("FieldMTUOverride")), mtuEntry,
	)

	profileWin := a.fyneApp.NewWindow(i18n.T("DlgProfileTitle"))
	a.profileWindow = profileWin

	profileWin.SetOnClosed(func() {
		a.profileWindow = nil
	})

	saveBtn := widget.NewButton(i18n.T("BtnSave"), func() {
		if a.saveProfile(editing,
			nameEntry.Text,
			selectedCode(protoCodes, protoSelect.SelectedIndex()),
			hostEntry.Text, ipsEntry.Text,
			portEntry.Text, pathEntry.Text,
			userEntry.Text, passEntry.Text,
			selectedCode(authCodes, authSelect.SelectedIndex()),
			selectedCode(routeCodes, routeSelect.SelectedIndex()),
			cidrEntry.Text, mtuEntry.Text, isNew) {
			profileWin.Close()
		}
	})
	saveBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton(i18n.T("BtnCancel"), func() {
		profileWin.Close()
	})

	profileWin.SetContent(container.NewBorder(nil, container.NewHBox(saveBtn, cancelBtn), nil, nil, container.NewVScroll(form)))
	profileWin.Resize(fyne.NewSize(460, 560))
	profileWin.Show()
}

// saveProfile creates or updates a profile and stores credentials.
// Returns true on success, false if validation or DB operation failed.
func (a *App) saveProfile(editing *model.ServerProfile,
	name, protocol, host, ips, portStr, pathStr, user, password, authMode, routeMode, cidrs, mtuStr string, isNew bool) bool {
	if name == "" || host == "" || user == "" {
		showError(i18n.T("DlgValidationTitle"), i18n.T("DlgValidationMsg"), a.window)
		return false
	}

	port := parseIntDefault(portStr, 443)
	mtu := parseIntDefault(mtuStr, 0)

	if isNew {
		p := &model.ServerProfile{
			Name:        name,
			Protocol:    protocol,
			Host:        host,
			ServerIPs:   ips,
			Port:        port,
			Path:        pathStr,
			Username:    user,
			AuthMode:    model.AuthMode(authMode),
			RoutingMode: model.RoutingMode(routeMode),
			CustomCIDRs: cidrs,
			MTUOverride: mtu,
		}
		id, err := a.db.CreateProfile(p)
		if err != nil {
			showError(i18n.T("DlgSaveError"), err.Error(), a.window)
			return false
		}
		_ = id
		if password != "" {
			if err := a.kc.SetPassword(name, password); err != nil {
				showError(i18n.T("DlgKeychainError"), err.Error(), a.window)
			}
		}
	} else {
		oldName := editing.Name
		editing.Name = name
		editing.Protocol = protocol
		editing.Host = host
		editing.ServerIPs = ips
		editing.Port = port
		editing.Path = pathStr
		editing.Username = user
		editing.AuthMode = model.AuthMode(authMode)
		editing.RoutingMode = model.RoutingMode(routeMode)
		editing.CustomCIDRs = cidrs
		editing.MTUOverride = mtu
		if err := a.db.UpdateProfile(editing); err != nil {
			showError(i18n.T("DlgSaveError"), err.Error(), a.window)
			return false
		}
		if password != "" {
			_ = a.kc.DeleteAll(oldName)
			if err := a.kc.SetPassword(name, password); err != nil {
				showError(i18n.T("DlgKeychainError"), err.Error(), a.window)
			}
		}
	}

	a.loadProfiles()
	return true
}

func fmtInt(n int) string {
	return strconv.Itoa(n)
}

func parseIntDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
