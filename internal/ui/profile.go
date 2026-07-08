package ui

import (
	"fmt"
	"strconv"
	"strings"

	"lmvpn/internal/i18n"
	"lmvpn/internal/model"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// authCodes and routeCodes keep the canonical enum values in a fixed
// order so that dropdown display labels (which are localised) can be
// mapped back to the codes stored in the database.
var (
	authCodes  = []string{string(model.AuthModeBoth), string(model.AuthModeJWT), string(model.AuthModePassword)}
	routeCodes = []string{string(model.RoutingFull), string(model.RoutingSplit), string(model.RoutingCustom)}

	protoCodes = []string{"wss", "ws"}
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

	tlsCaPEMEntry := widget.NewMultiLineEntry()
	tlsCaPEMEntry.Wrapping = fyne.TextWrapOff
	tlsCaPEMEntry.Scroll = container.ScrollNone
	tlsCaPEMEntry.SetMinRowsVisible(4)
	tlsCaPEMEntry.SetPlaceHolder(i18n.T("PlaceholderTLSCACert"))

	tlsCaPathEntry := widget.NewEntry()
	tlsCaPathEntry.Wrapping = fyne.TextWrapOff
	tlsCaPathEntry.Scroll = container.ScrollNone
	tlsCaPathEntry.SetPlaceHolder(i18n.T("PlaceholderTLSCAPath"))

	tlsInsecureCheck := widget.NewCheck(i18n.T("FieldTLSInsecure"), nil)

	tlsPinnedHashEntry := widget.NewEntry()
	tlsPinnedHashEntry.Wrapping = fyne.TextWrapOff
	tlsPinnedHashEntry.Scroll = container.ScrollNone
	tlsPinnedHashEntry.SetPlaceHolder(i18n.T("PlaceholderTLSPinnedHash"))

	var profileWin fyne.Window

	browseBtn := widget.NewButton(i18n.T("BtnBrowse"), func() {
		dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			defer reader.Close()
			tlsCaPathEntry.SetText(reader.URI().Path())
		}, profileWin).Show()
	})

	setTLSEnabled := func(enabled bool) {
		if enabled {
			tlsCaPEMEntry.Enable()
			tlsCaPathEntry.Enable()
			browseBtn.Enable()
			tlsInsecureCheck.Enable()
			tlsPinnedHashEntry.Enable()
		} else {
			tlsCaPEMEntry.Disable()
			tlsCaPathEntry.Disable()
			browseBtn.Disable()
			tlsInsecureCheck.Disable()
			tlsPinnedHashEntry.Disable()
		}
	}

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
		tlsCaPEMEntry.SetText(editing.TLSCACert)
		tlsCaPathEntry.SetText(editing.TLSCAPath)
		tlsInsecureCheck.SetChecked(editing.TLSInsecure)
		tlsPinnedHashEntry.SetText(editing.TLSPinnedHash)
		passEntry.SetPlaceHolder(i18n.T("PlaceholderPasswordUnchanged"))
	} else {
		protoSelect.SetSelectedIndex(0) // wss
		portEntry.SetText("443")
		pathEntry.SetText("/ws")
		authSelect.SetSelectedIndex(codeIndex(authCodes, string(model.AuthModeBoth)))
		routeSelect.SetSelectedIndex(codeIndex(routeCodes, string(model.RoutingFull)))
		mtuEntry.SetText("0")
	}

	protoSelect.OnChanged = func(value string) {
		setTLSEnabled(value == "wss")
	}
	setTLSEnabled(selectedCode(protoCodes, protoSelect.SelectedIndex()) == "wss")

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

		widget.NewLabel(i18n.T("FieldTLS")),
		widget.NewLabel(i18n.T("FieldTLSCAPath")),
		container.NewBorder(nil, nil, nil, browseBtn, tlsCaPathEntry),
		widget.NewLabel(i18n.T("FieldTLSCACert")),
		tlsCaPEMEntry,
		widget.NewLabel(i18n.T("HintTLSCAReplacesSystem")),
		tlsInsecureCheck,
		widget.NewLabel(i18n.T("FieldTLSPinnedHash")),
		tlsPinnedHashEntry,
	)

	profileWin = a.fyneApp.NewWindow(i18n.T("DlgProfileTitle"))
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
			cidrEntry.Text, mtuEntry.Text,
			tlsCaPEMEntry.Text, tlsCaPathEntry.Text,
			tlsPinnedHashEntry.Text, tlsInsecureCheck.Checked,
			isNew) {
			profileWin.Close()
		}
	})
	saveBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton(i18n.T("BtnCancel"), func() {
		profileWin.Close()
	})

	profileWin.SetContent(container.NewBorder(nil, container.NewHBox(saveBtn, cancelBtn), nil, nil, container.NewVScroll(form)))
	profileWin.Resize(fyne.NewSize(460, 760))
	profileWin.Show()
}

// saveProfile creates or updates a profile and stores credentials.
// Returns true on success, false if validation or DB operation failed.
func (a *App) saveProfile(editing *model.ServerProfile,
	name, protocol, host, ips, portStr, pathStr, user, password, authMode, routeMode, cidrs, mtuStr,
	tlsCaPEM, tlsCaPath, tlsPinnedHash string, tlsInsecure, isNew bool) bool {
	if name == "" || host == "" || user == "" {
		showError(i18n.T("DlgValidationTitle"), i18n.T("DlgValidationMsg"), a.window)
		return false
	}

	if ips != "" {
		tmp := &model.ServerProfile{ServerIPs: ips}
		_, invalid := tmp.ValidateServerIPs()
		if len(invalid) > 0 {
			showError(i18n.T("DlgValidationTitle"),
				fmt.Sprintf(i18n.T("DlgInvalidIPMsg"), strings.Join(invalid, ", ")),
				a.window)
			return false
		}
	}

	port := parseIntDefault(portStr, 443)
	mtu := parseIntDefault(mtuStr, 0)

	if isNew {
		p := &model.ServerProfile{
			Name:          name,
			Protocol:      protocol,
			Host:          host,
			ServerIPs:     ips,
			Port:          port,
			Path:          pathStr,
			Username:      user,
			AuthMode:      model.AuthMode(authMode),
			RoutingMode:   model.RoutingMode(routeMode),
			CustomCIDRs:   cidrs,
			MTUOverride:   mtu,
			TLSCACert:     tlsCaPEM,
			TLSCAPath:     tlsCaPath,
			TLSInsecure:   tlsInsecure,
			TLSPinnedHash: tlsPinnedHash,
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
		editing.TLSCACert = tlsCaPEM
		editing.TLSCAPath = tlsCaPath
		editing.TLSInsecure = tlsInsecure
		editing.TLSPinnedHash = tlsPinnedHash
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

// showProfileListWindow opens a window listing all saved profiles with
// buttons to add, edit, delete, and reset the database. Only one
// instance is allowed; if already open it is brought to the front.
func (a *App) showProfileListWindow() {
	if a.profileListWindow != nil {
		a.profileListWindow.RequestFocus()
		return
	}

	a.profileList = widget.NewList(
		func() int { return len(a.profiles) },
		func() fyne.CanvasObject {
			name := widget.NewLabel("")
			name.TextStyle = fyne.TextStyle{Bold: true}
			host := widget.NewLabel("")
			return container.NewVBox(name, host)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			box := obj.(*fyne.Container)
			nameLbl := box.Objects[0].(*widget.Label)
			hostLbl := box.Objects[1].(*widget.Label)
			if id >= 0 && id < len(a.profiles) {
				p := a.profiles[id]
				nameLbl.SetText(p.Name)
				hostLbl.SetText(fmt.Sprintf("%s:%d", p.Host, p.Port))
			}
		},
	)
	a.profileList.OnSelected = func(id widget.ListItemID) {
		a.listSelectedIndex = id
	}
	a.profileList.OnUnselected = func(_ widget.ListItemID) {
		a.listSelectedIndex = -1
	}

	addBtn := widget.NewButton(i18n.T("BtnAddProfile"), a.onAddProfile)
	editBtn := widget.NewButton(i18n.T("BtnEdit"), a.onEditProfileFromList)
	deleteBtn := widget.NewButton(i18n.T("BtnDelete"), a.onDeleteProfileFromList)
	resetDBBtn := widget.NewButton(i18n.T("BtnResetDB"), a.onResetDB)

	buttons := container.NewGridWithColumns(4, addBtn, editBtn, deleteBtn, resetDBBtn)

	win := a.fyneApp.NewWindow(i18n.T("ProfileListTitle"))
	a.profileListWindow = win
	win.SetOnClosed(func() {
		a.profileListWindow = nil
		a.profileList = nil
		a.listSelectedIndex = -1
	})

	win.SetContent(container.NewBorder(nil, buttons, nil, nil, a.profileList))
	win.Resize(fyne.NewSize(460, 400))
	win.Show()
}

// onEditProfileFromList opens the profile editor for the profile
// selected in the list window.
func (a *App) onEditProfileFromList() {
	idx := a.listSelectedIndex
	if idx < 0 || idx >= len(a.profiles) {
		dialog.NewCustom(i18n.T("DlgNoListSelectTitle"), i18n.T("BtnOK"),
			widget.NewLabel(i18n.T("DlgNoListSelectEditMsg")), a.profileListWindow).Show()
		return
	}
	p := a.profiles[idx]
	a.showProfileDialog(&p)
}

// onDeleteProfileFromList deletes the profile selected in the list
// window after confirmation.
func (a *App) onDeleteProfileFromList() {
	idx := a.listSelectedIndex
	if idx < 0 || idx >= len(a.profiles) {
		dialog.NewCustom(i18n.T("DlgNoListSelectTitle"), i18n.T("BtnOK"),
			widget.NewLabel(i18n.T("DlgNoListSelectDeleteMsg")), a.profileListWindow).Show()
		return
	}
	p := a.profiles[idx]
	dialog.NewCustomConfirm(i18n.T("DlgDeleteProfileTitle"),
		i18n.T("BtnDelete"), i18n.T("BtnCancel"),
		widget.NewLabel(i18n.T("DlgDeleteProfileMsg", map[string]interface{}{"name": p.Name})),
		func(ok bool) {
			if !ok {
				return
			}
			if err := a.db.DeleteProfile(p.ID); err != nil {
				showError(i18n.T("DlgError"), err.Error(), a.profileListWindow)
				return
			}
			_ = a.kc.DeleteAll(p.Name)
			a.loadProfiles()
		}, a.profileListWindow).Show()
}
