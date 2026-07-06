package ui

import (
	"strconv"

	"lmvpn/internal/model"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// showProfileDialog displays an add/edit dialog for a server profile.
// If editing is nil, a new profile is created.
func (a *App) showProfileDialog(editing *model.ServerProfile) {
	isNew := editing == nil

	nameEntry := widget.NewEntry()
	serverEntry := widget.NewEntry()
	userEntry := widget.NewEntry()
	passEntry := widget.NewPasswordEntry()
	authSelect := widget.NewSelect([]string{"both", "jwt", "password"}, nil)
	routeSelect := widget.NewSelect([]string{"full", "split", "custom"}, nil)
	cidrEntry := widget.NewMultiLineEntry()
	cidrEntry.SetPlaceHolder("10.0.0.0/8, 172.16.0.0/12")
	mtuEntry := widget.NewEntry()
	mtuEntry.SetPlaceHolder("0 = use server MTU")

	if !isNew {
		nameEntry.SetText(editing.Name)
		serverEntry.SetText(editing.ServerURL)
		userEntry.SetText(editing.Username)
		authSelect.SetSelected(string(editing.AuthMode))
		routeSelect.SetSelected(string(editing.RoutingMode))
		cidrEntry.SetText(editing.CustomCIDRs)
		mtuEntry.SetText(fmtInt(editing.MTUOverride))
		passEntry.SetPlaceHolder("(unchanged)")
	} else {
		authSelect.SetSelected(string(model.AuthModeBoth))
		routeSelect.SetSelected(string(model.RoutingFull))
		mtuEntry.SetText("0")
	}

	form := container.NewVBox(
		widget.NewLabel("Name"), nameEntry,
		widget.NewLabel("Server URL"), serverEntry,
		widget.NewLabel("Username"), userEntry,
		widget.NewLabel("Password"), passEntry,
		widget.NewLabel("Auth Mode"), authSelect,
		widget.NewLabel("Routing Mode"), routeSelect,
		widget.NewLabel("Custom CIDRs (comma-separated)"), cidrEntry,
		widget.NewLabel("MTU Override"), mtuEntry,
	)

	d := dialog.NewCustomConfirm("Profile", "Save", "Cancel", form, func(save bool) {
		if !save {
			return
		}
		a.saveProfile(editing, nameEntry.Text, serverEntry.Text,
			userEntry.Text, passEntry.Text,
			authSelect.Selected, routeSelect.Selected,
			cidrEntry.Text, mtuEntry.Text, isNew)
	}, a.window)
	d.Resize(fyne.NewSize(400, 500))
	d.Show()
}

// saveProfile creates or updates a profile and stores credentials.
func (a *App) saveProfile(editing *model.ServerProfile,
	name, server, user, password, authMode, routeMode, cidrs, mtuStr string, isNew bool) {
	if name == "" || server == "" || user == "" {
		showError("Validation", "Name, Server URL, and Username are required.", a.window)
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
			showError("Save Error", err.Error(), a.window)
			return
		}
		_ = id
		if password != "" {
			if err := a.kc.SetPassword(name, password); err != nil {
				showError("Keychain Error", err.Error(), a.window)
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
			showError("Save Error", err.Error(), a.window)
			return
		}
		if password != "" {
			_ = a.kc.DeleteAll(oldName)
			if err := a.kc.SetPassword(name, password); err != nil {
				showError("Keychain Error", err.Error(), a.window)
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
