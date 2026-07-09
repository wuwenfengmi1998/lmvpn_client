package ui

import (
	"encoding/json"
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
	routeCodes = []string{string(model.RoutingFull), string(model.RoutingProxy), string(model.RoutingBypass)}

	protoCodes = []string{"wss", "ws"}

	fetchTimingCodes = []string{string(model.FetchBefore), string(model.FetchAfter)}
)

func authModeLabels() []string {
	return []string{i18n.T("AuthModeBoth"), i18n.T("AuthModeJWT"), i18n.T("AuthModePassword")}
}

func routeModeLabels() []string {
	return []string{i18n.T("RoutingModeFull"), i18n.T("RoutingModeProxy"), i18n.T("RoutingModeBypass")}
}

func protoLabels() []string {
	return []string{"wss", "ws"}
}

func fetchTimingLabels() []string {
	return []string{i18n.T("FetchTimingBefore"), i18n.T("FetchTimingAfter")}
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

// urlEntryRow holds the widgets for a single CIDR URL source row.
type urlEntryRow struct {
	urlEntry    *widget.Entry
	timingSelect *widget.Select
	container   *fyne.Container
}

// cidrURLList manages a dynamic list of CIDR URL source rows.
type cidrURLList struct {
	rows   []*urlEntryRow
	container *fyne.Container
	parent    fyne.Window
}

func newCIDRURLList(parent fyne.Window) *cidrURLList {
	cl := &cidrURLList{
		container: container.NewVBox(),
		parent:    parent,
	}
	return cl
}

func (cl *cidrURLList) addRow(url string, timingIdx int) {
	urlEntry := widget.NewEntry()
	urlEntry.Wrapping = fyne.TextWrapOff
	urlEntry.Scroll = container.ScrollHorizontalOnly
	urlEntry.SetPlaceHolder(i18n.T("PlaceholderCIDRURL"))
	urlEntry.SetText(url)

	timingSelect := widget.NewSelect(fetchTimingLabels(), nil)
	if timingIdx >= 0 && timingIdx < len(fetchTimingCodes) {
		timingSelect.SetSelectedIndex(timingIdx)
	} else {
		timingSelect.SetSelectedIndex(0)
	}

	row := &urlEntryRow{
		urlEntry:     urlEntry,
		timingSelect: timingSelect,
	}

	removeBtn := widget.NewButton(i18n.T("BtnRemoveURL"), func() {
		cl.removeRow(row)
	})

	row.container = container.NewBorder(nil, nil, nil, removeBtn,
		container.NewVBox(urlEntry, timingSelect))

	cl.rows = append(cl.rows, row)
	cl.container.Add(row.container)
}

func (cl *cidrURLList) removeRow(row *urlEntryRow) {
	for i, r := range cl.rows {
		if r == row {
			cl.rows = append(cl.rows[:i], cl.rows[i+1:]...)
			cl.container.Remove(row.container)
			break
		}
	}
}

func (cl *cidrURLList) loadFromJSON(jsonStr string) {
	for _, r := range cl.rows {
		cl.container.Remove(r.container)
	}
	cl.rows = nil

	sources := model.ParseCIDRURLs(jsonStr)
	for _, s := range sources {
		timingIdx := codeIndex(fetchTimingCodes, string(s.FetchTiming))
		cl.addRow(s.URL, timingIdx)
	}
}

func (cl *cidrURLList) toSources() []model.CIDRURLSource {
	var sources []model.CIDRURLSource
	for _, r := range cl.rows {
		url := strings.TrimSpace(r.urlEntry.Text)
		if url == "" {
			continue
		}
		sources = append(sources, model.CIDRURLSource{
			URL:         url,
			FetchTiming: model.FetchTiming(selectedCode(fetchTimingCodes, r.timingSelect.SelectedIndex())),
		})
	}
	return sources
}

func (cl *cidrURLList) toJSON() string {
	return marshalCIDRURLs(cl.toSources())
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

	cidrV4Entry := widget.NewMultiLineEntry()
	cidrV4Entry.Wrapping = fyne.TextWrapOff
	cidrV4Entry.Scroll = container.ScrollNone
	cidrV4Entry.SetMinRowsVisible(3)
	cidrV4Entry.SetPlaceHolder(i18n.T("PlaceholderCIDRV4"))

	cidrV6Entry := widget.NewMultiLineEntry()
	cidrV6Entry.Wrapping = fyne.TextWrapOff
	cidrV6Entry.Scroll = container.ScrollNone
	cidrV6Entry.SetMinRowsVisible(3)
	cidrV6Entry.SetPlaceHolder(i18n.T("PlaceholderCIDRV6"))

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

	// CIDR URL lists for IPv4 and IPv6.
	v4URLList := newCIDRURLList(profileWin)
	v6URLList := newCIDRURLList(profileWin)

	v4AddURLBtn := widget.NewButton(i18n.T("BtnAddURL"), func() {
		v4URLList.addRow("", 0)
	})
	v6AddURLBtn := widget.NewButton(i18n.T("BtnAddURL"), func() {
		v6URLList.addRow("", 0)
	})

	// CIDR section visibility: hide when routing mode is "full".
	cidrV4EntryBox := container.NewVBox(widget.NewLabel(i18n.T("FieldCIDRV4")), cidrV4Entry)
	cidrV6EntryBox := container.NewVBox(widget.NewLabel(i18n.T("FieldCIDRV6")), cidrV6Entry)
	v4URLBox := container.NewVBox(widget.NewLabel(i18n.T("FieldCIDRV4URLs")), v4URLList.container, v4AddURLBtn)
	v6URLBox := container.NewVBox(widget.NewLabel(i18n.T("FieldCIDRV6URLs")), v6URLList.container, v6AddURLBtn)
	cidrSection := container.NewVBox(cidrV4EntryBox, cidrV6EntryBox, v4URLBox, v6URLBox)

	setCIDRVisible := func(mode string) {
		if mode == string(model.RoutingFull) {
			cidrSection.Hide()
		} else {
			cidrSection.Show()
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
		cidrV4Entry.SetText(editing.CIDRV4)
		cidrV6Entry.SetText(editing.CIDRV6)
		v4URLList.loadFromJSON(editing.CIDRV4URLs)
		v6URLList.loadFromJSON(editing.CIDRV6URLs)
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

	routeSelect.OnChanged = func(_ string) {
		setCIDRVisible(selectedCode(routeCodes, routeSelect.SelectedIndex()))
	}
	setCIDRVisible(selectedCode(routeCodes, routeSelect.SelectedIndex()))

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

		cidrSection,

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

	// Update parent references now that the window exists.
	v4URLList.parent = profileWin
	v6URLList.parent = profileWin

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
			cidrV4Entry.Text, cidrV6Entry.Text,
			v4URLList.toJSON(), v6URLList.toJSON(),
			mtuEntry.Text,
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
	profileWin.Resize(fyne.NewSize(460, 860))
	profileWin.Show()
}

// saveProfile creates or updates a profile and stores credentials.
// Returns true on success, false if validation or DB operation failed.
func (a *App) saveProfile(editing *model.ServerProfile,
	name, protocol, host, ips, portStr, pathStr, user, password, authMode, routeMode,
	cidrV4, cidrV6, cidrV4URLs, cidrV6URLs, mtuStr,
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
			CIDRV4:        cidrV4,
			CIDRV6:        cidrV6,
			CIDRV4URLs:    cidrV4URLs,
			CIDRV6URLs:    cidrV6URLs,
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
		editing.CIDRV4 = cidrV4
		editing.CIDRV6 = cidrV6
		editing.CIDRV4URLs = cidrV4URLs
		editing.CIDRV6URLs = cidrV6URLs
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

// marshalCIDRURLsForDB is a helper for tests/debugging that encodes
// CIDRURLSource slice to JSON.
func marshalCIDRURLsForDB(sources []model.CIDRURLSource) string {
	data, err := json.Marshal(sources)
	if err != nil {
		return ""
	}
	return string(data)
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
