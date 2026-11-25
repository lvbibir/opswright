package app

import (
	"errors"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/icons"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/theme"
	"github.com/lvbibir/opswright/locales"
)

// The master password seals the store's master key (keychain), and so
// opens both saved secrets and backups. It is asked for by the lock
// screen; the About page changes it, re-sealing the same master key, so
// no secret has to be re-encrypted. The password lives only in the sheet
// while it is open.

type masterPwForm struct {
	current, password, confirm string
	// busy is set while the key is derived, off the UI thread
	busy bool
	err  string
}

func (a *App) openMasterPassword() {
	a.masterPw, a.masterPwOpen = &masterPwForm{}, true
}

func (a *App) closeMasterPassword() {
	a.masterPw, a.masterPwOpen = nil, false
}

// SubmitMasterPassword checks the sheet, then reseals the master key off
// the UI thread, since deriving takes a moment.
func (a *App) SubmitMasterPassword(m locales.Messages) bool {
	f := a.masterPw
	if f == nil || f.busy || !a.unlocked() {
		return false
	}
	f.err = ""
	switch {
	case f.current == "":
		return false
	case len([]rune(f.password)) < minMasterPassword:
		f.err = m.MasterPasswordShort(strconv.Itoa(minMasterPassword))
		return false
	case f.password != f.confirm:
		f.err = m.ConfirmPasswordMismatch()
		return false
	}
	current, password := []byte(f.current), []byte(f.password)
	f.busy = true
	kc := a.kc
	go func() {
		err := kc.ChangePassword(current, password)
		clear(current)
		clear(password)
		a.post(func() { a.masterPasswordDone(m, f, err) })
	}()
	return true
}

// masterPasswordDone ends the sheet's action: a wrong password stays in
// the sheet.
func (a *App) masterPasswordDone(m locales.Messages, f *masterPwForm, err error) {
	f.busy = false
	if a.masterPw != f {
		return
	}
	switch {
	case errors.Is(err, keychain.ErrWrongPassword):
		f.err = m.MasterPasswordWrong()
	case err != nil:
		f.err = secretStoreErrorText(m, err)
	default:
		a.closeMasterPassword()
	}
}

// masterPasswordView is the sheet changing the master password.
func (a *App) masterPasswordView(c *ui.Context, m locales.Messages) {
	f := a.masterPw
	if f == nil {
		return
	}
	tok := currentTokens(c)
	sheet(stickyModal(c, &a.masterPwOpen, func() {
		ui.Column(c).Gap(18).Width(420).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				tile(c, icons.Password, tok.TintAt(6), 36)
				sheetTitle(c, m.MasterPasswordChangeTitle())
			})
			// Each field submits the sheet; the first one holds the focus.
			first := true
			field := func(key, label string, value *string) {
				ui.Field(c, label, func() {
					in := formTextInput(c.Key(key), value).Password().Disabled(f.busy)
					if first {
						in.AutoFocus()
						a.masterPwHold.apply(c, f, in)
						first = false
					}
					if in.Submitted() {
						a.SubmitMasterPassword(m)
					}
				})
			}
			ui.Form(c, func() {
				field("mp:current", m.CurrentMasterPassword(), &f.current)
				field("mp:pw", m.NewMasterPassword(), &f.password)
				field("mp:confirm", m.ConfirmPassword(), &f.confirm)
			})
			ui.Text(c, m.MasterPasswordChangeNote()).FontSize(theme.Footnote).TextColor(tok.TextMuted)
			if f.err != "" {
				ui.Text(c, f.err).FontSize(theme.Footnote).TextColor(tok.Danger)
			}
			ui.Box(c).Height(1).FillWidth().Background(tok.Line)
			ui.Row(c).Gap(8).Children(func() {
				ui.Spacer(c)
				if pill(c, m.Cancel(), false, false, false) {
					a.masterPwOpen = false
				}
				if pill(c, m.Save(), true, false, f.busy) {
					a.SubmitMasterPassword(m)
				}
			})
		})
	}), tok)
	if !a.masterPwOpen && a.masterPw == f {
		a.closeMasterPassword()
	}
}
