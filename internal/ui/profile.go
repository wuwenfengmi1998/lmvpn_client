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
)

func authModeLabels() []string {
	return []string{i18n.T("AuthModeBoth"), i18n.T("AuthModeJWT"), i18n.T("AuthModePassword")}
}

func routeModeLabels() []string {
	return []string{i18n.T("RoutingModeFull"), i18n.T("RoutingModeSplit"), i18n.T("RoutingModeCustom")}
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
	serverEntry := widget.NewEntry()
	userEntry := widget.NewEntry()
	passEntry := widget.NewPasswordEntry()
	authSelect := widget.NewSelect(authModeLabels(), nil)
	routeSelect := widget.NewSelect(routeModeLabels(), nil)
	cidrEntry := widget.NewMultiLineEntry()
	cidrEntry.SetPlaceHolder(i18n.T("PlaceholderCIDRs"))
	mtuEntry := widget.NewEntry()
	mtuEntry.SetPlaceHolder(i18n.T("PlaceholderMTU"))

	if !isNew {
		nameEntry.SetText(editing.Name)
		serverEntry.SetText(editing.ServerURL)
		userEntry.SetText(editing.Username)
		authSelect.SetSelectedIndex(codeIndex(authCodes, string(editing.AuthMode)))
		routeSelect.SetSelectedIndex(codeIndex(routeCodes, string(editing.RoutingMode)))
		cidrEntry.SetText(editing.CustomCIDRs)
		mtuEntry.SetText(fmtInt(editing.MTUOverride))
		passEntry.SetPlaceHolder(i18n.T("PlaceholderPasswordUnchanged"))
	} else {
		authSelect.SetSelectedIndex(codeIndex(authCodes, string(model.AuthModeBoth)))
		routeSelect.SetSelectedIndex(codeIndex(routeCodes, string(model.RoutingFull)))
		mtuEntry.SetText("0")
	}

	form := container.NewVBox(
		widget.NewLabel(i18n.T("FieldName")), nameEntry,
		widget.NewLabel(i18n.T("FieldServerURL")), serverEntry,
		widget.NewLabel(i18n.T("FieldUsername")), userEntry,
		widget.NewLabel(i18n.T("FieldPassword")), passEntry,
		widget.NewLabel(i18n.T("FieldAuthMode")), authSelect,
		widget.NewLabel(i18n.T("FieldRoutingMode")), routeSelect,
		widget.NewLabel(i18n.T("FieldCustomCIDRs")), cidrEntry,
		widget.NewLabel(i18n.T("FieldMTUOverride")), mtuEntry,
	)

	profileWin := a.fyneApp.NewWindow(i18n.T("DlgProfileTitle"))
	a.profileWindow = profileWin

	profileWin.SetOnClosed(func() {
		a.profileWindow = nil
	})

	saveBtn := widget.NewButton(i18n.T("BtnSave"), func() {
		a.saveProfile(editing, nameEntry.Text, serverEntry.Text,
			userEntry.Text, passEntry.Text,
			selectedCode(authCodes, authSelect.SelectedIndex()),
			selectedCode(routeCodes, routeSelect.SelectedIndex()),
			cidrEntry.Text, mtuEntry.Text, isNew)
		profileWin.Close()
	})
	saveBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton(i18n.T("BtnCancel"), func() {
		profileWin.Close()
	})

	profileWin.SetContent(container.NewBorder(nil, container.NewHBox(saveBtn, cancelBtn), nil, nil, form))
	profileWin.Resize(fyne.NewSize(460, 560))
	profileWin.Show()
}

// saveProfile creates or updates a profile and stores credentials.
func (a *App) saveProfile(editing *model.ServerProfile,
	name, server, user, password, authMode, routeMode, cidrs, mtuStr string, isNew bool) {
	if name == "" || server == "" || user == "" {
		showError(i18n.T("DlgValidationTitle"), i18n.T("DlgValidationMsg"), a.window)
		return
	}

	mtu := parseIntDefault(mtuStr, 0)

	if isNew {
		p := &model.ServerProfile{
			Name:        name,
			ServerURL:   server,
			Username:    user,
			AuthMode:    model.AuthMode(authMode),
			RoutingMode: model.RoutingMode(routeMode),
			CustomCIDRs: cidrs,
			MTUOverride: mtu,
		}
		id, err := a.db.CreateProfile(p)
		if err != nil {
			showError(i18n.T("DlgSaveError"), err.Error(), a.window)
			return
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
		editing.ServerURL = server
		editing.Username = user
		editing.AuthMode = model.AuthMode(authMode)
		editing.RoutingMode = model.RoutingMode(routeMode)
		editing.CustomCIDRs = cidrs
		editing.MTUOverride = mtu
		if err := a.db.UpdateProfile(editing); err != nil {
			showError(i18n.T("DlgSaveError"), err.Error(), a.window)
			return
		}
		if password != "" {
			_ = a.kc.DeleteAll(oldName)
			if err := a.kc.SetPassword(name, password); err != nil {
				showError(i18n.T("DlgKeychainError"), err.Error(), a.window)
			}
		}
	}

	a.loadProfiles()
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
