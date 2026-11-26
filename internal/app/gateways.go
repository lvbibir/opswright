package app

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/icons"
	"github.com/lvbibir/opswright/internal/theme"
	"github.com/lvbibir/opswright/locales"
)

// gatewayForm is the gateway (jump server) editor's fields.
type gatewayForm struct {
	isNew               bool
	orig                assets.Gateway
	name, address, port string
	user                string
	// cred is the credential's ID, credPick its label in the select.
	cred     string
	credPick string
	login    string
	// direct, exec and sftp are the connection's capabilities.
	direct, exec, sftp bool
	note               string
	err                string
}

func (a *App) openGatewayForm(g *assets.Gateway) {
	f := &gatewayForm{isNew: g == nil, port: "22", login: assets.DefaultGatewayLogin}
	if g != nil {
		f.orig = *g
		f.name, f.address, f.port, f.user = g.Name, g.Address, strconv.Itoa(g.Port), g.User
		f.cred, f.login = g.CredentialID, g.Login
		f.direct, f.exec, f.sftp, f.note = g.DirectTCPIP, g.Exec, g.SFTP, g.Note
	}
	a.gatewayForm, a.gatewayEditing = f, true
}

// SaveGatewayForm validates the sheet and saves the gateway.
func (a *App) SaveGatewayForm(m locales.Messages) bool {
	f := a.gatewayForm
	if f == nil {
		return false
	}
	f.err = ""
	port, err := strconv.Atoi(strings.TrimSpace(f.port))
	if err != nil || port < 1 || port > 65535 {
		f.err = m.InvalidPort()
		return false
	}
	g := f.orig
	g.Name, g.Address, g.Port, g.User, g.CredentialID = f.name, f.address, port, f.user, f.cred
	g.Login = strings.TrimSpace(f.login)
	if g.Login == assets.DefaultGatewayLogin {
		g.Login = ""
	}
	g.DirectTCPIP, g.Exec, g.SFTP, g.Note = f.direct, f.exec, f.sftp, f.note
	if _, err := a.store.SaveGateway(g); err != nil {
		f.err = fieldError(m, err)
		return false
	}
	a.gatewayForm, a.gatewayEditing = nil, false
	return true
}

// connectGateway opens a terminal on the gateway itself: a temporary
// host of its address, user and credential, so the bastion's own shell
// (its host chooser, its menu) is reached, not a host it manages.
func (a *App) connectGateway(m locales.Messages, gw assets.Gateway) {
	a.nextTemp++
	h := assets.Host{
		ID: tempPrefix + strconv.Itoa(a.nextTemp), Name: gw.Label(),
		Address: gw.Address, Port: gw.Port, User: gw.User,
		Auth: assets.AuthCredential, CredentialID: gw.CredentialID, CwdFollow: true,
	}
	h.Normalize()
	a.temps[h.ID] = h
	if _, err := a.Connect(h.ID, openAt{}); err != nil {
		delete(a.temps, h.ID)
		a.toastError(m.ErrGeneric(err.Error()))
	}
}

// gatewayCredSelect picks the credential that logs in to the gateway.
func (a *App) gatewayCredSelect(c *ui.Context, m locales.Messages, f *gatewayForm) {
	var opts, ids []string
	for _, cr := range a.store.Credentials() {
		opts, ids = append(opts, cr.Name), append(ids, cr.ID)
	}
	if len(opts) == 0 {
		ui.Text(c, m.GatewayNoCredential()).FontSize(theme.Footnote).TextColor(currentTokens(c).Danger)
		return
	}
	if f.cred == "" {
		f.cred = ids[0]
	}
	label := opts[0]
	for i, id := range ids {
		if id == f.cred {
			label = opts[i]
		}
	}
	f.credPick = label
	if formSelect(c.Key("gw:cred"), &f.credPick, opts).Label(m.FieldAuth()).FillWidth().Changed() {
		if i := slices.Index(opts, f.credPick); i >= 0 {
			f.cred = ids[i]
		}
	}
}

// gatewayNote describes a gateway in a list: where it is, who logs in and
// what its connection can do.
func gatewayNote(m locales.Messages, gw assets.Gateway) string {
	note := gw.Addr()
	if gw.User != "" {
		note += " · " + gw.User
	}
	if gw.DirectTCPIP {
		return note + " · " + m.GatewayCapDirect()
	}
	var caps []string
	if gw.Exec {
		caps = append(caps, "exec")
	}
	if gw.SFTP {
		caps = append(caps, "sftp")
	}
	if len(caps) == 0 {
		caps = append(caps, m.GatewayCapTerminalOnly())
	}
	return note + " · " + strings.Join(caps, " + ")
}

func (a *App) gatewayView(c *ui.Context, m locales.Messages) {
	f := a.gatewayForm
	if f == nil {
		return
	}
	tok := currentTokens(c)
	sheet(ui.Modal(c, &a.gatewayEditing, func() {
		ui.Column(c).Gap(18).Width(480).Children(func() {
			title := m.EditGateway()
			if f.isNew {
				title = m.NewGateway()
			}
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				tint := theme.Brand
				if !f.isNew {
					tint = tok.Tint(f.orig.ID)
				}
				tile(c, icons.JumpHost, tint, 36)
				ui.Column(c).Gap(1).Shrink(1).Children(func() {
					sheetTitle(c, title)
					if f.name != "" {
						ui.Text(c, f.name).FontSize(theme.Footnote).TextColor(tok.TextMuted).SingleLine()
					}
				})
			})
			ui.Form(c, func() {
				ui.Field(c, m.FieldName(), func() {
					in := formTextInput(c.Key("gw:name"), &f.name).Placeholder("bastion").AutoFocus()
					a.gatewayHold.apply(c, f, in)
				})
				ui.Field(c, m.FieldHost(), func() {
					formTextInput(c.Key("gw:addr"), &f.address).Placeholder("bastion.example.com")
				})
				ui.Field(c, m.FieldPort(), func() { formTextInput(c.Key("gw:port"), &f.port).Width(90) })
				ui.Field(c, m.FieldUser(), func() {
					formTextInput(c.Key("gw:user"), &f.user).Placeholder("bastion-account")
				})
				ui.Field(c, m.FieldAuth(), func() { a.gatewayCredSelect(c, m, f) })
				if !f.direct {
					ui.Field(c, m.GatewayLogin(), func() {
						ui.Column(c).Gap(4).FillWidth().Children(func() {
							formTextInput(c.Key("gw:login"), &f.login).Placeholder(assets.DefaultGatewayLogin)
							ui.Text(c, m.GatewayLoginHint("{gateway}", "{user}", "{host}")).FontSize(theme.Caption).TextColor(tok.TextMuted)
						})
					})
				}
				ui.Field(c, m.GatewayDirect(), func() { formSwitch(c.Key("gw:direct"), &f.direct).Label(m.GatewayDirect()) })
				if !f.direct {
					ui.Field(c, m.GatewayExec(), func() { formSwitch(c.Key("gw:exec"), &f.exec).Label(m.GatewayExec()) })
					ui.Field(c, m.GatewaySFTP(), func() { formSwitch(c.Key("gw:sftp"), &f.sftp).Label(m.GatewaySFTP()) })
				}
				ui.Field(c, m.FieldNote(), func() { formTextInput(c.Key("gw:note"), &f.note) })
			})
			ui.Text(c, m.GatewayHint()).FontSize(theme.Footnote).TextColor(tok.TextMuted)
			if !f.isNew {
				used := m.CredUnused()
				if hs := a.store.GatewayUsers(f.orig.ID); len(hs) > 0 {
					used = m.CredUsedBy(hostNames(hs))
				}
				ui.Text(c, used).FontSize(theme.Footnote).TextColor(tok.TextMuted)
			}
			if f.err != "" {
				ui.Text(c, f.err).FontSize(theme.Footnote).TextColor(tok.Danger)
			}
			ui.Box(c).Height(1).FillWidth().Background(tok.Line)
			ui.Row(c).Gap(8).Children(func() {
				if !f.isNew && pill(c, m.Delete(), false, true, false) {
					if err := a.store.DeleteGateway(f.orig.ID); err != nil {
						f.err = a.inUseText(m, err)
					} else {
						a.gatewayEditing = false
					}
				}
				ui.Spacer(c)
				if pill(c, m.Cancel(), false, false, false) {
					a.gatewayEditing = false
				}
				if pill(c, m.Save(), true, false, false) {
					a.SaveGatewayForm(m)
				}
			})
		})
	}), tok)
	if !a.gatewayEditing && a.gatewayForm == f {
		a.gatewayForm = nil
	}
}

// gatewaysSection is the settings' list of jump servers, each of which
// can be connected to on its own.
func (a *App) gatewaysSection(c *ui.Context, m locales.Messages, tok theme.Tokens) {
	a.settingsSection(c, tok, nil, "", func(g *settingsGroup) {
		gs := a.store.Gateways()
		if len(gs) == 0 {
			g.row(m.NoGateways(), "", func() {})
		}
		for _, gw := range gs {
			gw := gw
			note := a.pv(gatewayNote(m, gw))
			if hs := a.store.GatewayUsers(gw.ID); len(hs) > 0 {
				note += " — " + m.CredUsedBy(hostNames(hs))
			}
			g.row(gw.Label(), note, func() {
				if iconButton(c, "gw:conn:"+gw.ID, icons.Terminal, m.ConnectGateway()) {
					a.connectGateway(m, gw)
				}
				if iconButton(c, "gw:edit:"+gw.ID, icons.Edit, m.Edit()) {
					a.openGatewayForm(&gw)
				}
				if iconButton(c, "gw:del:"+gw.ID, icons.Delete, m.Delete()) {
					if err := a.store.DeleteGateway(gw.ID); err != nil {
						a.toastError(a.inUseText(m, err))
					}
				}
			})
		}
		g.row("", "", func() {
			if pill(c, m.NewGateway(), false, false, false) {
				a.openGatewayForm(nil)
			}
		})
	})
}
