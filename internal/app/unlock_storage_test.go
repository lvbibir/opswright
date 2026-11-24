package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Licoy/sshyard/internal/assets"
	"github.com/Licoy/sshyard/internal/keychain"
	"github.com/Licoy/sshyard/locales"
	"github.com/egoist/mygo/ui"
)

func TestReloadsMetadataAfterAnotherInstance(t *testing.T) {
	dir := t.TempDir()
	platform := func() Platform {
		return Platform{DataDir: dir, SecretLockPath: filepath.Join(dir, "secrets.lock"), SecretScope: "sshyard", HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: keychain.NewMemory()}
	}
	first, err := New(platform())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	first.Start()
	tt := ui.NewTester(first.View, 1200, 760)
	tt.SetLocale("en")
	tt.Frame()
	setMasterPassword(t, first, tt, "correct horse")
	second, err := New(platform())
	if !errors.Is(err, keychain.ErrBusy) {
		t.Fatalf("second open: %v", err)
	}
	defer second.Close()
	if second.Store() != nil {
		t.Fatal("metadata opened before acquiring the lifetime lock")
	}
	second.Start()
	stt := ui.NewTester(second.View, 1200, 760)
	stt.SetLocale("en")
	stt.Frame()
	if second.ready() || !stt.HasText(en.SecretsUnlockFailed()) {
		t.Fatal("the second instance did not say why it could not read the data")
	}
	if !errors.Is(second.unlock.err, keychain.ErrBusy) || stt.HasText("private") {
		t.Fatalf("second launch: %v", second.unlock.err)
	}
	saved, err := first.Store().Add(assets.Host{Name: "added by first", Address: "host.example", User: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	second.retryPrepare()
	waitFor(t, stt, "second metadata ready", second.ready)
	if second.unlocked() {
		t.Fatal("the second instance unlocked itself")
	}
	if _, ok := second.Store().Get(saved.ID); !ok {
		t.Fatal("successful retry reused stale metadata")
	}
	if _, err := second.Store().Add(assets.Host{Name: "added by second", Address: "another.example", User: "demo"}); err != nil {
		t.Fatal(err)
	}
}

func TestUnlockFileErrorsAreSafeAndSpecific(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		m := locales.In(locale)
		for _, tc := range []struct {
			err  error
			want string
		}{
			{os.ErrPermission, m.SecretsReasonPermission()},
			{os.ErrNotExist, m.SecretsReasonNotFound()},
			{syscall.ENOSPC, m.SecretsReasonDiskFull()},
		} {
			err := &keychain.OpError{Op: "file-read", Err: &os.PathError{Op: "open", Path: "/private/sensitive/user-file", Err: tc.err}}
			got := unlockErrorText(m, err)
			if !strings.Contains(got, tc.want) || strings.Contains(got, "/private/") {
				t.Fatalf("unsafe or vague file error: %q", got)
			}
		}
	}
}

func TestLockScreenKeepsWindowControls(t *testing.T) {
	a, tt, _ := newLockedTestApp(t)
	closed := false
	a.SetWindow(WindowControls{Minimize: func() {}, Maximize: func() {}, Unmaximize: func() {}, Close: func() { closed = true }})
	tt.Frame()
	if !tt.HasText(en.MasterPasswordFirstTitle()) {
		t.Fatal("the lock screen did not show")
	}
	mustClick(t, tt, en.WindowClose())
	if !closed {
		t.Fatal("the lock screen did not retain the window close action")
	}
}
