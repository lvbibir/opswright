package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/assets"
	"github.com/lvbibir/opswright/internal/hostlist"
	"github.com/lvbibir/opswright/internal/icons"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/theme"
	"github.com/lvbibir/opswright/locales"
)

// hostImport is the sheet importing a host list: the text typed or read
// from a file, and the rows it parsed.
type hostImport struct {
	text string
	// rows are the preview's rows, from the text as last parsed.
	rows []*importRow
	// action is what every row a saved host already answers for does: one
	// choice sets it for all of them at once, and a single row may still
	// be given its own in the preview, which this does not follow.
	action string
	err    string
	// picking is set while the system's file dialog is open.
	picking bool
}

// importRow is one row of the preview.
type importRow struct {
	row hostlist.Row
	// pick is whether it is imported; a row that cannot be saved starts
	// unpicked.
	pick bool
	// named is set when the list gave the row a name of its own, so that
	// a host without one keeps its address as its name.
	named bool
	// name and group are what it is saved as. conflict is the saved host
	// answering for the same address, port and user, and action what to
	// do about it. gateway is the saved gateway the row names, and
	// gatewaySet whether the list said which one, so that a row that
	// names none leaves the saved host's as it is rather than reaching
	// its servers directly, a gateway passed over being a connection the
	// user did not ask for.
	name, group      string
	conflict, action string
	gateway          string
	gatewaySet       bool
}

// importActions are the choices for a row a saved host answers for, the
// first the default: skip, overwrite it, or keep both.
func importActions(m locales.Messages) []string {
	return []string{m.ImportSkip(), m.ImportOverwrite(), m.ImportKeepBoth()}
}

// openImport opens the sheet importing a host list.
func (a *App) openImport() {
	a.imp, a.importing = &hostImport{}, true
}

// importConflict is the saved host at the same address, port and user.
func (a *App) importConflict(r hostlist.Row) (assets.Host, bool) {
	for _, h := range a.store.List() {
		if strings.EqualFold(h.Address, r.Address) && h.Port == r.Port && h.User == r.User {
			return h, true
		}
	}
	return assets.Host{}, false
}

// parseImport reads the sheet's text into its preview.
func (a *App) parseImport(m locales.Messages) {
	f := a.imp
	if f == nil {
		return
	}
	f.err, f.rows = "", nil
	if strings.TrimSpace(f.text) == "" {
		return
	}
	rows, err := hostlist.Parse([]byte(f.text))
	if err != nil {
		f.err = importParseError(m, err)
		return
	}
	actions := importActions(m)
	f.action = actions[0]
	for _, r := range rows {
		ir := &importRow{row: r, pick: r.OK(), named: r.Name != "", name: r.Label(), group: r.Group, action: actions[0]}
		ir.gateway = a.importGateway(r.Gateway)
		ir.gatewaySet = ir.gateway != ""
		if old, ok := a.importConflict(r); ok {
			ir.conflict, ir.action = old.ID, actions[0]
		}
		f.rows = append(f.rows, ir)
	}
}

// importGateway is the saved gateway the list named, by its name or its
// address, ignoring case; "" when the list names none, or names one that
// is not here (the row is then imported without it, and says so).
func (a *App) importGateway(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	for _, gw := range a.store.Gateways() {
		if strings.EqualFold(gw.Name, name) || strings.EqualFold(gw.Address, name) {
			return gw.ID
		}
	}
	return ""
}

// importParseError says why the list could not be read.
func importParseError(m locales.Messages, err error) string {
	switch {
	case errors.Is(err, hostlist.ErrEmpty):
		return m.ImportEmpty()
	case errors.Is(err, hostlist.ErrTooLarge):
		return m.ImportTooLarge()
	case errors.Is(err, hostlist.ErrEncoding):
		return m.ImportNotUTF8()
	}
	return m.ImportFailed(err.Error())
}

// importChosen is whether a row of the preview is imported.
func importChosen(r *importRow, actions []string) bool {
	return r.pick && r.row.OK() && !(r.conflict != "" && r.action == actions[0])
}

// importCounts is the preview's numbers: the hosts to save, the rows a
// saved host already answers for, and the rows left out.
func (a *App) importCounts(m locales.Messages, f *hostImport) (n, existing, out int) {
	actions := importActions(m)
	for _, r := range f.rows {
		if r.conflict != "" {
			existing++
		}
		if importChosen(r, actions) {
			n++
			continue
		}
		out++
	}
	return n, existing, out
}

// importEvery sets what every row a saved host already answers for does,
// so that a list exported here, edited and read back updates the hosts it
// came from with one choice rather than one per row.
func importEvery(f *hostImport, action string) {
	for _, r := range f.rows {
		if r.conflict != "" {
			r.action = action
		}
	}
}

// importNeedsSecrets reports a preview that would write passwords.
func (a *App) importNeedsSecrets(m locales.Messages, f *hostImport) bool {
	actions := importActions(m)
	for _, r := range f.rows {
		if importChosen(r, actions) && (len(r.row.Password) > 0 || len(r.row.SuPassword) > 0) {
			return true
		}
	}
	return false
}

// Import saves the rows of the preview the user chose. Every chosen row is
// checked first, so either all of them are saved or none is: their secrets
// go in one encrypted commit, and the hosts in one metadata write, each
// compensating the other when the second fails. The sheet is kept open
// while a row cannot be saved, telling which and why.
func (a *App) Import(m locales.Messages) {
	f := a.imp
	if f == nil {
		return
	}
	f.err = ""
	if !a.unlocked() && a.importNeedsSecrets(m, f) {
		f.err = m.ImportNeedsUnlock()
		return
	}
	actions := importActions(m)
	n, existing, out := a.importCounts(m, f)
	if n == 0 {
		if existing > 0 {
			// Nothing was chosen but the rows that are already saved: say
			// so, rather than let the sheet close on an empty import.
			a.toast(func(m locales.Messages) ui.Toast {
				return ui.Toast{Title: m.ImportAllSaved(strconv.Itoa(existing)), Type: "warning"}
			})
		}
		return
	}
	baseline := a.store.List()
	names := map[string]bool{}
	for _, h := range baseline {
		names[strings.ToLower(h.Name)] = true
	}
	var snap assets.Snapshot
	var changes []keychain.Change
	for _, r := range f.rows {
		if !importChosen(r, actions) {
			continue
		}
		h, overwrote := a.importHost(r, actions, names)
		if err := h.Validate(); err != nil {
			f.err = m.ImportFailed(h.Label() + ": " + fieldError(m, err))
			return
		}
		changes = append(changes, a.importSecrets(r, h, overwrote)...)
		snap.Hosts = append(snap.Hosts, h)
	}
	tx := &secretTx{kc: a.kc}
	fail := func(err error) {
		if rb := tx.rollback(); rb != nil {
			err = errors.Join(err, rb)
		}
		f.err = m.ImportFailed(fieldError(m, err))
	}
	if err := tx.setBatch(changes); err != nil {
		fail(err)
		return
	}
	if err := a.store.RestoreIfHostsUnchanged(snap, baseline); err != nil {
		fail(err)
		return
	}
	for _, r := range f.rows {
		if importChosen(r, actions) {
			r.pick = false
		}
	}
	a.toast(func(m locales.Messages) ui.Toast {
		return ui.Toast{Title: m.ImportDone(strconv.Itoa(n), strconv.Itoa(out)), Type: "success"}
	})
	a.importing = false
	a.imp = nil
}

// importHost is the host a row is saved as: a new one, the saved one it
// overwrites, or, when both are kept, one beside it under a name no host
// has. Its name is the list's when the list gave it one, so that a host
// without a title keeps its address as its name. overwrote says a saved
// host is updated rather than a new one added.
func (a *App) importHost(r *importRow, actions []string, names map[string]bool) (h assets.Host, overwrote bool) {
	// An overwrite keeps the saved host, so its own name is free.
	var old assets.Host
	if r.conflict != "" && r.action == actions[1] {
		if found, ok := a.store.Get(r.conflict); ok {
			old, overwrote = found, true
			delete(names, strings.ToLower(old.Name))
		}
	}
	name := ""
	if r.named {
		name = strings.TrimSpace(r.name)
		for i := 2; name != "" && names[strings.ToLower(name)]; i++ {
			name = fmt.Sprintf("%s (%d)", strings.TrimSpace(r.name), i)
		}
	}
	if overwrote {
		h = a.importFields(old, r)
	} else {
		h = a.importFields(assets.Host{ID: assets.NewID(), CwdFollow: true}, r)
	}
	if name != "" {
		h.Name = name
	}
	names[strings.ToLower(h.Name)] = true
	return h, overwrote
}

// importFields gives h what the row says about where it is, who logs in,
// which gateway reaches it and whether its terminals become root, but not
// its secrets.
func (a *App) importFields(h assets.Host, r *importRow) assets.Host {
	h.Address, h.Port, h.User, h.Group = r.row.Address, r.row.Port, r.row.User, strings.TrimSpace(r.group)
	if r.gatewaySet {
		h.GatewayID = r.gateway
	}
	h.Auth, h.CredentialID, h.AskPassphrase = assets.AuthAsk, "", false
	h.TerminalSu, h.TerminalSuPrompt = false, ""
	if r.row.Su {
		h.TerminalSu, h.TerminalSuPrompt = true, strings.TrimSpace(r.row.SuPrompt)
	}
	if len(r.row.Password) > 0 {
		h.Auth = assets.AuthPassword
	}
	return h
}

// importSecrets is what a row writes to the encrypted store: its
// password, and its su password. Overwriting a host clears the secrets of
// a kind the row does not fill in, rather than leaving them under a new
// login.
func (a *App) importSecrets(r *importRow, h assets.Host, overwrote bool) []keychain.Change {
	var changes []keychain.Change
	if overwrote {
		if h.Auth != assets.AuthPassword {
			changes = append(changes, keychain.Change{Key: keychain.Host(h.ID, keychain.Password), Delete: true})
		}
		changes = append(changes,
			keychain.Change{Key: keychain.Host(h.ID, keychain.PrivateKey), Delete: true},
			keychain.Change{Key: keychain.Host(h.ID, keychain.Passphrase), Delete: true})
		if !h.TerminalSu {
			changes = append(changes, keychain.Change{Key: keychain.HostElevate(h.ID), Delete: true})
		}
	}
	if len(r.row.Password) > 0 {
		changes = append(changes, keychain.Change{Key: keychain.Host(h.ID, keychain.Password), Value: r.row.Password})
	}
	if r.row.Su && len(r.row.SuPassword) > 0 {
		changes = append(changes, keychain.Change{Key: keychain.HostElevate(h.ID), Value: r.row.SuPassword})
	}
	return changes
}

// pickImportFile reads a list from a file the user chooses.
func (a *App) pickImportFile(m locales.Messages) {
	f := a.imp
	if f == nil || f.picking || a.p.PickFile == nil {
		return
	}
	f.picking = true
	title := m.ImportChooseFile()
	go func() {
		path, err := a.p.PickFile(title)
		var data []byte
		if err == nil && path != "" {
			data, err = hostlist.ReadFile(path)
		}
		a.post(func() {
			if a.imp != f {
				return
			}
			f.picking = false
			if path == "" && err == nil {
				return // cancelled
			}
			if err != nil {
				f.err = importParseError(m, err)
				return
			}
			f.text = string(data)
			a.parseImport(m)
		})
	}()
}

// importGroups are the groups the saved hosts are in, for the preview's
// group field.
func (a *App) importGroups() []string {
	_, groups, _ := hostsBy(a.store, "")
	return groups
}

// importView is the sheet importing a host list. It keeps its place while
// typing: a click on the window around it does not close it.
func (a *App) importView(c *ui.Context, m locales.Messages) {
	f := a.imp
	if f == nil {
		return
	}
	tok := currentTokens(c)
	actions := importActions(m)
	sheet(stickyModal(c, &a.importing, func() {
		// Shrink and MinHeight let the preview give way when the window is
		// short, so that the buttons stay on it: a list longer than the
		// space left scrolls instead of pushing them out.
		ui.Column(c).Gap(12).Width(820).Shrink(1).MinHeight(0).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				tile(c, icons.Import, theme.Brand, 36)
				ui.Column(c).Gap(1).Shrink(1).Grow(1).Children(func() {
					sheetTitle(c, m.ImportTitle())
					ui.Text(c, m.ImportHint()).FontSize(theme.Footnote).TextColor(tok.TextMuted)
				})
				if a.p.PickFile != nil {
					if (IconButton{Icon: icons.OpenFile, Label: m.ImportChooseFile(), Disabled: f.picking}).Clicked(c, "imp:file") {
						a.pickImportFile(m)
					}
				}
			})
			area := formTextArea(c.Key("imp:text"), &f.text).Placeholder(hostlist.Example).Label(m.ImportTitle()).MinHeight(110)
			a.importHold.apply(c, f, area)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, m.ImportKeys()).FontSize(theme.Caption).TextColor(tok.TextMuted).Grow(1).Shrink(1)
				if pill(c, m.ImportPreview(), false, false, strings.TrimSpace(f.text) == "") {
					a.parseImport(m)
				}
			})
			n, existing, out := a.importCounts(m, f)
			if len(f.rows) > 0 {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.ImportSummary(strconv.Itoa(n), strconv.Itoa(out))).FontSize(theme.Body).FontWeight(500)
					ui.Spacer(c)
					if existing > 0 {
						ui.Text(c, m.ImportExisting(strconv.Itoa(existing))).FontSize(theme.Footnote).TextColor(tok.TextMuted)
						if formSelect(c.Key("imp:every"), &f.action, actions).Label(m.ImportForSaved()).Width(160).Changed() {
							importEvery(f, f.action)
						}
					}
				})
				sheetScroll(ui.Scroll(c.Key("imp:rows")).Shrink(1).MinHeight(0).MaxHeight(340)).Children(func() {
					ui.Column(c).Gap(6).FillWidth().Children(func() {
						for i, r := range f.rows {
							a.importRowView(c, m, tok, actions, i, r)
						}
					})
				})
			}
			if f.err != "" {
				ui.Text(c, f.err).FontSize(theme.Footnote).TextColor(tok.Danger)
			}
			ui.Box(c).Height(1).FillWidth().Background(tok.Line)
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if pill(c, m.Cancel(), false, false, false) {
					a.importing = false
				}
				if pill(c, m.ImportButton(strconv.Itoa(n)), true, false, n == 0) {
					a.Import(m)
				}
			})
		})
	}), tok)
	if !a.importing {
		a.imp = nil
	}
}

// importRowView is one row of the preview: what is imported, what it is
// called, where it is, and what to do when a saved host answers for it.
func (a *App) importRowView(c *ui.Context, m locales.Messages, tok theme.Tokens, actions []string, i int, r *importRow) {
	key := "imp:" + strconv.Itoa(i)
	ui.Column(c.Key(key)).Gap(4).Padding(6, 8).Radius(theme.RadiusControl).Border(1, tok.Line).FillWidth().Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth().Children(func() {
			formCheckbox(c.Key(key+":pick"), &r.pick, "").Label(m.ImportPick(r.row.Label())).Disabled(!r.row.OK()).Shrink(0)
			formTextInput(c.Key(key+":name"), &r.name).Label(m.FieldName()).Width(150).Disabled(!r.row.OK())
			ui.Text(c, r.target()).FontSize(theme.Footnote).TextColor(tok.TextMuted).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			formCombobox(c.Key(key+":group"), &r.group, a.importGroups()).Label(m.FieldGroup()).Width(130)
			if r.conflict != "" {
				formSelect(c.Key(key+":act"), &r.action, actions).Label(m.ImportConflictAction()).Width(140)
			}
		})
		var notes []string
		if r.conflict != "" {
			if old, ok := a.store.Get(r.conflict); ok {
				notes = append(notes, m.ImportConflict(old.Label()))
			}
		}
		if r.row.User != "" {
			notes = append(notes, m.FieldUser()+" "+r.row.User)
		}
		if r.row.Gateway != "" {
			notes = append(notes, m.ImportGateway(r.row.Gateway))
		}
		if r.row.Su {
			notes = append(notes, m.ImportSu())
		}
		if len(notes) > 0 {
			ui.Text(c, strings.Join(notes, " · ")).FontSize(theme.Caption).TextColor(tok.TextMuted)
		}
		if r.row.Gateway != "" && !r.gatewaySet {
			// The gateway it names is not here: the host is imported
			// without one, and the editor has to be told which.
			ui.Text(c, m.ImportGatewayMissing(r.row.Gateway)).FontSize(theme.Caption).TextColor(tok.Warning)
		}
		if r.row.Su && len(r.row.SuPassword) == 0 {
			// The host is imported as one that becomes root, without a
			// password to type: it elevates once the editor has one.
			ui.Text(c, m.TerminalSuNeedsPassword()).FontSize(theme.Caption).TextColor(tok.Warning)
		}
		if text := importIssueText(m, r.row); text != "" {
			ui.Text(c, text).FontSize(theme.Caption).TextColor(tok.Warning)
		}
	})
}

// target is where the row's host is, as the preview names it.
func (r *importRow) target() string {
	s := r.row.Address
	if r.row.User != "" {
		s = r.row.User + "@" + s
	}
	if r.row.Port != 0 && r.row.Port != 22 {
		s += ":" + strconv.Itoa(r.row.Port)
	}
	return s
}

// importIssueText says why a parsed row cannot be saved, "" when it can.
func importIssueText(m locales.Messages, r hostlist.Row) string {
	var parts []string
	for _, i := range r.Issues {
		switch i.Code {
		case hostlist.IssueHost:
			parts = append(parts, m.ImportIssueHost())
		case hostlist.IssuePort:
			parts = append(parts, m.ImportIssuePort())
		case hostlist.IssueAddress:
			parts = append(parts, m.ImportIssueAddress())
		case hostlist.IssueField:
			parts = append(parts, m.ImportIssueField(i.Field))
		case hostlist.IssueQuote:
			parts = append(parts, m.ImportIssueQuote(i.Field))
		case hostlist.IssueSuPrompt:
			parts = append(parts, m.TerminalSuInvalid())
		case hostlist.IssueSuPassword:
			parts = append(parts, m.SuPasswordInvalid())
		}
	}
	return strings.Join(parts, " · ")
}
