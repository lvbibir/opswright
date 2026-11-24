package keychain

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"sync"
	"testing"
)

func TestOpenLockedEmptyStaysLocked(t *testing.T) {
	o := options(t)
	k := openLockedTest(t, o)
	if k.Unlocked() {
		t.Fatal("new store unlocked")
	}
	if k.PasswordProtected() {
		t.Fatal("new store protected")
	}
	mustGetMissing(t, k, "missing")
	if exists, err := k.Exists("missing"); exists || err != nil {
		t.Fatalf("empty store existence: %v %v", exists, err)
	}
	if err := k.Delete("missing"); err != nil {
		t.Fatal(err)
	}
	if err := k.DeleteCred("missing"); err != nil {
		t.Fatal(err)
	}
	if err := k.Apply(nil); err != nil {
		t.Fatal(err)
	}
	if err := k.Set("secret", []byte("value")); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked write: %v", err)
	}
	if err := k.Apply([]Change{{Key: "a", Delete: true}, {Key: "b", Value: []byte("value")}}); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked mixed batch: %v", err)
	}
	if _, err := k.Seal("test", []byte("value"), nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked seal: %v", err)
	}
	if _, err := k.Unseal("test", nil, nil); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked unseal: %v", err)
	}
	if _, err := os.Stat(o.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("encrypted file created: %v", err)
	}
	if _, err := os.Stat(masterPath(o)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("master file created: %v", err)
	}
	if second, err := OpenLocked(o); second != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("locked store lost lifetime lock: %v", err)
	}
}

func TestExplicitUnlockSaveAndReopen(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	k := openLockedTest(t, o)
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	if !k.Unlocked() {
		t.Fatal("unlock left the store locked")
	}
	sealed, err := k.Seal("test", []byte("value"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	if k.Unlocked() || !errors.Is(k.UnlockPassword([]byte(testPassword)), ErrLocked) {
		t.Fatal("closed store unlocked")
	}
	k = openLockedTest(t, o)
	for _, operation := range []func() error{
		func() error { _, err := k.Get("secret"); return err },
		func() error { return k.Set("secret", []byte("new")) },
		func() error { return k.Delete("secret") },
		func() error { return k.Apply(nil) },
	} {
		if err := operation(); !errors.Is(err, ErrLocked) {
			t.Fatalf("existing locked store operation: %v", err)
		}
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
	if plain, err := k.Unseal("test", sealed, nil); err != nil || string(plain) != "value" {
		t.Fatalf("reopened unseal: %v", err)
	}
}

func TestLockedUnlockFailurePreservesCiphertextAndRetries(t *testing.T) {
	for _, failure := range []string{"wrong", "corrupt", "empty"} {
		t.Run(failure, func(t *testing.T) {
			o := options(t)
			k := openTest(t, o)
			if err := k.Set("secret", []byte("value")); err != nil {
				t.Fatal(err)
			}
			k.Close()
			original, err := os.ReadFile(o.Path)
			if err != nil {
				t.Fatal(err)
			}
			master, err := os.ReadFile(masterPath(o))
			if err != nil {
				t.Fatal(err)
			}
			want := error(ErrWrongPassword)
			switch failure {
			case "corrupt", "empty":
				want = ErrCorrupt
				broken := append([]byte(nil), original...)
				if failure == "empty" {
					broken = nil
				} else {
					broken[len(broken)-1] ^= 1
				}
				if err := os.WriteFile(o.Path, broken, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(o.Path)
			if err != nil {
				t.Fatal(err)
			}
			k = openLockedTest(t, o)
			password := testPassword
			if failure == "wrong" {
				password = "not the password"
			}
			if err := k.UnlockPassword([]byte(password)); !errors.Is(err, want) {
				t.Fatalf("unlock failure: %v", err)
			}
			if k.Unlocked() || len(k.b.(*encryptedStore).key) != 0 {
				t.Fatal("failed unlock retained candidate key")
			}
			after, err := os.ReadFile(o.Path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed unlock changed ciphertext: %v", err)
			}
			if now, _ := os.ReadFile(masterPath(o)); !bytes.Equal(master, now) {
				t.Fatal("failed unlock changed master.enc")
			}
			if failure == "corrupt" || failure == "empty" {
				// Corruption is fixed externally before reopening, not overwritten.
				k.Close()
				if err := os.WriteFile(o.Path, original, 0o600); err != nil {
					t.Fatal(err)
				}
				k = openLockedTest(t, o)
			}
			if err := k.UnlockPassword([]byte(testPassword)); err != nil {
				t.Fatal(err)
			}
			mustGet(t, k, "secret", []byte("value"))
		})
	}
}

func TestConcurrentUnlockSharesOneDerivation(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	k := openLockedTest(t, o)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			k.Unlocked()
			if err := k.UnlockPassword([]byte(testPassword)); err != nil {
				t.Error(err)
			}
			if err := k.Set("secret", []byte("value")); err != nil {
				t.Error(err)
			}
			mustGet(t, k, "secret", []byte("value"))
		})
	}
	wg.Wait()
	if !k.Unlocked() {
		t.Fatal("concurrent unlock left the store locked")
	}
	mustGet(t, k, "secret", []byte("value"))
	// The store must still hold one secret, not one write per goroutine.
	values, err := k.b.(*encryptedStore).decrypt()
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(values)
	if len(values) != 2 || len(values["secret"]) == 0 || len(values[BackupKey]) == 0 {
		t.Fatalf("entries: %d", len(values))
	}
}

func TestInjectedBackendDoesNotRequireUnlock(t *testing.T) {
	k := New(NewMemory())
	if !k.Unlocked() || k.Lock() != nil {
		t.Fatal("injected store requires unlock")
	}
}

// A password that is wrong leaves no trace, and the same store can be
// retried with the right one.
func TestWrongPasswordLeavesStoreUntouched(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	k := openLockedTest(t, o)
	if err := k.UnlockPassword([]byte("wrong")); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if k.Unlocked() {
		t.Fatal("wrong password unlocked")
	}
	if err := k.Set("secret", []byte("value")); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
}

// The master password is never stored: master.enc holds only ciphertext,
// and no derived key is kept outside it.
func TestPasswordIsNeverPersisted(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	for _, path := range []string{o.Path, masterPath(o)} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(testPassword)) {
			t.Fatalf("password in %s", path)
		}
		sum := sha256.Sum256([]byte(testPassword))
		if bytes.Contains(b, sum[:]) {
			t.Fatalf("derived password material in %s", path)
		}
	}
}
