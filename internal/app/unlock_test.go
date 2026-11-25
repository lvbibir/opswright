package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/locales"
)

// authorizationBackend is an explicitly injected store, as newTestApp's.
type authorizationBackend struct{ *keychain.Memory }

// lockedTestApp is a test app on an explicitly injected store, so no
// master password stands in the way of what the test drives.
func lockedTestApp(t *testing.T, b keychain.Backend) (*App, *ui.Tester) {
	t.Helper()
	dir := t.TempDir()
	return lockedTestAppIn(t, dir, b)
}

func lockedTestAppIn(t *testing.T, dir string, b keychain.Backend) (*App, *ui.Tester) {
	t.Helper()
	a, err := New(Platform{DataDir: dir, Secrets: b, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	a.Start()
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("en")
	tt.Frame()
	return a, tt
}

// TestStorageResolutionRecovers: a data folder that could not be read is
// retried from the window that replaced the app.
func TestStorageResolutionRecovers(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	a, _ := New(Platform{HomeDir: dir, CacheDir: dir, KDF: cheapKDF, System: keychain.NewMemory(), ResolveStorage: func() (StoragePaths, error) {
		calls++
		if calls == 1 {
			return StoragePaths{}, errors.New("private raw diagnostic")
		}
		return StoragePaths{DataDir: dir, LockPath: filepath.Join(dir, "secrets.lock"), Scope: "test"}, nil
	}})
	t.Cleanup(a.Close)
	tt := ui.NewTester(a.View, 1200, 760)
	tt.SetLocale("en")
	tt.Frame()
	if a.ready() || !tt.HasText(en.SecretsUnlockFailed()) {
		t.Fatal("a failed data folder did not replace the app")
	}
	if tt.HasText("private raw diagnostic") {
		t.Fatal("the raw error reached the window")
	}
	a.retryPrepare()
	waitFor(t, tt, "the data folder read", a.ready)
	if !a.appLocked() {
		t.Fatal("the retry left the app unlocked")
	}
}

// TestUnlockErrorWhitelist: only known reasons and stages reach the
// interface, never raw backend text.
func TestUnlockErrorWhitelist(t *testing.T) {
	for _, lang := range []string{"en", "zh-Hans"} {
		m := locales.In(lang)
		for _, err := range []error{
			&keychain.OpError{Op: "master-password", Err: errors.New("private raw diagnostic")},
			&keychain.OpError{Op: "master-legacy", Err: errors.New("private raw diagnostic")},
			&keychain.OpError{Op: "file-read", Err: os.ErrNotExist},
			&keychain.OpError{Op: "unknown-stage", Err: keychain.ErrCorrupt},
			keychain.ErrBusy, keychain.ErrWrongPassword, keychain.ErrNoPassword, keychain.ErrPasswordSetup,
		} {
			got := unlockErrorText(m, err)
			if got == "" || containsText(got, "private raw diagnostic") {
				t.Fatalf("%s: %q", lang, got)
			}
		}
	}
}

func containsText(s, sub string) bool { return strings.Contains(s, sub) }
