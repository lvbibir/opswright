package app

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
	"github.com/lvbibir/opswright/internal/keychain"
	"github.com/lvbibir/opswright/locales"
)

// The store is opened off the UI thread; completed work returns through post.
type unlockState struct {
	mu           sync.Mutex
	closed       bool
	ready        atomic.Bool
	secretsReady atomic.Bool
	busy         bool
	completed    bool
	err          error
	// servicesStarted is set once the work an unlock makes possible ran.
	servicesStarted bool
	started         bool
	// stored is whether secrets.enc exists, which tells whether saved
	// secrets are worth asking for at all.
	stored bool
}

type lockedSecrets struct{}

func (lockedSecrets) Get(string) ([]byte, error) { return nil, keychain.ErrLocked }
func (lockedSecrets) Set(string, []byte) error   { return keychain.ErrLocked }
func (lockedSecrets) Delete(string) error        { return keychain.ErrLocked }

func (a *App) ready() bool    { return a.unlock.ready.Load() }
func (a *App) unlocked() bool { return a.ready() && a.unlock.secretsReady.Load() }

// withSecrets runs an explicit user action that needs saved secrets. While
// the lock screen stands nothing runs: it, not a dialog, asks for the
// master password, so the action is dropped rather than queued.
func (a *App) withSecrets(after func()) {
	if a.unlocked() && after != nil {
		after()
	}
}

func (a *App) prepareStorage() error {
	if err := a.resolveStorage(); err != nil {
		return err
	}
	kc, err := a.openSecrets()
	if err != nil {
		return err
	}
	// A reset of the app lock left the data folder to empty, now that no
	// other copy writes to it.
	if done, err := finishReset(a.p.DataDir); err != nil {
		a.closeSecrets(kc)
		return &keychain.OpError{Op: "file-write", Err: err}
	} else if done {
		a.closeSecrets(kc)
		if kc, err = a.openSecrets(); err != nil {
			return err
		}
		a.toast(func(m locales.Messages) ui.Toast { return ui.Toast{Title: m.AppLockResetDone()} })
	}
	if err := a.loadData(); err != nil {
		a.closeSecrets(kc)
		return &keychain.OpError{Op: "file-read", Err: err}
	}
	a.loadAppLock()
	_, statErr := os.Stat(filepath.Join(a.p.DataDir, "secrets.enc"))
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		a.closeSecrets(kc)
		return &keychain.OpError{Op: "file-read", Err: statErr}
	}
	a.unlock.stored = statErr == nil
	a.kc = kc
	a.unlock.ready.Store(true)
	a.unlock.secretsReady.Store(kc.Unlocked())
	// The app is locked exactly while the store is: no master password,
	// nothing open.
	a.lock.locked = !kc.Unlocked()
	a.syncChoices()
	a.resolveFonts(false, false, false)
	a.updateCheck = a.settings.CheckUpdates
	// The updater keeps its own choice: follow the settings.
	a.upd.SetAutomatic(a.settings.CheckUpdates)
	a.routeUpdates()
	return nil
}

// retryPrepare stands behind the window that replaced the app when the
// data folder could not be read at all.
func (a *App) retryPrepare() {
	a.unlock.mu.Lock()
	if a.unlock.closed || a.unlock.busy || a.ready() {
		a.unlock.mu.Unlock()
		return
	}
	a.unlock.busy = true
	a.unlock.mu.Unlock()
	err := a.prepareStorage()
	if err == nil {
		a.applyWindowSettings()
		a.startIndependent()
	}
	a.unlock.mu.Lock()
	a.unlock.busy, a.unlock.err = false, err
	a.unlock.mu.Unlock()
	a.invalidate()
	if err == nil {
		a.probeUnlock()
	}
}

// afterUnlock starts what the master password makes possible.
func (a *App) afterUnlock() {
	a.startRestoredConnections()
	if !a.unlock.servicesStarted {
		a.unlock.servicesStarted = true
		a.autoStartForwards()
		a.syncMCP()
		a.startAutoBackup()
	}
}

func (a *App) openSecrets() (*keychain.Keychain, error) {
	if a.p.Secrets != nil {
		return keychain.New(a.p.Secrets), nil
	}
	system := a.p.System
	if system == nil {
		system = keychain.System{}
	}
	lock := a.p.SecretLockPath
	if lock == "" {
		lock = filepath.Join(a.p.DataDir, "secrets.lock")
	}
	scope := a.p.SecretScope
	if scope == "" {
		scope = "opswright"
	}
	return keychain.OpenLocked(keychain.OpenOptions{Path: filepath.Join(a.p.DataDir, "secrets.enc"), LockPath: lock, Scope: scope, System: system, KDF: a.kdf})
}

// StoragePaths keeps the canonical lifetime lock separate from overridden data.
type StoragePaths struct{ DataDir, LockPath, Scope string }

func (a *App) resolveStorage() error {
	if a.p.ResolveStorage != nil {
		paths, err := a.p.ResolveStorage()
		if err != nil {
			return &keychain.OpError{Op: "file-read", Err: err}
		}
		a.p.DataDir, a.p.SecretLockPath, a.p.SecretScope = paths.DataDir, paths.LockPath, paths.Scope
	} else if a.p.StartupError != nil {
		return &keychain.OpError{Op: "file-read", Err: a.p.StartupError}
	}
	if a.p.DataDir == "" {
		return &keychain.OpError{Op: "file-read", Err: errors.New("data directory unavailable")}
	}
	return nil
}

// closeSecrets releases the lifetime lock of a store being replaced or
// ended; a failure is logged, since nothing can be done about it then.
func (a *App) closeSecrets(kc *keychain.Keychain) {
	if err := kc.Close(); err != nil {
		log.Printf("opswright: credential store cleanup failed (stage=close)")
	}
}
