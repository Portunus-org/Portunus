// Package secrets encrypts key material stored in the database (REALITY and
// TLS private keys) with AES-256-GCM under a master key kept outside the
// database, so a leaked database file alone does not reveal them.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const keyLen = 32

// prefix versions the ciphertext format.
const prefix = "v1:"

type Box struct {
	mu   sync.RWMutex
	aead cipher.AEAD
	key  []byte
}

// LoadOrCreate reads the master key from path, creating it (0600) if it
// does not exist. The file holds the key base64-encoded.
func LoadOrCreate(path string) (*Box, bool, error) {
	raw, err := os.ReadFile(path)
	created := false
	switch {
	case errors.Is(err, os.ErrNotExist):
		key := make([]byte, keyLen)
		rand.Read(key)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, false, err
		}
		enc := base64.StdEncoding.EncodeToString(key) + "\n"
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, false, fmt.Errorf("create master key: %w", err)
		}
		if _, err := f.WriteString(enc); err != nil {
			f.Close()
			return nil, false, err
		}
		if err := f.Close(); err != nil {
			return nil, false, err
		}
		raw, created = []byte(enc), true
	case err != nil:
		return nil, false, fmt.Errorf("read master key: %w", err)
	}

	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != keyLen {
		return nil, false, fmt.Errorf("master key %s: want %d base64-encoded bytes", path, keyLen)
	}
	box, err := New(key)
	return box, created, err
}

func New(key []byte) (*Box, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead, key: key}, nil
}

// Replace swaps the master key in place so existing Box references use other's key.
func (b *Box) Replace(other *Box) {
	other.mu.RLock()
	key := append([]byte(nil), other.key...)
	aead := other.aead
	other.mu.RUnlock()
	b.mu.Lock()
	clear(b.key)
	b.key = key
	b.aead = aead
	b.mu.Unlock()
}

// Save writes the base64-encoded master key to path atomically (0600).
func (b *Box) Save(path string) error {
	b.mu.RLock()
	enc := base64.StdEncoding.EncodeToString(b.key) + "\n"
	b.mu.RUnlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".master.key-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.WriteString(enc); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Derive returns a 32-byte key for purpose, derived from the master key with
// HKDF-SHA256. It is stable for as long as the master key is.
func (b *Box) Derive(purpose string) []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()
	k, err := hkdf.Key(sha256.New, b.key, nil, "portunus:"+purpose, keyLen)
	if err != nil {
		panic(err) // only fails for invalid lengths
	}
	return k
}

// Seal encrypts plaintext; aad binds the ciphertext to its context (for
// example "inbound:12:reality") so it cannot be moved to another row.
func (b *Box) Seal(plaintext, aad string) string {
	b.mu.RLock()
	aead := b.aead
	b.mu.RUnlock()
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	out := aead.Seal(nonce, nonce, []byte(plaintext), []byte(aad))
	return prefix + base64.RawStdEncoding.EncodeToString(out)
}

func (b *Box) Open(ciphertext, aad string) (string, error) {
	b.mu.RLock()
	aead := b.aead
	b.mu.RUnlock()
	enc, ok := strings.CutPrefix(ciphertext, prefix)
	if !ok {
		return "", errors.New("unknown ciphertext format")
	}
	raw, err := base64.RawStdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	ns := aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	pt, err := aead.Open(nil, raw[:ns], raw[ns:], []byte(aad))
	if err != nil {
		return "", errors.New("decrypt: wrong master key or corrupted data")
	}
	return string(pt), nil
}
