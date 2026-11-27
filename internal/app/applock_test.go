package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/applock"
	"github.com/lvbibir/opswright/internal/keychain"
)

// lockTestApp is a locked test app whose clock the test moves.
func lockTestApp(t *testing.T) (*App, *ui.Tester, *time.Time) {
	t.Helper()
	a, tt, _ := newLockedTestApp(t)
	now := time.Unix(1_700_000_000, 0)
	a.lock.now = func() time.Time { return now }
	a.lock.input = now
	return a, tt, &now
}

// TestAppLockMasterPassword: a store without a master password starts
// locked and the lock screen sets one; it is never written down, a wrong
// one is counted on disk, and the right one opens the app.
func TestAppLockMasterPassword(t *testing.T) {
	a, tt, _ := lockTestApp(t)
	dir := a.p.DataDir
	if !a.appLocked() || !a.kc.CanUsePassword() {
		t.Fatal("a fresh store is not waiting for its master password")
	}
	// Too short, and a mismatch, both refused.
	a.lock.password, a.lock.confirm = "short", "short"
	if a.SubmitAppUnlock(en) || a.lock.err == "" {
		t.Fatal("short master password accepted")
	}
	a.lock.password, a.lock.confirm = "correct horse", "battery staple"
	if a.SubmitAppUnlock(en) || a.lock.err == "" {
		t.Fatal("mismatched master password accepted")
	}
	setMasterPassword(t, a, tt, "correct horse")
	if a.appLocked() || a.kc.CanUsePassword() {
		t.Fatal("the master password did not open the app")
	}
	if _, err := os.Stat(filepath.Join(dir, "master.enc")); err != nil {
		t.Fatal("master.enc missing", err)
	}
	// The password itself is nowhere on disk.
	for _, name := range []string{"master.enc", "secrets.enc", "applock.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "correct horse") {
			t.Fatalf("the master password is in %s", name)
		}
	}
	// Locking drops the master key; a wrong password is counted and
	// waits; the right one opens it again.
	a.LockApp()
	if !a.appLocked() || a.unlocked() {
		t.Fatal("locking left the store open")
	}
	unlockApp(t, a, tt, "not the password")
	if !a.appLocked() || a.lock.err == "" {
		t.Fatal("a wrong master password opened the app")
	}
	if a.lock.cfg.Failures != 1 {
		t.Fatalf("failed attempts %d", a.lock.cfg.Failures)
	}
	cfg, err := applock.Load(filepath.Join(dir, applock.File))
	if err != nil || cfg.Failures != 1 {
		t.Fatalf("failed attempts not kept: %+v %v", cfg, err)
	}
	unlockApp(t, a, tt, "correct horse")
	if a.appLocked() || !a.unlocked() {
		t.Fatalf("the right master password did not open the app: %q", a.lock.err)
	}
	if a.lock.cfg.Failures != 0 {
		t.Fatal("the success did not forget the failures")
	}
}

// TestAppLockWait: too many wrong attempts make the next one wait.
func TestAppLockWait(t *testing.T) {
	a, tt, now := lockTestApp(t)
	setMasterPassword(t, a, tt, "correct horse")
	a.LockApp()
	for range 6 {
		unlockApp(t, a, tt, "wrong")
	}
	if w := a.lock.cfg.Wait(*now); w <= 0 {
		t.Fatal("failed attempts do not wait")
	}
	if msg := a.waitText(en); msg == "" || !strings.Contains(msg, "s") {
		t.Fatalf("wait text %q", msg)
	}
	a.lock.password = "correct horse"
	if a.SubmitAppUnlock(en) {
		t.Fatal("a wait did not refuse the attempt")
	}
	*now = now.Add(time.Hour)
	unlockApp(t, a, tt, "correct horse")
	if a.appLocked() {
		t.Fatal("the wait outlived its time")
	}
}

// TestAppLockReset: the reset throws away every saved secret and asks the
// next start to empty the rest of the data folder.
func TestAppLockReset(t *testing.T) {
	a, tt, _ := lockTestApp(t)
	dir := a.p.DataDir
	setMasterPassword(t, a, tt, "correct horse")
	if err := a.kc.Set(keychain.Cred("c1", keychain.Password), []byte("hunter2")); err != nil {
		t.Fatal(err)
	}
	a.LockApp()
	if msg := a.ResetAppLock(en); msg != "" {
		t.Fatal(msg)
	}
	if !a.lock.resetting || !a.appLocked() {
		t.Fatal("the reset did not keep the lock screen")
	}
	if _, err := os.Stat(filepath.Join(dir, resetFile)); err != nil {
		t.Fatal("the reset marker is missing", err)
	}
	// The secrets are gone; the marker empties the rest at the next start.
	a.unlock.secretsReady.Store(true)
	if _, err := a.kc.Get(keychain.Cred("c1", keychain.Password)); err == nil {
		t.Fatal("a reset kept a secret")
	}
}

// TestAppLockUnreadableSettings: the file that says when the lock locks
// opens nothing, so an unreadable one falls back to the defaults.
func TestAppLockUnreadableSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, applock.File), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := New(Platform{DataDir: dir, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: keychain.NewMemory()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if a.lock.cfg != applock.Default() {
		t.Fatalf("lock settings %+v", a.lock.cfg)
	}
	if !a.appLocked() {
		t.Fatal("an unreadable lock setting unlocked the app")
	}
}

// TestAppLockRememberedKey: this computer caches the master key when the
// password is set, so it starts unlocked; a manual lock asks for the
// password again for the rest of the run, and the next launch does not.
func TestAppLockRememberedKey(t *testing.T) {
	dir := t.TempDir()
	system := keychain.NewMemory()
	platform := func() Platform {
		return Platform{DataDir: dir, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: system}
	}
	a, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Start()
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("en")
	tt.Frame()
	if !a.appLocked() || !a.kc.CanUsePassword() {
		t.Fatal("a new install did not wait for its master password")
	}
	setMasterPassword(t, a, tt, "correct horse")
	if _, err := system.Get(keychain.MasterKey); err != nil {
		t.Fatalf("the master key was not cached on this computer: %v", err)
	}
	a.Close()

	// A fresh launch with the cache starts unlocked, no password asked.
	again, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	again.Start()
	stt := ui.NewTester(again.View, 1200, 760)
	stt.SetLocale("en")
	waitFor(t, stt, "the remembered key opened the store", again.unlocked)
	if again.appLocked() {
		t.Fatal("the remembered key did not open the app")
	}
	// A manual lock asks for the password, and the cache is not used again
	// this run.
	again.LockApp()
	if !again.appLocked() || again.unlocked() {
		t.Fatal("the manual lock left the store open")
	}
	unlockApp(t, again, stt, "correct horse")
	if again.appLocked() {
		t.Fatalf("the password did not clear the manual lock: %q", again.lock.err)
	}
	again.Close()

	// The next launch remembers again.
	third, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(third.Close)
	third.Start()
	ttt := ui.NewTester(third.View, 1200, 760)
	ttt.SetLocale("en")
	waitFor(t, ttt, "the next launch opened by itself", third.unlocked)
	if third.appLocked() {
		t.Fatal("the launch after a manual lock did not remember")
	}
}

// TestAppLockNewComputer: a data folder carried to a computer whose
// keychain knows nothing of it asks for the master password, and
// remembers it from then on.
func TestAppLockNewComputer(t *testing.T) {
	dir := t.TempDir()
	first := keychain.NewMemory()
	a, err := New(Platform{DataDir: dir, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: first})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Start()
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("en")
	tt.Frame()
	setMasterPassword(t, a, tt, "correct horse")
	if err := a.kc.Set(keychain.Cred("c1", keychain.Password), []byte("hunter2")); err != nil {
		t.Fatal(err)
	}
	a.Close()

	// Another computer: the same folder, an empty keychain.
	second := keychain.NewMemory()
	b, err := New(Platform{DataDir: dir, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	b.Start()
	stt := ui.NewTester(b.View, 1200, 760)
	stt.SetLocale("en")
	stt.Frame()
	if !b.appLocked() || b.kc.CanUsePassword() {
		t.Fatal("a new computer did not ask for the master password")
	}
	if stt.HasText(en.MasterPasswordFirstTitle()) {
		t.Fatal("a new computer offered to set a password instead of asking for one")
	}
	unlockApp(t, b, stt, "wrong horse")
	if !b.appLocked() {
		t.Fatal("a wrong master password opened the store")
	}
	unlockApp(t, b, stt, "correct horse")
	if got, err := b.kc.Get(keychain.Cred("c1", keychain.Password)); err != nil || string(got) != "hunter2" {
		t.Fatalf("the store did not open: %q %v", got, err)
	}
	if _, err := second.Get(keychain.MasterKey); err != nil {
		t.Fatalf("the password was not remembered on this computer: %v", err)
	}
}

// TestAppLockIdleHidesWindows: the idle time and sleep hide the windows,
// never locking them and never asking for the password.
func TestAppLockIdleHidesWindows(t *testing.T) {
	a, tt, now := lockTestApp(t)
	setMasterPassword(t, a, tt, "correct horse")
	hides := 0
	a.p.HideWindows = func() { hides++ }
	if a.lock.cfg.IdleMinutes != applock.Default().IdleMinutes {
		t.Fatalf("default idle minutes %d", a.lock.cfg.IdleMinutes)
	}
	*now = now.Add(time.Duration(a.lock.cfg.IdleMinutes) * time.Minute)
	a.checkIdle()
	if hides != 1 || a.appLocked() || !a.unlocked() {
		t.Fatalf("idle hid %d times, locked=%v", hides, a.appLocked())
	}
	if err := a.setLockIdle(0); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(24 * time.Hour)
	a.checkIdle()
	if hides != 1 {
		t.Fatal("idle 0 hid the windows")
	}
	// Sleep hides while that is on, and locks nothing either.
	var sleep func()
	a.p.WatchSleep = func(f func()) func() { sleep = f; return func() {} }
	a.lock.stopSleep = nil
	a.watchSleep()
	if sleep == nil {
		t.Fatal("sleep was not watched")
	}
	if err := a.setLockOnSleep(true); err != nil {
		t.Fatal(err)
	}
	sleep()
	waitFor(t, tt, "the sleep hide", func() bool { return hides == 2 })
	if a.appLocked() {
		t.Fatal("sleep locked the app")
	}
}

// TestAppLockMakesMissingBackupKey: a store that is protected but has no
// backup key is not opened silently; the master password is asked for, and
// entering it makes the key, so a backup can be made.
func TestAppLockMakesMissingBackupKey(t *testing.T) {
	dir := t.TempDir()
	system := keychain.NewMemory()
	platform := func() Platform {
		return Platform{DataDir: dir, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: system}
	}
	a, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Start()
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("en")
	tt.Frame()
	setMasterPassword(t, a, tt, "correct horse")
	if err := a.kc.Delete(keychain.BackupKey); err != nil {
		t.Fatal(err)
	}
	a.Close()

	again, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	again.Start()
	stt := ui.NewTester(again.View, 1200, 760)
	stt.SetLocale("en")
	stt.Frame()
	if !again.appLocked() {
		t.Fatal("a store without a backup key opened by itself")
	}
	if stt.HasText(en.MasterPasswordFirstTitle()) {
		t.Fatal("a protected store offered to set a password instead of asking for one")
	}
	unlockApp(t, again, stt, "correct horse")
	if again.appLocked() {
		t.Fatalf("the password did not open the store: %q", again.lock.err)
	}
	if _, err := again.kc.BackupKey(); err != nil {
		t.Fatalf("the backup key was not made: %v", err)
	}
}

// TestAppLockSealsFormerInstallKey: a store without master.enc, opened by
// the key this computer cached, is not opened silently; the lock screen
// sets the first master password, which seals the key it holds and makes
// the backup key.
func TestAppLockSealsFormerInstallKey(t *testing.T) {
	dir := t.TempDir()
	system := keychain.NewMemory()
	platform := func() Platform {
		return Platform{DataDir: dir, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: system}
	}
	a, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Start()
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("en")
	tt.Frame()
	setMasterPassword(t, a, tt, "correct horse")
	if err := a.kc.Set(keychain.Cred("c1", keychain.Password), []byte("hunter2")); err != nil {
		t.Fatal(err)
	}
	if err := a.kc.Delete(keychain.BackupKey); err != nil {
		t.Fatal(err)
	}
	a.Close()
	// A former install: secrets.enc without master.enc.
	if err := os.Remove(filepath.Join(dir, "master.enc")); err != nil {
		t.Fatal(err)
	}

	again, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	again.Start()
	stt := ui.NewTester(again.View, 1200, 760)
	stt.SetLocale("en")
	stt.Frame()
	if !again.appLocked() {
		t.Fatal("a former install opened by itself")
	}
	if !again.kc.CanSetPassword() {
		t.Fatal("the former install did not offer to set a password")
	}
	setMasterPassword(t, again, stt, "correct horse")
	if !again.kc.PasswordProtected() {
		t.Fatal("the store did not take a master password")
	}
	if _, err := again.kc.BackupKey(); err != nil {
		t.Fatalf("the backup key was not made: %v", err)
	}
	if got, err := again.kc.Get(keychain.Cred("c1", keychain.Password)); err != nil || string(got) != "hunter2" {
		t.Fatalf("the store lost its secrets: %q %v", got, err)
	}
}
