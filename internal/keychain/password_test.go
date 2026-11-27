package keychain

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
)

func setupPassword(t *testing.T, o OpenOptions, password string) {
	t.Helper()
	k := openLockedTest(t, o)
	if !k.CanUsePassword() {
		t.Fatal("empty store cannot use a password")
	}
	if err := k.SetupPassword([]byte(password)); err != nil {
		t.Fatal(err)
	}
	if !k.Unlocked() || !k.PasswordProtected() {
		t.Fatal("setup left the store locked or unprotected")
	}
	if err := k.Set("secret", []byte("value")); err != nil {
		t.Fatal(err)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordSetupAndUnlock(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	k := openLockedTest(t, o)
	if !k.PasswordProtected() || k.CanUsePassword() {
		t.Fatal("protection not detected")
	}
	if err := k.UnlockPassword([]byte("wrong")); !errors.Is(err, ErrWrongPassword) || k.Unlocked() {
		t.Fatal("wrong password", err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
}

func TestPasswordSetupNeedsEmptyStore(t *testing.T) {
	o := options(t)
	k := openTest(t, o)
	if err := k.Set("secret", []byte("value")); err != nil {
		t.Fatal(err)
	}
	k.Close()
	k = openLockedTest(t, o)
	if k.CanUsePassword() {
		t.Fatal("store with secrets offers a password")
	}
	if err := k.SetupPassword([]byte("password")); !errors.Is(err, ErrPasswordSetup) {
		t.Fatal(err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
}

// TestOrphanedStoreNeedsTheCachedKey: a secrets.enc whose master.enc is
// gone may only be opened by this computer's cached key; setting a new
// password over it would leave those secrets unopenable for good.
func TestOrphanedStoreNeedsTheCachedKey(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	if err := os.Remove(masterPath(o)); err != nil {
		t.Fatal(err)
	}
	k := openLockedTest(t, o)
	if err := k.UnlockPassword([]byte(testPassword)); !errors.Is(err, ErrMasterMissing) {
		t.Fatal(err)
	}
	if k.CanUsePassword() {
		t.Fatal("orphaned store offers a password")
	}
	if err := k.SetupPassword([]byte("new password")); !errors.Is(err, ErrPasswordSetup) {
		t.Fatal("orphaned store accepted a new password")
	}
	if err := k.TryUnlock(); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
}

func TestPasswordDamagedMaster(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	if err := os.WriteFile(masterPath(o), []byte("damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	k := openLockedTest(t, o)
	if err := k.UnlockPassword([]byte(testPassword)); !errors.Is(err, ErrMasterInvalid) || k.Unlocked() {
		t.Fatal(err)
	}
}

func TestPasswordChange(t *testing.T) {
	o := options(t)
	setupPassword(t, o, "old password")
	k := openLockedTest(t, o)
	if err := k.ChangePassword([]byte("old password"), []byte("new password")); err == nil {
		t.Fatal("changed while locked")
	}
	if err := k.UnlockPassword([]byte("old password")); err != nil {
		t.Fatal(err)
	}
	if err := k.ChangePassword([]byte("wrong"), []byte("new password")); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if err := k.ChangePassword([]byte("old password"), []byte("new password")); err != nil {
		t.Fatal(err)
	}
	k.Close()
	k = openLockedTest(t, o)
	if err := k.UnlockPassword([]byte("old password")); !errors.Is(err, ErrWrongPassword) {
		t.Fatal("old password still opens", err)
	}
	if err := k.UnlockPassword([]byte("new password")); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
}

// Locking drops the master key, so every secret needs the password again.
func TestLockDropsTheMasterKey(t *testing.T) {
	o := options(t)
	setupPassword(t, o, testPassword)
	k := unlockTest(t, o, testPassword)
	if err := k.Lock(); err != nil {
		t.Fatal(err)
	}
	if k.Unlocked() {
		t.Fatal("locked store still unlocked")
	}
	if _, err := k.Get("secret"); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if _, err := k.Seal("test", []byte("value"), nil); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if err := k.UnlockPassword([]byte("wrong")); !errors.Is(err, ErrWrongPassword) {
		t.Fatal(err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
}

// blockedSystem counts what the computer's keychain was asked, and can
// be made to fail, as a locked keyring does.
type blockedSystem struct {
	*Memory
	reads atomic.Int32
	fail  error
}

func (b *blockedSystem) Get(key string) ([]byte, error) {
	b.reads.Add(1)
	if b.fail != nil {
		return nil, b.fail
	}
	return b.Memory.Get(key)
}
func (b *blockedSystem) Set(key string, value []byte) error {
	if b.fail != nil {
		return b.fail
	}
	return b.Memory.Set(key, value)
}

// TestSystemCacheAndSilentUnlock: setting the master password caches the
// master key in this computer's keychain, which then opens the store
// without it; forgetting the cache makes the password necessary again.
func TestSystemCacheAndSilentUnlock(t *testing.T) {
	o := options(t)
	system := &blockedSystem{Memory: NewMemory()}
	o.System = system
	setupPassword(t, o, testPassword)
	key, err := system.Get(MasterKey)
	if err != nil || len(key) != 32 {
		t.Fatalf("cached key: %d bytes, %v", len(key), err)
	}
	clear(key)

	k := openLockedTest(t, o)
	if k.Unlocked() {
		t.Fatal("a reopened store started unlocked")
	}
	if err := k.TryUnlock(); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
	if system.reads.Load() == 0 {
		t.Fatal("the silent unlock read no keychain")
	}

	// Locking drops the in-memory key; the cache is untouched.
	if err := k.Lock(); err != nil {
		t.Fatal(err)
	}
	if err := k.TryUnlock(); err != nil {
		t.Fatal(err)
	}
}

// TestTryUnlockWithoutCachedKey: a computer that never had the password
// typed on it, or whose keychain cannot be read, stays locked without an
// error the caller shows.
func TestTryUnlockWithoutCachedKey(t *testing.T) {
	o := options(t)
	system := &blockedSystem{Memory: NewMemory()}
	o.System = system
	setupPassword(t, o, testPassword)
	if err := system.Memory.Delete(MasterKey); err != nil {
		t.Fatal(err)
	}
	k := openLockedTest(t, o)
	if err := k.TryUnlock(); !errors.Is(err, ErrNotFound) || k.Unlocked() {
		t.Fatalf("missing cache: %v", err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	// Opening it with the password caches the key here, so the next
	// launch needs nothing.
	if _, err := system.Memory.Get(MasterKey); err != nil {
		t.Fatalf("password unlock cached nothing: %v", err)
	}
	k.Close()

	system.fail = errors.New("keyring locked")
	k = openLockedTest(t, o)
	if err := k.TryUnlock(); err == nil || k.Unlocked() {
		t.Fatalf("unreadable keychain: %v", err)
	}
	system.fail = nil
}

// TestForgetSystem: forgetting drops only the cached key, so the store
// keeps working while it is open and asks again after a restart.
func TestForgetSystem(t *testing.T) {
	o := options(t)
	system := &blockedSystem{Memory: NewMemory()}
	o.System = system
	setupPassword(t, o, testPassword)
	k := unlockTest(t, o, testPassword)
	if err := k.ForgetSystem(); err != nil {
		t.Fatal(err)
	}
	if _, err := system.Memory.Get(MasterKey); !errors.Is(err, ErrNotFound) {
		t.Fatal("the cached key stayed")
	}
	mustGet(t, k, "secret", []byte("value"))
	k.Close()
	k = openLockedTest(t, o)
	if err := k.TryUnlock(); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k, "secret", []byte("value"))
}

// TestSealPasswordExistingStore: a store without master.enc, opened by the
// key this computer cached, takes its first password by sealing the key it
// holds, keeping its secrets and making the backup key.
func TestSealPasswordExistingStore(t *testing.T) {
	o := options(t)
	k := openLockedTest(t, o)
	if err := k.SetupPassword([]byte("first-pw")); err != nil {
		t.Fatal(err)
	}
	if err := k.Set("secret", []byte("value")); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete(BackupKey); err != nil {
		t.Fatal(err)
	}
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(masterPath(o)); err != nil {
		t.Fatal(err)
	}
	k2 := openLockedTest(t, o)
	if err := k2.TryUnlock(); err != nil {
		t.Fatal(err)
	}
	if !k2.CanSetPassword() || k2.CanUsePassword() {
		t.Fatalf("can set=%v can use=%v", k2.CanSetPassword(), k2.CanUsePassword())
	}
	mustGet(t, k2, "secret", []byte("value"))
	if err := k2.SealPassword([]byte("second-pw")); err != nil {
		t.Fatal(err)
	}
	if !k2.PasswordProtected() || !k2.Unlocked() {
		t.Fatal("the store took no password")
	}
	if _, err := k2.BackupKey(); err != nil {
		t.Fatalf("no backup key: %v", err)
	}
	if err := k2.Close(); err != nil {
		t.Fatal(err)
	}
	// The password opens it from here on; the secrets stay.
	k3 := openLockedTest(t, o)
	if err := k3.UnlockPassword([]byte("second-pw")); err != nil {
		t.Fatal(err)
	}
	mustGet(t, k3, "secret", []byte("value"))
}
