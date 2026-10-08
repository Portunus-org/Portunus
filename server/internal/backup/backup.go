// Package backup packs a whole panel into one archive and back: a snapshot
// of the database, the master key and what sing-box keeps. Sealed with the
// installation's recovery key, the archive can be left on other servers
// (see core: standby nodes) without trusting them with its contents.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Source is where a panel's data is.
type Source struct {
	DataDir string
	// DBPath is the database file; Snapshot copies it consistently to a
	// new file while it is in use.
	DBPath   string
	Snapshot func(ctx context.Context, to string) error
	// MasterKeyPath may lie outside DataDir.
	MasterKeyPath string
}

// Write writes the archive (.tar.gz) of src to w.
func Write(ctx context.Context, w io.Writer, src Source) error {
	snap, err := os.CreateTemp(src.DataDir, "backup-*.db")
	if err != nil {
		return err
	}
	snap.Close()
	os.Remove(snap.Name()) // SQLite wants to create the file itself
	defer os.Remove(snap.Name())
	if err := src.Snapshot(ctx, snap.Name()); err != nil {
		return fmt.Errorf("snapshot the database: %w", err)
	}

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	add := func(path, name string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
			return err
		}
		_, err = io.Copy(tw, io.LimitReader(f, info.Size()))
		return err
	}
	dbName := filepath.Base(src.DBPath)
	if err := add(snap.Name(), dbName); err != nil {
		return err
	}
	err = filepath.WalkDir(src.DataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src.DataDir, path)
		switch {
		case rel == ".":
			return nil
		case d.IsDir() && rel == "cache": // map tiles: fetched again when needed
			return filepath.SkipDir
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{Name: rel + "/", Typeflag: tar.TypeDir, Mode: 0o700})
		case !d.Type().IsRegular(), strings.HasPrefix(rel, dbName), strings.HasPrefix(rel, "backup-"):
			return nil
		}
		return add(path, rel)
	})
	if err != nil {
		return err
	}
	// A key kept outside the data directory would otherwise be missing.
	if key := src.MasterKeyPath; !strings.HasPrefix(key, src.DataDir+string(filepath.Separator)) {
		if err := add(key, "master.key"); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// maxFile bounds one unpacked file: a guard against a corrupt archive.
const maxFile = 1 << 30

// Unpack restores an archive made by Write into dir.
func Unpack(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return errors.New("not a Portunus backup")
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if !filepath.IsLocal(h.Name) {
			return fmt.Errorf("unexpected path in the backup: %q", h.Name)
		}
		path := filepath.Join(dir, h.Name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, io.LimitReader(tr, maxFile))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}

// ---------------------------------------------------------------- sealing

// magic starts a sealed backup.
const magic = "PORTUNUS-BACKUP-1\n"

// NewRecoveryKey makes up a recovery key: 160 random bits, written so that
// a person can copy it (XXXX-XXXX-…, letters and digits without lookalikes
// of each other's case).
func NewRecoveryKey() string {
	b := make([]byte, 20)
	rand.Read(b)
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	var parts []string
	for i := 0; i < len(s); i += 4 {
		parts = append(parts, s[i:i+4])
	}
	return strings.Join(parts, "-")
}

// cipherFor derives the cipher from a recovery key as a person may type it:
// case, spaces and dashes do not matter. The key is random, so one hash is
// enough; there is no password to stretch.
func cipherFor(key string) (cipher.AEAD, error) {
	key = strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\t", "", "\r", "", "\n", "").Replace(key))
	if len(key) < 16 {
		return nil, errors.New("the recovery key is too short")
	}
	sum := sha256.Sum256([]byte("portunus recovery key\x00" + key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts an archive with the recovery key.
func Seal(archive []byte, key string) ([]byte, error) {
	aead, err := cipherFor(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(magic), nonce...)
	return aead.Seal(out, nonce, archive, []byte(magic)), nil
}

// Sealed reports whether data is a sealed backup.
func Sealed(data []byte) bool { return bytes.HasPrefix(data, []byte(magic)) }

// ErrWrongKey: the recovery key does not open the backup.
var ErrWrongKey = errors.New("the recovery key does not fit this backup")

// Open decrypts a sealed backup.
func Open(data []byte, key string) ([]byte, error) {
	aead, err := cipherFor(key)
	if err != nil {
		return nil, err
	}
	body := data[len(magic):]
	if !Sealed(data) || len(body) < aead.NonceSize() {
		return nil, errors.New("not a sealed Portunus backup")
	}
	plain, err := aead.Open(nil, body[:aead.NonceSize()], body[aead.NonceSize():], []byte(magic))
	if err != nil {
		return nil, ErrWrongKey
	}
	return plain, nil
}
