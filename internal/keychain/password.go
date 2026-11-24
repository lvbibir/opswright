package keychain

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"

	"github.com/Licoy/sshyard/internal/vault"
	"golang.org/x/crypto/chacha20poly1305"
)

// The master password is the one secret SSHYard asks for: the random
// master key is sealed under it (vault, Argon2id) in master.enc beside
// secrets.enc. The password itself is never stored. The same key is
// cached in the computer's own keychain, so this computer opens the store
// without asking; another computer, which has master.enc but not the
// cache, asks for the password once. Locking drops the in-memory key.
const masterFile = "master.enc"

// store is the encrypted store; the caller holds k.mu.
func (k *Keychain) store() (*encryptedStore, error) {
	if k.closed {
		return nil, &OpError{Op: "closed", Err: ErrLocked}
	}
	b, ok := k.b.(*encryptedStore)
	if !ok {
		return nil, &OpError{Op: "master-password", Err: ErrNoPassword}
	}
	return b, nil
}

// PasswordProtected reports whether a master password protects the store,
// that is whether master.enc exists.
func (k *Keychain) PasswordProtected() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	b, err := k.store()
	return err == nil && b.protected
}

// CanUsePassword reports whether SetupPassword may set the store's master
// password: the store is empty, so setting one leaves nothing behind.
func (k *Keychain) CanUsePassword() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	b, err := k.store()
	return err == nil && !b.protected && !b.exists && len(b.key) == 0
}

// SetupPassword creates the master key of an empty, locked store under the
// master password, unlocks the store with it, and caches that key in this
// computer's keychain.
func (k *Keychain) SetupPassword(password []byte) error { return k.setup(password) }

// setup seals a new random master key under password into master.enc and
// unlocks the store with it. A store that already has master.enc, or
// whose secrets.enc exists, refuses: a new key would leave those secrets
// unopenable for good.
func (k *Keychain) setup(password []byte) error {
	k.unlockMu.Lock()
	defer k.unlockMu.Unlock()
	k.mu.Lock()
	b, err := k.store()
	if err == nil && (b.protected || b.exists || len(b.key) != 0) {
		err = &OpError{Op: "master-password-write", Err: ErrPasswordSetup}
	}
	if err != nil {
		k.mu.Unlock()
		return err
	}
	path, kdf := b.masterPath, b.kdf
	k.mu.Unlock()
	key := make([]byte, chacha20poly1305.KeySize)
	defer clear(key)
	if _, err := rand.Read(key); err != nil {
		return &OpError{Op: "master-password-write", Err: err}
	}
	if err := sealMaster(path, key, password, kdf); err != nil {
		return err
	}
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return errors.Join(&OpError{Op: "closed", Err: ErrLocked}, removeMaster(path))
	}
	if err := b.unlock(key); err != nil {
		k.mu.Unlock()
		return errors.Join(err, removeMaster(path))
	}
	b.protected = true
	system := b.system
	k.mu.Unlock()
	if err := k.ensureBackupKey(password); err != nil {
		return err
	}
	cacheMaster(system, key)
	return nil
}

// cacheMaster keeps key in the computer's own keychain so that it opens
// the store without asking. A keychain that cannot be written is not an
// error: this computer then asks for the password like any other.
func cacheMaster(system Backend, key []byte) {
	if system == nil {
		return
	}
	_ = system.Set(MasterKey, key)
}

// TryUnlock opens the store with the master key this computer cached in
// its own keychain, which needs no master password. A keychain without
// one, or that cannot be read, leaves the store locked for an explicit
// unlock; neither is an error the caller shows, since a new computer
// always starts that way.
func (k *Keychain) TryUnlock() error {
	k.unlockMu.Lock()
	defer k.unlockMu.Unlock()
	k.mu.Lock()
	b, err := k.store()
	if err != nil {
		k.mu.Unlock()
		return err
	}
	if len(b.key) != 0 {
		k.mu.Unlock()
		return nil
	}
	system := b.system
	k.mu.Unlock()
	if system == nil {
		return &OpError{Op: "master-cache", Err: ErrUnavailable}
	}
	key, err := system.Get(MasterKey)
	if errors.Is(err, ErrNotFound) {
		return &OpError{Op: "master-cache", Err: ErrNotFound}
	}
	if err != nil {
		return &OpError{Op: "master-cache", Err: err}
	}
	defer clear(key)
	if len(key) != chacha20poly1305.KeySize {
		return &OpError{Op: "master-cache", Err: ErrMasterInvalid}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return &OpError{Op: "closed", Err: ErrLocked}
	}
	return b.unlock(key)
}

// ForgetSystem drops the cached master key of this computer, so that the
// next launch asks for the master password. The store keeps working while
// it stays open.
func (k *Keychain) ForgetSystem() error {
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return &OpError{Op: "closed", Err: ErrLocked}
	}
	b, ok := k.b.(*encryptedStore)
	if !ok {
		k.mu.Unlock()
		return &OpError{Op: "master-cache", Err: ErrUnavailable}
	}
	system := b.system
	k.mu.Unlock()
	if system == nil {
		return nil
	}
	return system.Delete(MasterKey)
}

// UnlockPassword opens the master key with the master password and unlocks
// the store with it.
func (k *Keychain) UnlockPassword(password []byte) error {
	k.unlockMu.Lock()
	defer k.unlockMu.Unlock()
	k.mu.Lock()
	b, err := k.store()
	switch {
	case err != nil:
	case !b.protected && b.exists:
		// secrets.enc without master.enc: only the OS keychain of a former
		// install could open it, and that key is gone.
		err = &OpError{Op: "master-password", Err: ErrMasterMissing}
	case !b.protected:
		err = &OpError{Op: "master-password", Err: ErrNoPassword}
	}
	if err != nil || len(b.key) != 0 {
		k.mu.Unlock()
		return err
	}
	path := b.masterPath
	k.mu.Unlock()
	key, err := openMaster(path, password)
	if err != nil {
		return err
	}
	defer clear(key)
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return &OpError{Op: "closed", Err: ErrLocked}
	}
	if err := b.unlock(key); err != nil {
		k.mu.Unlock()
		return err
	}
	system := b.system
	k.mu.Unlock()
	// A store from before the backup key existed (or one whose write
	// failed) gets it now, the one moment the password is at hand.
	if err := k.ensureBackupKey(password); err != nil {
		return err
	}
	// This computer remembers the password from here on.
	cacheMaster(system, key)
	return nil
}

// Lock forgets the in-memory master key, so every secret waits for the
// master password again; the ciphertext and the lifetime lock stay.
func (k *Keychain) Lock() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return &OpError{Op: "closed", Err: ErrLocked}
	}
	if b, ok := k.b.(*encryptedStore); ok {
		b.lockKey()
	}
	return nil
}

// unlockedMaster is the master key of an unlocked store, once current
// proves to be the master password.
func (k *Keychain) unlockedMaster(current []byte) (*encryptedStore, []byte, error) {
	k.mu.Lock()
	b, err := k.store()
	switch {
	case err != nil:
	case !b.protected:
		err = &OpError{Op: "master-password", Err: ErrNoPassword}
	case len(b.key) == 0:
		err = &OpError{Op: "master-password", Err: ErrLocked}
	}
	if err != nil {
		k.mu.Unlock()
		return nil, nil, err
	}
	key, path := append([]byte(nil), b.key...), b.masterPath
	k.mu.Unlock()
	opened, err := openMaster(path, current)
	same := err == nil && bytes.Equal(opened, key)
	clear(opened)
	if err == nil && !same {
		err = &OpError{Op: "master-password", Err: ErrMasterInvalid}
	}
	if err != nil {
		clear(key)
		return nil, nil, err
	}
	return b, key, nil
}

// ChangePassword seals the master key under next in place of current.
func (k *Keychain) ChangePassword(current, next []byte) error {
	k.unlockMu.Lock()
	defer k.unlockMu.Unlock()
	b, key, err := k.unlockedMaster(current)
	if err != nil {
		return err
	}
	defer clear(key)
	if err := sealMaster(b.masterPath, key, next, b.kdf); err != nil {
		return err
	}
	cacheMaster(b.system, key)
	// Backups seal under the new password from now on; files sealed with
	// the old one still open with it.
	return k.replaceBackupKey(next)
}

// replaceBackupKey keeps the key next derives to in place of the one kept.
func (k *Keychain) replaceBackupKey(next []byte) error {
	k.mu.Lock()
	b, err := k.store()
	if err != nil {
		k.mu.Unlock()
		return err
	}
	kdf := b.kdf
	k.mu.Unlock()
	key, err := vault.Derive(next, kdf)
	if err != nil {
		return &OpError{Op: "master-password-write", Err: err}
	}
	defer key.Clear()
	raw, err := json.Marshal(key)
	if err != nil {
		return &OpError{Op: "master-password-write", Err: err}
	}
	defer clear(raw)
	return k.Set(BackupKey, raw)
}

func sealMaster(path string, key, password []byte, kdf vault.Params) error {
	sealed, err := vault.Seal(key, password, kdf)
	if err != nil {
		return &OpError{Op: "master-password-write", Err: err}
	}
	if err := writeEncrypted(path, sealed); err != nil {
		return &OpError{Op: "master-password-write", Err: err}
	}
	return nil
}

func openMaster(path string, password []byte) ([]byte, error) {
	sealed, err := readEncrypted(path)
	if err != nil {
		return nil, &OpError{Op: "master-password", Err: err}
	}
	key, err := vault.Open(sealed, password)
	switch {
	case errors.Is(err, vault.ErrPassword):
		return nil, &OpError{Op: "master-password", Err: ErrWrongPassword}
	case err != nil || len(key) != chacha20poly1305.KeySize:
		clear(key)
		return nil, &OpError{Op: "master-password", Err: ErrMasterInvalid}
	}
	return key, nil
}

// BackupKey is the vault key exports and backup targets seal with: derived
// from the master password when it was set or changed, kept in the store,
// so a file it seals opens with that password on any device.
func (k *Keychain) BackupKey() (vault.Key, error) {
	b, err := k.Get(BackupKey)
	if err != nil {
		return vault.Key{}, err
	}
	defer clear(b)
	var key vault.Key
	if json.Unmarshal(b, &key) != nil || !key.Valid() {
		key.Clear()
		return vault.Key{}, &OpError{Op: "file-decrypt", Err: ErrCorrupt}
	}
	return key, nil
}

// ensureBackupKey keeps the backup key the master password derives to,
// once there is none: the one moment the password is at hand.
func (k *Keychain) ensureBackupKey(password []byte) error {
	k.mu.Lock()
	b, err := k.store()
	if err != nil {
		k.mu.Unlock()
		return err
	}
	kdf := b.kdf
	k.mu.Unlock()
	if ok, err := k.Exists(BackupKey); err != nil || ok {
		return err
	}
	key, err := vault.Derive(password, kdf)
	if err != nil {
		return &OpError{Op: "master-password-write", Err: err}
	}
	defer key.Clear()
	raw, err := json.Marshal(key)
	if err != nil {
		return &OpError{Op: "master-password-write", Err: err}
	}
	defer clear(raw)
	return k.Set(BackupKey, raw)
}

func removeMaster(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
