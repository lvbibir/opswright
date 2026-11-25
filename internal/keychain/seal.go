package keychain

import (
	"bytes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"

	"golang.org/x/crypto/chacha20poly1305"
)

// Data kept outside secrets.enc that must not reach the disk in plain
// text (as the AI conversations) is sealed under a key derived from the
// master key for its purpose. The master key never leaves the package.

const sealMagic = "OPSWRIGHT-SEALED\x00"
const sealVersion byte = 1

// sealer is a backend that seals data under its master key.
type sealer interface {
	seal(purpose string, plain, aad []byte) ([]byte, error)
	unseal(purpose string, sealed, aad []byte) ([]byte, error)
}

// sealer is the store's sealing side; every backend has one.
func (k *Keychain) sealer() (sealer, error) {
	if k.closed {
		return nil, &OpError{Op: "closed", Err: ErrLocked}
	}
	s, ok := k.b.(sealer)
	if !ok {
		return nil, ErrUnavailable
	}
	return s, nil
}

// Seal encrypts plain for purpose, bound to aad, which Unseal must be
// given again; ErrLocked while the store is locked.
func (k *Keychain) Seal(purpose string, plain, aad []byte) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s, err := k.sealer()
	if err != nil {
		return nil, err
	}
	return s.seal(purpose, plain, aad)
}

// Unseal decrypts what Seal gave for purpose and aad; ErrCorrupt when it
// was not, or was changed since.
func (k *Keychain) Unseal(purpose string, sealed, aad []byte) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s, err := k.sealer()
	if err != nil {
		return nil, err
	}
	return s.unseal(purpose, sealed, aad)
}

// deriveSealKey is the cipher for purpose: XChaCha20-Poly1305 under a key
// HKDF derives from the master key.
func deriveSealKey(master []byte, purpose string) (cipher.AEAD, error) {
	if len(master) != chacha20poly1305.KeySize || purpose == "" {
		return nil, ErrLocked
	}
	key, err := hkdf.Key(sha256.New, master, nil, "opswright/seal/"+purpose, chacha20poly1305.KeySize)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	return chacha20poly1305.NewX(key)
}

// sealAAD binds sealed data to its format, the store's scope, its purpose
// and the caller's aad.
func sealAAD(scope, purpose string, aad []byte) []byte {
	out := append([]byte(sealMagic), sealVersion)
	out = append(out, scope...)
	out = append(out, 0)
	out = append(out, purpose...)
	out = append(out, 0)
	return append(out, aad...)
}

func sealWith(master, scope []byte, purpose string, plain, aad []byte) ([]byte, error) {
	aead, err := deriveSealKey(master, purpose)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(append([]byte(sealMagic), sealVersion), nonce...)
	return aead.Seal(out, nonce, plain, sealAAD(string(scope), purpose, aad)), nil
}

func unsealWith(master, scope []byte, purpose string, sealed, aad []byte) ([]byte, error) {
	aead, err := deriveSealKey(master, purpose)
	if err != nil {
		return nil, err
	}
	h := len(sealMagic) + 1
	if len(sealed) < h+chacha20poly1305.NonceSizeX+chacha20poly1305.Overhead || !bytes.HasPrefix(sealed, []byte(sealMagic)) {
		return nil, ErrCorrupt
	}
	if sealed[len(sealMagic)] != sealVersion {
		return nil, ErrVersion
	}
	plain, err := aead.Open(nil, sealed[h:h+chacha20poly1305.NonceSizeX], sealed[h+chacha20poly1305.NonceSizeX:], sealAAD(string(scope), purpose, aad))
	if err != nil {
		return nil, ErrCorrupt
	}
	return plain, nil
}

// The encrypted store seals under its master key, bound to its scope.
func (b *encryptedStore) seal(purpose string, plain, aad []byte) ([]byte, error) {
	if len(b.key) != chacha20poly1305.KeySize {
		return nil, ErrLocked
	}
	return sealWith(b.key, b.scope(), purpose, plain, aad)
}

func (b *encryptedStore) unseal(purpose string, sealed, aad []byte) ([]byte, error) {
	if len(b.key) != chacha20poly1305.KeySize {
		return nil, ErrLocked
	}
	return unsealWith(b.key, b.scope(), purpose, sealed, aad)
}

// scope is what the sealed data of this store is bound to.
func (b *encryptedStore) scope() []byte { return b.aad }

// Memory seals under a master key of its own, as an unlocked store does,
// so that an injected test store can keep what production encrypts. It
// writes nothing to disk and never needs a password.
func (m *Memory) seal(purpose string, plain, aad []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return nil, m.Fail
	}
	return sealWith(m.key, []byte(memoryScope), purpose, plain, aad)
}

func (m *Memory) unseal(purpose string, sealed, aad []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return nil, m.Fail
	}
	return unsealWith(m.key, []byte(memoryScope), purpose, sealed, aad)
}
