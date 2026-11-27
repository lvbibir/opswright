package app

import (
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/backup"
	"github.com/lvbibir/opswright/internal/backup/local"
	"github.com/lvbibir/opswright/internal/icons"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/internal/theme"
	"github.com/lvbibir/opswright/locales"
)

// localBackupRemote is the folder scheduled and manual local backups go
// to, as a place; a nil folder means there is nowhere to write.
func (a *App) localBackupRemote() *local.Folder {
	dir := a.localBackupDir()
	if dir == "" {
		return nil
	}
	return &local.Folder{Dir: dir}
}

// localBackupDir is the folder local backups go to: the chosen one, or
// opswright_bak beside the data folder.
func (a *App) localBackupDir() string {
	if d := a.settings.LocalBackup.Dir; d != "" {
		return d
	}
	if a.p.DataDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Clean(a.p.DataDir)), "opswright_bak")
}

// newestLocal is when this device's newest backup in objs was made.
func newestLocal(objs []backup.Object, device string) (time.Time, bool) {
	var t time.Time
	ok := false
	for _, o := range objs {
		if o.Device == device && (!ok || o.Time.After(t)) {
			t, ok = o.Time, true
		}
	}
	return t, ok
}

// localBackupDue reports whether a scheduled backup is due at now: there
// is none of this device's yet, or the newest is older than the interval.
func (a *App) localBackupDue(now time.Time) bool {
	r := a.localBackupRemote()
	if r == nil {
		return false
	}
	objs, err := r.List(a.cloudCtx)
	if err != nil {
		return false
	}
	newest, ok := newestLocal(objs, a.deviceID())
	if !ok {
		return true
	}
	return !now.Before(newest.Add(settings.LocalEvery(a.settings.LocalBackup.Interval)))
}

// autoLocalBackup starts the scheduled backup to the folder on this
// computer when one is due; a failure tells.
func (a *App) autoLocalBackup(now time.Time) {
	if !a.settings.LocalBackup.Enabled || a.localBusy {
		return
	}
	if !a.localBackupDue(now) {
		return
	}
	a.runLocalBackup(false)
}

// runLocalBackup writes one backup to the folder on this computer, sealed
// under the master password's backup key as an export is. manual says the
// user asked for it, so its outcome is told either way.
func (a *App) runLocalBackup(manual bool) {
	if a.localBusy || !a.unlocked() {
		return
	}
	r := a.localBackupRemote()
	if r == nil {
		return
	}
	key, err := a.kc.BackupKey()
	if err != nil {
		a.localBackupDone("", manual, err)
		return
	}
	p, err := a.collectBackup(true)
	var plain []byte
	if err == nil {
		plain, err = a.packBackup(p)
	}
	if err != nil {
		key.Clear()
		a.localBackupDone("", manual, err)
		return
	}
	device, keep := a.deviceID(), a.settings.LocalBackup.Keep
	name := backup.Name(time.Now(), device)
	a.localBusy = true
	ctx := a.cloudCtx
	go func() {
		sealed, err := key.Seal(plain)
		key.Clear()
		if err == nil {
			err = putBackup(ctx, r, name, sealed, device, keep)
		}
		a.post(func() { a.localBackupDone(name, manual, err) })
	}()
}

// localBackupDone reports how a local backup went: a scheduled one that
// worked says nothing, a manual one and any failure tell.
func (a *App) localBackupDone(name string, manual bool, err error) {
	a.localBusy = false
	if err == nil && !manual {
		return
	}
	a.toasts = append(a.toasts, func(m locales.Messages) ui.Toast {
		if err != nil {
			return ui.Toast{Title: m.LocalBackupFailed(backupErrText(m, err)), Type: "error"}
		}
		return ui.Toast{Title: m.BackupSaved(name), Type: "success"}
	})
}

// setLocalBackup changes the scheduled backup's preferences.
func (a *App) setLocalBackup(change func(*settings.LocalBackup), m locales.Messages) {
	s := a.settings
	lb := s.LocalBackup
	change(&lb)
	s.LocalBackup = lb
	if err := a.ApplySettings(s); err != nil {
		a.toastError(m.ErrGeneric(err.Error()))
	}
}

// setLocalBackupDir makes dir the folder local backups go to; "" is the
// default beside the data folder, and anything that is not an absolute
// path is refused.
func (a *App) setLocalBackupDir(dir string, m locales.Messages) {
	dir = strings.TrimSpace(dir)
	a.setLocalBackup(func(lb *settings.LocalBackup) { lb.Dir = dir }, m)
	a.localDir = a.settings.LocalBackup.Dir
	if dir != "" && a.localDir == "" {
		a.toastError(m.LocalBackupDirInvalid())
	}
}

// chooseLocalBackupDir asks for the folder local backups go to.
func (a *App) chooseLocalBackupDir(m locales.Messages) {
	if a.p.PickFolder == nil {
		return
	}
	title, start := m.LocalBackupChooseFolder(), a.localBackupDir()
	go func() {
		dir, err := a.p.PickFolder(title, start)
		a.post(func() {
			if err != nil {
				a.toastError(m.ErrGeneric(err.Error()))
				return
			}
			if dir != "" {
				a.setLocalBackupDir(dir, m)
			}
		})
	}()
}

// localIntervalLabel is the name of a scheduled backup's interval.
func localIntervalLabel(m locales.Messages, interval string) string {
	switch interval {
	case settings.LocalWeekly:
		return m.LocalEveryWeek()
	case settings.LocalMonthly:
		return m.LocalEveryMonth()
	}
	return m.LocalEveryDay()
}

// localBackupSection is the backup page's scheduled backup to a folder on
// this computer.
func (a *App) localBackupSection(c *ui.Context, m locales.Messages, tok theme.Tokens) {
	lb := a.settings.LocalBackup
	a.settingsSection(c, tok, icons.LocalDisk, m.LocalBackupSection(), func(g *settingsGroup) {
		g.row(m.LocalBackupEnabled(), m.LocalBackupEnabledNote(), func() {
			on := lb.Enabled
			if formSwitch(c.Key("lb:on"), &on).Label(m.LocalBackupEnabled()).Changed() {
				a.setLocalBackup(func(lb *settings.LocalBackup) { lb.Enabled = on }, m)
			}
		})
		g.row(m.LocalBackupInterval(), "", func() {
			opts := []string{m.LocalEveryDay(), m.LocalEveryWeek(), m.LocalEveryMonth()}
			cur := localIntervalLabel(m, lb.Interval)
			if formSelect(c.Key("lb:every"), &cur, opts).Label(m.LocalBackupInterval()).Width(140).Changed() {
				if i := slices.Index(opts, cur); i >= 0 {
					a.setLocalBackup(func(lb *settings.LocalBackup) { lb.Interval = settings.LocalIntervals[i] }, m)
				}
			}
		})
		g.row(m.BackupKeep(), m.BackupKeepNote(), func() {
			keep := float64(lb.Keep)
			if ui.NumberInput(c.Key("lb:keep"), &keep, 0, settings.LocalBackupKeepMax, 1).Label(m.BackupKeep()).Changed() {
				a.setLocalBackup(func(lb *settings.LocalBackup) { lb.Keep = int(keep) }, m)
			}
		})
		g.row(m.LocalBackupDir(), a.localBackupDir(), func() {
			in := formTextInput(c.Key("lb:dir"), &a.localDir).Placeholder(a.localBackupDir()).Label(m.LocalBackupDir())
			focused := in.Focused()
			if in.Submitted() || (a.localDirFocus && !focused) {
				if strings.TrimSpace(a.localDir) != lb.Dir {
					a.setLocalBackupDir(a.localDir, m)
				}
			}
			a.localDirFocus = focused
			if (IconButton{Icon: icons.OpenFile, Label: m.LocalBackupChooseFolder(), Disabled: a.p.PickFolder == nil}).Clicked(c, "lb:choose") {
				a.chooseLocalBackupDir(m)
			}
			if lb.Dir != "" && (IconButton{Icon: icons.Reset, Label: m.LocalBackupUseDefault()}).Clicked(c, "lb:default") {
				a.setLocalBackupDir("", m)
			}
		})
		g.row(m.LocalBackupNow(), "", func() {
			if pill(c, m.LocalBackupNow(), false, false, a.localBusy) {
				a.runLocalBackup(true)
			}
		})
		g.row(m.RestoreFromLocal(), "", func() {
			if pill(c, m.Restore(), false, false, a.localBackupDir() == "") {
				a.OpenLocalList()
			}
		})
	})
	ui.Text(c, m.LocalBackupNote()).FontSize(theme.Footnote).TextColor(tok.TextMuted).Padding(0, 4)
}
