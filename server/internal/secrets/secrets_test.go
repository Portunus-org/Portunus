package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTripAndBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	box, created, err := LoadOrCreate(path)
	if err != nil || !created {
		t.Fatalf("create: %v %v", created, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", st.Mode().Perm())
	}
	ct := box.Seal("private-key", "inbound:1")
	if got, err := box.Open(ct, "inbound:1"); err != nil || got != "private-key" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := box.Open(ct, "inbound:2"); err == nil {
		t.Fatal("ciphertext opened under a different context")
	}

	again, created, err := LoadOrCreate(path)
	if err != nil || created {
		t.Fatalf("reload: %v %v", created, err)
	}
	if got, err := again.Open(ct, "inbound:1"); err != nil || got != "private-key" {
		t.Fatalf("open with reloaded key: %q %v", got, err)
	}
}

func TestDerive(t *testing.T) {
	a, _ := New(make([]byte, 32))
	b, _ := New(append(make([]byte, 31), 1))
	if string(a.Derive("x")) != string(a.Derive("x")) {
		t.Fatal("derive is not deterministic")
	}
	if string(a.Derive("x")) == string(a.Derive("y")) || string(a.Derive("x")) == string(b.Derive("x")) {
		t.Fatal("derived keys collide")
	}
	if len(a.Derive("x")) != 32 {
		t.Fatal("wrong length")
	}
}
