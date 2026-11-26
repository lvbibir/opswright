package app

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/internal/layout"
	"github.com/lvbibir/opswright/internal/localkeys"
	"github.com/lvbibir/opswright/internal/session"
)

func TestLayoutRememberedLocalCredentialsWaitForUnlock(t *testing.T) {
	for _, where := range []string{"target", "none"} {
		t.Run(where, func(t *testing.T) {
			dir := t.TempDir()
			rememberLayoutIn(t, dir)
			a, _ := appIn(t, dir, nil)
			h := savedHost(t, a, "target", "127.0.0.1")
			rememberedHost := h
			// A real encrypted key makes default AuthAsk authentication reach the
			// network after credential selection; the dial then fails deliberately.
			pub := homeKey(t, a, "id_ed25519", "test-passphrase")
			keyID := session.KeyID(pub)
			if where != "none" {
				if err := a.lk.Remember(localkeys.Remembered{ID: keyID}); err != nil {
					t.Fatal(err)
				}
				if err := a.lk.SetLast(rememberedHost.ID, keyID); err != nil {
					t.Fatal(err)
				}
			}
			pane, err := a.makeConn(h.ID, layout.Terminal, 0)
			if err != nil {
				t.Fatal(err)
			}
			a.ws.Open(pane.item())
			a.Close()
			// The next start has a real store under a master password, so
			// the remembered key stays shut until that password opens it.
			store, err := keychain.OpenLocked(keychain.OpenOptions{Path: filepath.Join(dir, "secrets.enc"), LockPath: filepath.Join(dir, "secrets.lock"), Scope: "opswright", KDF: cheapKDF})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetupPassword([]byte(testBackupPassword)); err != nil {
				t.Fatal(err)
			}
			if where != "none" {
				if err := store.Set(keychain.LocalKeyPassphrase(keyID), []byte("test-passphrase")); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			var dials atomic.Int32
			again, err := New(Platform{DataDir: dir, HomeDir: dir, CacheDir: filepath.Join(dir, "cache"), KDF: cheapKDF, System: keychain.NewMemory(), Dial: func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				return nil, errors.New("test dial stopped")
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(again.Close)
			again.Start()
			tt := ui.NewTester(again.View, 1200, 760)
			tt.SetLocale("en")
			tt.Frame()
			if !again.appLocked() {
				t.Fatal("the next start did not wait for the master password")
			}
			restored := again.focused()
			if restored == nil || !restored.restoredQuiet {
				t.Fatal("missing quiet restored pane")
			}
			if where != "none" {
				if dials.Load() != 0 || !restored.waitingSecrets || !restored.restorePending || restored.s.Status().State != session.Idle {
					t.Fatal("remembered passphrase connection started before unlock")
				}
				if !again.restoredConnectionNeedsSecrets(h) {
					t.Fatal("remembered metadata not recognized")
				}
			}
			unlockApp(t, again, tt, testBackupPassword)
			waitFor(t, tt, "fresh noninteractive dial", func() bool { return dials.Load() == 1 && restored.s.Status().State == session.Failed })
			if again.prompting {
				t.Fatal("restoration opened an interactive prompt")
			}
			if where != "none" && (!again.unlocked() || restored.restorePending || restored.waitingSecrets) {
				t.Fatal("unlock did not start the pending connection once")
			}
		})
	}
}
