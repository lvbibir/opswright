package app

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/sshyard/internal/applock"
	"github.com/lvbibir/sshyard/internal/icons"
	"github.com/lvbibir/sshyard/internal/theme"
	"github.com/lvbibir/sshyard/locales"
)

// idleLabel names a choice of applock.IdleChoices.
func idleLabel(m locales.Messages, minutes int) string {
	if minutes <= 0 {
		return m.AppLockNever()
	}
	return m.AppLockMinutes(strconv.Itoa(minutes))
}

// lockView is the main window while the app is locked: the master
// password opens it, or, before one is set, sets it; the store's master
// key is sealed under it, and a key a former SSHYard kept in the OS
// keychain moves under it too.
func (a *App) lockView(c *ui.Context) {
	base := *c.Theme()
	base.Font = a.fonts.ui
	tok := theme.For(a.settings.Theme, base.Dark)
	c.SetTheme(tok.Apply(base))
	m := locales.In(c.Locale())
	l := &a.lock
	wait := a.waitText(m)
	if wait != "" {
		c.After(time.Second)
	}
	// A store ready for its first password; one that already has a
	// master.enc, or holds secrets without one, asks for the password.
	first := a.kc.CanUsePassword()
	title, note, action := m.AppLockedTitle(), m.AppLockedPrompt(), m.AppLockUnlock()
	if first {
		title, note, action = m.MasterPasswordFirstTitle(), m.MasterPasswordFirstNote(), m.MasterPasswordSet()
	}
	ui.Column(c).Fill().Background(tok.Canvas).TextColor(tok.Text).Children(func() {
		ui.Row(c.Key("locked-title")).Height(bandHeight(c)).FillWidth().DragWindow().Children(func() {
			ui.Spacer(c)
			a.windowButtons(c, m, tok)
		})
		ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space(4)).Children(func() {
			ui.Image(c, a.logo).Size(64, 64).Radius(theme.RadiusSheet).Label(m.AppName())
			sheetTitle(c, title)
			// A reset waits for the app to start again; a cancelled quit
			// leaves it here.
			if l.resetting {
				ui.Text(c, m.AppLockResetPending()).FontSize(theme.Body).TextColor(tok.TextMuted).TextAlign(ui.Center).MaxWidth(420)
				if pill(c, m.AppLockResetRestart(), true, false, false) {
					a.relaunch()
				}
				return
			}
			ui.Text(c, note).FontSize(theme.Body).TextColor(tok.TextMuted).TextAlign(ui.Center).MaxWidth(460)
			ui.Column(c).Width(320).Gap(theme.Space(3)).Children(func() {
				ui.Form(c, func() {
					focusFirst := true
					field := func(key, label string, value *string) {
						ui.Field(c, label, func() {
							in := formTextInput(c.Key(key), value).Password().Disabled(l.busy)
							if focusFirst {
								in.AutoFocus()
								l.hold.apply(c, l.gen, in)
								focusFirst = false
							}
							if in.Submitted() {
								a.SubmitAppUnlock(m)
							}
						})
					}
					field("lock:pw:"+strconv.Itoa(l.gen), m.MasterPassword(), &l.password)
					if first {
						field("lock:confirm:"+strconv.Itoa(l.gen), m.ConfirmPassword(), &l.confirm)
					}
				})
				if msg := cmp.Or(wait, l.err); msg != "" {
					ui.Text(c, msg).FontSize(theme.Footnote).TextColor(tok.Danger)
				}
				ui.Row(c).FillWidth().Children(func() {
					if pill(c, m.AppLockResetEllipsis(), false, false, l.busy) {
						l.resetOpen = true
					}
					ui.Spacer(c)
					if pill(c, action, true, false, l.busy || wait != "") {
						a.SubmitAppUnlock(m)
					}
				})
			})
		})
	})
	if ui.AlertDialog(c, &l.resetOpen, m.AppLockResetTitle(), m.AppLockResetBody(), m.Cancel(), m.AppLockReset()) == 1 {
		if msg := a.ResetAppLock(m); msg != "" {
			l.err = msg
		}
	}
}

// lockedWindowView is an editor's window while the app is locked: it
// opens in the main window, and the window's title names no file.
func (a *App) lockedWindowView(c *ui.Context, cn *conn) {
	base := *c.Theme()
	base.Font = a.fonts.ui
	tok := theme.For(a.settings.Theme, base.Dark)
	c.SetTheme(tok.Apply(base))
	c.Root().Background(tok.Canvas)
	m := locales.In(c.Locale())
	if ew := cn.ed; ew.win != nil && ew.title != m.AppName() {
		ew.title = m.AppName()
		w, title := ew.win, ew.title
		go w.SetTitle(title)
	}
	ui.Column(c).Fill().TextColor(tok.Text).AlignItems(ui.Center).Justify(ui.Center).Gap(theme.Space(4)).Children(func() {
		glyph(c, icons.Lock, tok.TextMuted, 32)
		sheetTitle(c, m.AppLockedTitle())
		if pill(c, m.AppLockShowMain(), true, false, false) {
			a.activateMainWindow()
		}
	})
}

// securityPage is the settings' page of the app lock: when it locks by
// itself, and the way to lock it now. The lock itself is the master
// password, so the page keeps nothing that could turn it off.
func (a *App) securityPage(c *ui.Context, m locales.Messages, tok theme.Tokens) {
	cfg := a.lock.cfg
	a.settingsSection(c, tok, icons.Lock, m.AppLockSection(), func(g *settingsGroup) {
		g.row(m.AppLockIdle(), m.AppLockIdleNote(), func() {
			labels := make([]string, len(applock.IdleChoices))
			for i, n := range applock.IdleChoices {
				labels[i] = idleLabel(m, n)
			}
			sel := idleLabel(m, cfg.IdleMinutes)
			if formSelect(c.Key("lockidle"), &sel, labels).Label(m.AppLockIdle()).Width(140).Changed() {
				if i := slices.Index(labels, sel); i >= 0 {
					if err := a.setLockIdle(applock.IdleChoices[i]); err != nil {
						c.AddToast(ui.Toast{Title: m.ErrGeneric(err.Error()), Type: "error"})
					}
				}
			}
		})
		g.row(m.AppLockOnSleep(), "", func() {
			sleep := cfg.LockOnSleep
			if formSwitch(c.Key("locksleep"), &sleep).Label(m.AppLockOnSleep()).Changed() {
				if err := a.setLockOnSleep(sleep); err != nil {
					c.AddToast(ui.Toast{Title: m.ErrGeneric(err.Error()), Type: "error"})
				}
			}
		})
		g.row(m.AppLockNow(), "", func() {
			if pill(c, m.AppLockAction(), false, false, false) {
				a.LockApp()
			}
		})
	})
	ui.Text(c, m.AppLockScopeNote()).FontSize(theme.Footnote).TextColor(tok.TextMuted).Padding(0, 4)
}
