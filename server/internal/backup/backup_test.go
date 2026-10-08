package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	src := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(src, rel)
		os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("portunus.db", "live database, must not be copied as it is")
	write("portunus.db-wal", "journal")
	write("master.key", "key")
	write("sing-box/acme/cert.pem", "certificate")
	write("cache/tiles/1.png", "tile")
	var archive bytes.Buffer
	err := Write(context.Background(), &archive, Source{
		DataDir: src, DBPath: filepath.Join(src, "portunus.db"), MasterKeyPath: filepath.Join(src, "master.key"),
		Snapshot: func(_ context.Context, to string) error { return os.WriteFile(to, []byte("snapshot"), 0o600) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(src, "backup-*")); len(left) != 0 {
		t.Fatalf("temporary snapshot left behind: %v", left)
	}

	key := NewRecoveryKey()
	sealed, err := Seal(archive.Bytes(), key)
	if err != nil || !Sealed(sealed) || bytes.Contains(sealed, []byte("snapshot")) {
		t.Fatalf("seal: %v", err)
	}
	if _, err := Open(sealed, NewRecoveryKey()); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("opened with another key: %v", err)
	}
	// As a person types it.
	plain, err := Open(sealed, " "+bytes.NewBufferString(key).String()+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if lower, err := Open(sealed, string(bytes.ToLower([]byte(key)))); err != nil || !bytes.Equal(lower, plain) {
		t.Fatalf("lower case key: %v", err)
	}

	dst := t.TempDir()
	if err := Unpack(bytes.NewReader(plain), dst); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"portunus.db": "snapshot", "master.key": "key", "sing-box/acme/cert.pem": "certificate"} {
		if got, _ := os.ReadFile(filepath.Join(dst, rel)); string(got) != want {
			t.Fatalf("%s: %q", rel, got)
		}
	}
	for _, rel := range []string{"portunus.db-wal", "cache"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err == nil {
			t.Fatalf("%s should not be in a backup", rel)
		}
	}
	if err := Unpack(bytes.NewReader([]byte("junk")), dst); err == nil {
		t.Fatal("junk unpacked")
	}
}
