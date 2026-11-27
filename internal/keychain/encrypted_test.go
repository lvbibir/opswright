package keychain

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestOpenFileFailureAndRetry(t *testing.T) {
	o := options(t)
	// A directory where the store file belongs: the store exists but cannot
	// be read, on every platform. A path under a plain file would not do,
	// because Windows reports it as not found, which OpenLocked takes for a
	// new store rather than a read failure.
	if err := os.MkdirAll(o.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	k, err := OpenLocked(o)
	var oe *OpError
	if k != nil || !errors.As(err, &oe) {
		t.Fatalf("failed stage: %v", err)
	}
	if oe.Op != "file-read" {
		t.Fatalf("failed stage: %v", err)
	}
	if err := os.Remove(o.Path); err != nil {
		t.Fatal(err)
	}
	openTest(t, o)
}

func TestWriteFailurePreservesOldValuesAndBatch(t *testing.T) {
	o := options(t)
	k := openTest(t, o)
	if err := k.Apply([]Change{{Key: "one", Value: []byte("old1")}, {Key: "two", Value: []byte("old2")}}); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(o.Path)
	// Force atomic replacement to fail, after a temporary ciphertext is synced.
	if err := os.Remove(o.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(o.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := k.Apply([]Change{{Key: "one", Value: []byte("new1")}, {Key: "two", Delete: true}}); err == nil {
		t.Fatal("write failure masked")
	}
	mustGet(t, k, "one", []byte("old1"))
	mustGet(t, k, "two", []byte("old2"))
	entries, err := os.ReadDir(filepath.Dir(o.Path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatal("temporary files leaked")
	}
	if err := os.Remove(o.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.Path, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := k.Apply([]Change{{Key: "one", Value: []byte("new1")}, {Key: "two", Delete: true}}); err != nil {
		t.Fatal(err)
	}
	k.Close()
	k = unlockTest(t, o, testPassword)
	mustGet(t, k, "one", []byte("new1"))
	mustGetMissing(t, k, "two")
	if runtime.GOOS != "windows" {
		stat, _ := os.Stat(o.Path)
		if stat.Mode().Perm() != 0o600 {
			t.Fatal("ciphertext permissions", stat.Mode())
		}
	}
}

func TestLifetimeLockAndConcurrentWrites(t *testing.T) {
	o := options(t)
	k := openTest(t, o)
	other := o
	other.Path = filepath.Join(t.TempDir(), "different.enc")
	if second, err := OpenLocked(other); second != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("shared master raced: %v", err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := k.Set(fmt.Sprintf("key%d", i), []byte{byte(i)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := range 20 {
		mustGet(t, k, fmt.Sprintf("key%d", i), []byte{byte(i)})
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockChild$")
	cmd.Env = append(os.Environ(), "OPSWRIGHT_TEST_LOCK="+o.LockPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cross-process lock: %v %s", err, out)
	}
	k.Close()
	lock, err := acquireLock(o.LockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if _, err := os.Stat(o.LockPath); err != nil {
		t.Fatal("lock file removed", err)
	}
}
func TestLockChild(t *testing.T) {
	path := os.Getenv("OPSWRIGHT_TEST_LOCK")
	if path == "" {
		t.Skip("subprocess helper")
	}
	lock, err := acquireLock(path)
	if lock != nil {
		lock.Close()
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("child acquired parent's lock: %v", err)
	}
}

// An embedded Memory must not promote a batch implementation that skips hooks.
type flakyBackend struct {
	*Memory
	failKey string
	fail    error
}

func (b *flakyBackend) Set(key string, value []byte) error {
	if key == b.failKey {
		return b.fail
	}
	return b.Memory.Set(key, value)
}
func TestInjectedBatchFailureRollsBack(t *testing.T) {
	b := &flakyBackend{Memory: NewMemory(), failKey: "two", fail: errors.New("injected")}
	b.Memory.Set("one", []byte("old"))
	k := New(b)
	err := k.Apply([]Change{{Key: "one", Value: []byte("new")}, {Key: "two", Value: []byte("new")}})
	if !errors.Is(err, b.fail) {
		t.Fatal("failure bypassed", err)
	}
	mustGet(t, k, "one", []byte("old"))
	mustGetMissing(t, k, "two")
}

func TestClosedStoreCannotBypassFacade(t *testing.T) {
	o := options(t)
	k := openTest(t, o)
	if err := k.Set("credential", []byte("value")); err != nil {
		t.Fatal(err)
	}
	b := k.b.(*encryptedStore)
	if err := k.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get("credential"); !errors.Is(err, ErrLocked) {
		t.Fatal("inner store bypassed close", err)
	}
	if err := k.Apply([]Change{{Key: "credential", Delete: true}}); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
}

// TestWipe: an unlocked store wipes to an empty one, still unlocked; a
// locked one loses its file, and opens empty with the same password after.
func TestWipe(t *testing.T) {
	o := options(t)
	k := openTest(t, o)
	if err := k.Apply([]Change{{Key: "a", Value: []byte("1")}, {Key: "b", Value: []byte("2")}}); err != nil {
		t.Fatal(err)
	}
	if err := k.Wipe(); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("a"); !errors.Is(err, ErrNotFound) || !k.Unlocked() {
		t.Fatalf("after wipe: %v, unlocked %v", err, k.Unlocked())
	}
	if err := k.Set("c", []byte("3")); err != nil {
		t.Fatal(err)
	}
	k.Close()
	master, err := os.ReadFile(masterPath(o))
	if err != nil {
		t.Fatal(err)
	}

	k = openLockedTest(t, o)
	if err := k.Wipe(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(o.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file left: %v", err)
	}
	if err := k.UnlockPassword([]byte(testPassword)); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get("c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("secret kept: %v", err)
	}
	if now, _ := os.ReadFile(masterPath(o)); !bytes.Equal(now, master) {
		t.Fatal("master key replaced")
	}
}
