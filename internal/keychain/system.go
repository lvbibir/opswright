package keychain

import (
	"errors"

	"github.com/egoist/mygo"
)

// System is the computer's own keychain, mygo.Secrets, under the app's
// identifier (com.sshyard.desktop), as a Backend. SSHYard keeps exactly
// one entry there, MasterKey: the same key master.enc seals under the
// master password, cached so that this computer opens the store without
// asking for it. Copying the data folder to another computer, or to
// another OS user, carries master.enc but not this entry, so that
// computer asks for the password once. A test may inject keychain.Memory
// in its place.
type System struct{}

func (System) Get(key string) ([]byte, error) {
	if key != MasterKey {
		return nil, ErrNotFound
	}
	v, err := mygo.Secrets.Get(MasterKey)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, ErrNotFound
	}
	return v, mapSystemErr(err)
}

func (System) Set(key string, value []byte) error {
	if key != MasterKey {
		return ErrUnavailable
	}
	return mapSystemErr(mygo.Secrets.Set(MasterKey, value))
}

func (System) Delete(key string) error {
	if key != MasterKey {
		return nil
	}
	if err := mygo.Secrets.Delete(MasterKey); err != nil && !errors.Is(err, mygo.ErrSecretNotFound) {
		return mapSystemErr(err)
	}
	return nil
}

// mapSystemErr names the keychain's own failures, without its text.
func mapSystemErr(err error) error {
	if err == nil {
		return nil
	}
	return &OpError{Op: "master-cache", Err: err}
}
