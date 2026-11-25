package keychain

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lvbibir/sshyard/internal/vault"
)

// testPassword is the master password the tests set up their stores with.
const testPassword = "correct horse battery staple"

// cheapKDF keeps the derivation quick in tests; the app uses vault's default.
var cheapKDF = vault.Params{Time: 1, Memory: 8, Threads: 1}

func options(t *testing.T) OpenOptions {
	t.Helper()
	dir := t.TempDir()
	return OpenOptions{Path: filepath.Join(dir, "data", "secrets.enc"), LockPath: filepath.Join(dir, "app", "secrets.lock"), Scope: "test.sshyard", KDF: cheapKDF, System: NewMemory()}
}

// openLockedTest opens a store and leaves it as it is: locked unless the
// caller unlocks it.
func openLockedTest(t *testing.T, o OpenOptions) *Keychain {
	t.Helper()
	k, err := OpenLocked(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	return k
}

// openTest sets the master password of a fresh store and leaves it
// unlocked, as SetupPassword does.
func openTest(t *testing.T, o OpenOptions) *Keychain {
	t.Helper()
	k := openLockedTest(t, o)
	if err := k.SetupPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	return k
}

// unlockTest opens an existing store with the master password set up by
// openTest or setupPassword.
func unlockTest(t *testing.T, o OpenOptions, password string) *Keychain {
	t.Helper()
	k := openLockedTest(t, o)
	if err := k.UnlockPassword([]byte(password)); err != nil {
		t.Fatal(err)
	}
	return k
}

func masterPath(o OpenOptions) string { return filepath.Join(filepath.Dir(o.Path), masterFile) }

func mustGet(t *testing.T, k *Keychain, key string, want []byte) {
	t.Helper()
	got, err := k.Get(key)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("value mismatch for %s: %v", key, err)
	}
}

func mustGetMissing(t *testing.T, k *Keychain, key string) {
	t.Helper()
	if _, err := k.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSetGetDelete(t *testing.T) {
	m := NewMemory()
	k := New(m)
	if _, err := k.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := k.Set(Cred("h1", Password), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	if value, err := k.Get(Cred("h1", Password)); err != nil || string(value) != "secret" {
		t.Fatalf("roundtrip: %q %v", value, err)
	}
	if !k.Has(Cred("h1", Password)) || k.Has(Cred("h1", PrivateKey)) {
		t.Fatal("Has")
	}
	if err := k.DeleteCred("h1"); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete("missing"); err != nil {
		t.Fatal(err)
	}
	if m.Len() != 0 {
		t.Fatal("secret remains")
	}
	m.Fail = ErrUnavailable
	if err := k.Set("x", []byte("x")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := k.Exists("x"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestKeys(t *testing.T) {
	if Cred("c1", PrivateKey) != "cred/c1/key" || ProxyPassword("p1") != "proxy/p1/password" || ForwardPassword("f1") != "forward/f1/password" || AIKey("p1") != "ai/p1/apikey" || AIAgentEnv("codex", "API_KEY") != "ai/codex/env/API_KEY" {
		t.Fatal("keys changed")
	}
}

func TestEncryptedRoundTrip(t *testing.T) {
	o := options(t)
	k := openTest(t, o)
	secret := []byte(strings.Repeat("large-private-key-", 1000))
	key := Cred("c", PrivateKey)
	if err := k.Set(key, secret); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(o.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(before, secret) || bytes.Contains(before, []byte(key)) {
		t.Fatal("plaintext on disk")
	}
	sealedMaster, err := os.ReadFile(masterPath(o))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealedMaster, secret) || bytes.Contains(sealedMaster, []byte(testPassword)) {
		t.Fatal("plaintext in master.enc")
	}
	if err := k.Set(key, secret); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(o.Path)
	if bytes.Equal(before, after) {
		t.Fatal("nonce reused")
	}
	mustGet(t, k, key, secret)
	got, _ := k.Get(key)
	got[0] ^= 1
	mustGet(t, k, key, secret)
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(key); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if err := k.Set(key, secret); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	reopened := unlockTest(t, o, testPassword)
	mustGet(t, reopened, key, secret)
	if err := reopened.DeleteCred("c"); err != nil {
		t.Fatal(err)
	}
	if ok, err := reopened.Exists(key); ok || err != nil {
		t.Fatal("deleted key", err)
	}
}

func TestOpenRejectsMasterDamage(t *testing.T) {
	for _, kind := range []string{"missing", "short", "wrong", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			o := options(t)
			k := openTest(t, o)
			if err := k.Set("secret", []byte("value")); err != nil {
				t.Fatal(err)
			}
			k.Close()
			path := masterPath(o)
			want := ErrMasterInvalid
			switch kind {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				want = ErrMasterMissing
			case "short":
				if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "wrong":
				// A master.enc of another password: the sealed key differs.
				o2 := options(t)
				setupPassword(t, o2, "another password")
				other, err := os.ReadFile(masterPath(o2))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, other, 0o600); err != nil {
					t.Fatal(err)
				}
				want = ErrWrongPassword
			case "corrupt":
				if err := os.WriteFile(path, []byte("damaged"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(o.Path)
			k = openLockedTest(t, o)
			if err := k.UnlockPassword([]byte(testPassword)); !errors.Is(err, want) || k.Unlocked() {
				t.Fatalf("unlock: %v", err)
			}
			if k.CanUsePassword() || k.PasswordProtected() != (kind != "missing") {
				t.Fatalf("state: canUse=%v protected=%v", k.CanUsePassword(), k.PasswordProtected())
			}
			if err := k.SetupPassword([]byte("new")); !errors.Is(err, ErrPasswordSetup) {
				t.Fatalf("setup over an existing store: %v", err)
			}
			after, _ := os.ReadFile(o.Path)
			if !bytes.Equal(before, after) {
				t.Fatal("existing store overwritten")
			}
		})
	}
}

func TestOpenRejectsCiphertextDamageAndScope(t *testing.T) {
	for _, kind := range []string{"truncated", "tampered", "version", "scope"} {
		t.Run(kind, func(t *testing.T) {
			o := options(t)
			k := openTest(t, o)
			if err := k.Set("secret", []byte("value")); err != nil {
				t.Fatal(err)
			}
			k.Close()
			data, _ := os.ReadFile(o.Path)
			want := ErrCorrupt
			switch kind {
			case "truncated":
				data = data[:5]
			case "tampered":
				data[len(data)-1] ^= 1
			case "version":
				data[len(fileMagic)] = 2
				want = ErrVersion
			case "scope":
				o.Scope = "different.app"
			}
			if err := os.WriteFile(o.Path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			k = openLockedTest(t, o)
			if err := k.UnlockPassword([]byte(testPassword)); !errors.Is(err, want) || k.Unlocked() {
				t.Fatalf("unlock: %v", err)
			}
			if kind == "scope" || kind == "tampered" || kind == "truncated" || kind == "version" {
				if _, err := os.ReadFile(masterPath(o)); err != nil {
					t.Fatal("master file gone", err)
				}
			}
		})
	}
}
