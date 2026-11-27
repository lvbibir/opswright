package app

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/applock"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/settings"
	"github.com/lvbibir/opswright/locales"
)

// The app lock stands in front of every window (applock): while it is
// locked the windows show only the lock screen, and the shortcuts, the
// tray's actions and the notifications naming hosts wait, while sessions,
// forwards, transfers and backups go on. It opens with the master
// password of the encrypted store, which this computer usually keeps
// cached in its own keychain (keychain.TryUnlock): the app then starts
// unlocked, and only a lock asked for by hand, or a computer that has
// never had the password typed on it, shows the lock screen. Locking
// drops the master key from memory, so the lock hides the secrets and not
// only the windows, and the rest of that run answers with the password
// alone. Idle and sleep, which must not ask for it, only hide the
// windows. A lock that can no longer be opened is reset, which deletes
// every saved secret before the lock, and the rest of the data as the app
// starts again.

// appLock is the lock and the lock screen's fields, which live only while
// it shows.
type appLock struct {
	cfg    applock.Config
	loaded bool
	locked bool
	// input is when a window last had input
	input time.Time
	// password and confirm are the lock screen's fields
	password, confirm string
	// busy is set while the master password is checked, off the UI thread
	busy bool
	err  string
	// gen is a new one for each attempt that failed, whose fields start
	// empty
	gen  int
	hold focusHold
	// manual is set by a manual lock: the rest of this run answers the
	// lock screen with the master password, never the cached key
	manual bool
	// resetOpen asks whether to reset the lock; resetting is set once it
	// was, until the app starts again
	resetOpen, resetting bool
	stopSleep            func()
	// now is time.Now, replaced in tests
	now func() time.Time
}

// minMasterPassword is the shortest master password Opswright accepts: the
// key it seals is only as strong as what an offline guess pays for.
const minMasterPassword = 8

func (a *App) lockClock() time.Time {
	if a.lock.now != nil {
		return a.lock.now()
	}
	return time.Now()
}

func (a *App) lockPath() string { return filepath.Join(a.p.DataDir, applock.File) }

// loadAppLock reads the lock settings once the data folder is known: when
// it locks by itself, and how long wrong attempts wait. What opens the
// lock is the master password, so a file that cannot be read only falls
// back to the defaults.
func (a *App) loadAppLock() {
	l := &a.lock
	if l.loaded {
		return
	}
	l.loaded = true
	l.input = a.lockClock()
	l.cfg = applock.Default()
	if a.p.DataDir == "" {
		return
	}
	cfg, err := applock.Load(a.lockPath())
	if err != nil {
		log.Printf("opswright: app lock settings unreadable, using defaults: %v", err)
		return
	}
	l.cfg = cfg
}

// appLocked reports whether the lock screen stands in front of the app.
func (a *App) appLocked() bool { return a.lock.locked }

// LockApp locks the app now, as asked by hand: the master key leaves
// memory, so every saved secret waits for the master password, and the
// rest of this run never opens with the cached one. The next start opens
// without asking again.
func (a *App) LockApp() {
	l := &a.lock
	if !a.ready() || l.locked {
		return
	}
	l.manual = true
	if err := a.kc.Lock(); err != nil {
		log.Printf("opswright: locking the secret store failed: %v", err)
	}
	a.unlock.secretsReady.Store(false)
	l.locked = true
	l.password, l.confirm, l.err, l.hold = "", "", "", focusHold{}
	l.gen++
	a.invalidate()
}

// noteInput is told of each input in a window.
func (a *App) noteInput(ui.InputEvent) { a.lock.input = a.lockClock() }

// checkIdle hides the windows once Opswright had no input for the minutes
// set, and is how long until then otherwise: 0 when it never does.
// Hiding never asks for the master password: only a lock by hand does.
func (a *App) checkIdle() time.Duration {
	l := &a.lock
	if !a.ready() || l.cfg.IdleMinutes <= 0 {
		return 0
	}
	left := time.Duration(l.cfg.IdleMinutes)*time.Minute - a.lockClock().Sub(l.input)
	if left <= 0 {
		a.hideWindows()
		l.input = a.lockClock()
		return 0
	}
	return left
}

// hideWindows hides the main window and its panels, as the idle and sleep
// choices ask, without locking: the app comes back as it was.
func (a *App) hideWindows() {
	if a.p.HideWindows != nil {
		a.p.HideWindows()
	}
}

// probeUnlock opens the store with the master key this computer cached in
// its own keychain, which needs no password. A computer without one, as
// after moving the data folder here, waits for the lock screen instead.
func (a *App) probeUnlock() {
	if !a.ready() || a.unlocked() || a.lock.manual {
		return
	}
	kc := a.kc
	go func() {
		err := kc.TryUnlock()
		if err == nil && !kc.Has(keychain.BackupKey) {
			// A store without a backup key (from before it existed, or a
			// former install): it is made when the master password is
			// entered, so do not open silently. A store that already has
			// master.enc waits for the password; one without it takes its
			// first, sealing the key it already holds.
			if kc.PasswordProtected() {
				_ = kc.Lock()
			}
			return
		}
		a.post(func() {
			if err == nil {
				a.openStoreNow()
			}
		})
	}()
}

// openStoreNow takes an unlock, whether the cached key or the master
// password gave it: the app shows its windows and starts what the store
// makes possible.
func (a *App) openStoreNow() {
	l := &a.lock
	if !l.locked {
		return
	}
	l.locked = false
	l.input = a.lockClock()
	l.password, l.confirm, l.err = "", "", ""
	a.unlock.stored = true
	a.unlock.secretsReady.Store(true)
	a.afterUnlock()
	a.invalidate()
}

// ForgetSystem drops the cached master key of this computer, so that the
// next launch asks for the master password again. It says why it could
// not.
func (a *App) ForgetSystem(m locales.Messages) string {
	if err := a.kc.ForgetSystem(); err != nil {
		return m.ErrGeneric(secretStoreErrorText(m, err))
	}
	return ""
}

// lockGate runs at the start of a window's frame: true when the lock
// screen shows in place of the window.
func (a *App) lockGate(c *ui.Context) bool {
	if left := a.checkIdle(); left > 0 {
		c.After(left)
	}
	return a.lock.locked
}

// watchSleep hides the windows as the screen locks or the computer
// sleeps, while that is set to; like the idle time, it never asks for the
// master password.
func (a *App) watchSleep() {
	if a.p.WatchSleep == nil || a.lock.stopSleep != nil {
		return
	}
	a.lock.stopSleep = a.p.WatchSleep(func() {
		a.post(func() {
			if a.lock.cfg.LockOnSleep {
				a.hideWindows()
			}
		})
	})
}

// saveLock keeps cfg as the lock.
func (a *App) saveLock(cfg applock.Config) error {
	if err := applock.Save(a.lockPath(), cfg); err != nil {
		return err
	}
	a.lock.cfg = cfg
	return nil
}

// countFailure counts a failed attempt in the lock, kept so that quitting
// does not start the count over; a failure to save it keeps it here.
func (a *App) countFailure() {
	cfg := a.lock.cfg
	cfg.Fail(a.lockClock())
	if err := a.saveLock(cfg); err != nil {
		log.Printf("opswright: app lock attempts not saved: %v", err)
		a.lock.cfg = cfg
	}
}

// waitText says how long until the next attempt, "" when it may be made.
func (a *App) waitText(m locales.Messages) string {
	w := a.lock.cfg.Wait(a.lockClock())
	if w <= 0 {
		return ""
	}
	return m.AppLockWait(strconv.Itoa(int((w + time.Second - 1) / time.Second)))
}

// SubmitAppUnlock checks the lock screen: the master password, or the
// first one, which also migrates a former install's keychain key.
func (a *App) SubmitAppUnlock(m locales.Messages) bool {
	l := &a.lock
	if !l.locked || l.busy {
		return false
	}
	l.err = ""
	if l.err = a.waitText(m); l.err != "" {
		return false
	}
	if l.password == "" {
		return false
	}
	setup := a.kc.CanUsePassword() || a.kc.CanSetPassword()
	if setup {
		switch {
		case len([]rune(l.password)) < minMasterPassword:
			l.err = m.MasterPasswordShort(strconv.Itoa(minMasterPassword))
			return false
		case l.password != l.confirm:
			l.err = m.ConfirmPasswordMismatch()
			return false
		}
	}
	pw, gen := []byte(l.password), l.gen
	l.busy = true
	go func() {
		err := a.setOrUnlockMaster(pw)
		clear(pw)
		a.post(func() { a.appUnlockDone(m, gen, err) })
	}()
	return true
}

// setOrUnlockMaster opens the store with pw. Without a master password
// yet, it sets that password: an empty store starts a new key, and one a
// former install left open, opened by the key this computer cached, seals
// the key it already holds.
func (a *App) setOrUnlockMaster(pw []byte) error {
	switch {
	case a.kc.CanUsePassword():
		return a.kc.SetupPassword(pw)
	case a.kc.CanSetPassword():
		return a.kc.SealPassword(pw)
	}
	return a.kc.UnlockPassword(pw)
}

// appUnlockDone takes the check, made against the lock as it was at gen.
func (a *App) appUnlockDone(m locales.Messages, gen int, err error) {
	l := &a.lock
	l.busy = false
	if !l.locked || l.gen != gen {
		return
	}
	switch {
	case errors.Is(err, keychain.ErrWrongPassword):
		a.countFailure()
		l.password, l.confirm, l.err = "", "", m.MasterPasswordWrong()
		l.gen++
		return
	case err != nil:
		l.err = secretStoreErrorText(m, err)
		return
	}
	cfg := l.cfg
	cfg.Succeed()
	if err := a.saveLock(cfg); err != nil {
		log.Printf("opswright: app lock attempts not saved: %v", err)
		l.cfg = cfg
	}
	a.openStoreNow()
}

// ResetAppLock is the way out of a forgotten master password: every saved
// secret is deleted, then the lock, so that removing it never hands over
// what it stood in front of; the rest of the data folder goes as the app
// starts again (finishReset), since what runs now would write it back.
// The lock screen stays until then. It says why it could not.
func (a *App) ResetAppLock(m locales.Messages) string {
	if err := a.kc.Wipe(); err != nil {
		return m.ErrGeneric(secretStoreErrorText(m, err))
	}
	// The next start deletes the rest of the data folder, so nothing of
	// it counts as saved any more.
	a.unlock.stored = false
	if err := settings.WriteFileAtomic(filepath.Join(a.p.DataDir, resetFile), nil, 0o600); err != nil {
		return m.ErrGeneric(err.Error())
	}
	l := &a.lock
	l.cfg, l.locked, l.resetting = applock.Default(), true, true
	l.password, l.confirm, l.err, l.hold = "", "", "", focusHold{}
	l.gen++
	l.resetOpen = false
	a.relaunch()
	a.invalidate()
	return ""
}

// relaunch quits and starts the app again; a quit that is cancelled
// leaves the app as it is.
func (a *App) relaunch() {
	if a.p.Relaunch != nil {
		a.p.Relaunch()
	}
}

// resetFile in the data folder asks the next start to empty it: the app
// lock was reset.
const resetFile = ".opswright-reset"

// resetFiles are what a reset deletes from the data folder, which may be
// one set from outside holding more than Opswright's: everything Opswright
// keeps there but the lifetime lock, the updater's state and the
// windows' places. resetTemps are the prefixes of the files written
// beside them before they replace them.
var (
	resetFiles = []string{"hosts.json", "settings.json", "layout.json", "known_hosts", "localkeys.json", cloudFile, applock.File, "secrets.enc", "master.enc", "ai-history", "ai-audit", "backgrounds"}
	resetTemps = []string{".tmp-", ".secrets-", ".known_hosts-"}
)

// finishReset empties dir of what resetFiles name when a reset asked
// for it, and reports whether it did. A failure keeps the request, so the
// next start tries again.
func finishReset(dir string) (bool, error) {
	marker := filepath.Join(dir, resetFile)
	if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var errs []error
	for _, name := range resetFiles {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			errs = append(errs, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if slices.ContainsFunc(resetTemps, func(p string) bool { return strings.HasPrefix(e.Name(), p) }) {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return false, err
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}

// setLockIdle and setLockOnSleep change when the lock locks by itself.
func (a *App) setLockIdle(minutes int) error {
	cfg := a.lock.cfg
	cfg.IdleMinutes = minutes
	a.lock.input = a.lockClock()
	return a.saveLock(cfg)
}

func (a *App) setLockOnSleep(on bool) error {
	cfg := a.lock.cfg
	cfg.LockOnSleep = on
	return a.saveLock(cfg)
}
