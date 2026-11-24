package keychain

import "errors"

var (
	ErrNotFound      = errors.New("keychain: secret not found")
	ErrUnavailable   = errors.New("keychain: no master key for this operation")
	ErrLocked        = errors.New("keychain: secret store locked")
	ErrBusy          = errors.New("keychain: secret store in use")
	ErrMasterMissing = errors.New("keychain: master key missing")
	ErrMasterInvalid = errors.New("keychain: invalid master key")
	ErrCorrupt       = errors.New("keychain: encrypted store damaged or key mismatched")
	ErrVersion       = errors.New("keychain: unsupported encrypted store version")
	// ErrWrongPassword is a master password that does not open the master
	// key.
	ErrWrongPassword = errors.New("keychain: wrong master password")
	// ErrNoPassword is a password operation on a store no master password
	// protects yet.
	ErrNoPassword = errors.New("keychain: no master password set")
	// ErrPasswordSetup is setting a master password for a store that
	// already has one, or has secrets a new one would orphan.
	ErrPasswordSetup = errors.New("keychain: master password needs an empty store")
)

// OpError identifies the failed stage without requiring UI to render raw errors.
// Op is lock, master-cache, master-password, master-password-write,
// file-read, file-decrypt, file-write, or closed.
type OpError struct {
	Op  string
	Err error
}

func (e *OpError) Error() string { return "keychain: " + e.Op + ": " + e.Err.Error() }
func (e *OpError) Unwrap() error { return e.Err }
