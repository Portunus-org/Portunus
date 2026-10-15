package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

// writeCert puts a certificate for domain at path, after its key when
// withKey (the way the panel's cache keeps them).
func writeCert(t *testing.T, path, domain string, notAfter time.Time, withKey bool) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		Issuer:       pkix.Name{CommonName: "R11"},
		DNSNames:     []string{domain},
		NotBefore:    notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	if withKey {
		k, _ := x509.MarshalECPrivateKey(key)
		out = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: k})
	}
	out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Moving to another domain takes along the address in the links and the
// connections that presented the old name with a Let's Encrypt
// certificate; a connection that presents something else stays as it is.
func TestMoveDomain(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	if err := c.SetDomain(ctx, "old.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPublicHost(ctx, "old.example.com"); err != nil {
		t.Fatal(err)
	}
	onDomain, err := c.CreateInbound(ctx, InboundInput{Protocol: "hysteria2", Port: 30443, Enabled: true, TLSMode: proxy.TLSACME, ServerName: "old.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.CreateInbound(ctx, InboundInput{Protocol: "hysteria2", Port: 30444, Enabled: true, TLSMode: proxy.TLSSelfSigned, ServerName: "old.example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if err := c.MoveDomain(ctx, "new.example.com"); err != nil {
		t.Fatal(err)
	}
	if d, _ := c.Domain(ctx); d != "new.example.com" {
		t.Fatalf("domain %q", d)
	}
	if h, _ := c.PublicHost(ctx); h != "new.example.com" {
		t.Fatalf("public host %q", h)
	}
	if in, _ := c.Inbound(ctx, onDomain.ID); in.Settings.ServerName != "new.example.com" {
		t.Fatalf("inbound on the domain presents %q", in.Settings.ServerName)
	}
	if in, _ := c.Inbound(ctx, other.ID); in.Settings.ServerName != "old.example.com" {
		t.Fatalf("self-signed inbound changed to %q", in.Settings.ServerName)
	}

	// An address of its own (an IP) is the operator's and stays.
	if err := c.SetPublicHost(ctx, "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	if err := c.MoveDomain(ctx, "third.example.com"); err != nil {
		t.Fatal(err)
	}
	if h, _ := c.PublicHost(ctx); h != "203.0.113.5" {
		t.Fatalf("public host %q", h)
	}
}

// A move is refused where it would leave the panel behind a name that
// cannot work.
func TestMoveDomainRefusals(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	if err := c.SetDomain(ctx, "old.example.com"); err != nil {
		t.Fatal(err)
	}
	for domain, code := range map[string]string{
		"":                "required",
		"old.example.com": "same_domain",
		"not a domain":    "invalid_hostname",
		"203.0.113.5":     "invalid_hostname",
		// Reserved for documentation: it never points to this server.
		"nowhere.invalid": "points_elsewhere",
	} {
		_, err := c.MoveDomainTo(ctx, domain)
		var ve *ValidationError
		if !errors.As(err, &ve) || ve.Code != code {
			t.Errorf("%q: %v, want %s", domain, err, code)
		}
	}
	if d, _ := c.Domain(ctx); d != "old.example.com" {
		t.Fatalf("domain changed to %q", d)
	}
}

// The certificates of both stores are listed by domain, and those of other
// domains can be removed; the server's own cannot, and a name never reaches
// outside the stores.
func TestKeptCertificates(t *testing.T) {
	c, now := newBindingCore(t)
	ctx := context.Background()
	dir := t.TempDir()
	c.acme.StorageDir = filepath.Join(dir, "acme")
	c.panelACMEDir = filepath.Join(dir, "panel-acme")
	until := now.Add(60 * 24 * time.Hour)
	writeCert(t, filepath.Join(c.acme.StorageDir, "certificates", "acme-v02", "cur.example.com", "cur.example.com.crt"), "cur.example.com", until, false)
	writeCert(t, filepath.Join(c.acme.StorageDir, "certificates", "acme-v02", "old.example.com", "old.example.com.crt"), "old.example.com", until, false)
	writeCert(t, filepath.Join(c.panelACMEDir, "old.example.com"), "old.example.com", until.Add(24*time.Hour), true)
	writeCert(t, filepath.Join(c.panelACMEDir, "gone.example.com+rsa"), "gone.example.com", until, true)
	// What else the panel's cache holds is no certificate of a domain.
	if err := os.WriteFile(filepath.Join(c.panelACMEDir, "acme_account+key"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.SetDomain(ctx, "cur.example.com"); err != nil {
		t.Fatal(err)
	}

	st, err := c.DomainState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Certificate == nil || st.Certificate.Domain != "cur.example.com" {
		t.Fatalf("certificate of the domain: %+v", st.Certificate)
	}
	if len(st.Others) != 2 || st.Others[0].Domain != "gone.example.com" || st.Others[1].Domain != "old.example.com" {
		t.Fatalf("others: %+v", st.Others)
	}
	// One entry for a domain kept in both stores: the one that lasts longest.
	if !st.Others[1].NotAfter.Equal(until.Add(24 * time.Hour)) {
		t.Fatalf("old.example.com until %v", st.Others[1].NotAfter)
	}

	for domain, code := range map[string]string{"cur.example.com": "certificate_in_use", "../acme": "invalid_hostname", "a/b.example.com": "invalid_hostname"} {
		var ve *ValidationError
		if err := c.RemoveCertificate(ctx, domain); !errors.As(err, &ve) || ve.Code != code {
			t.Errorf("remove %q: %v, want %s", domain, err, code)
		}
	}
	if err := c.RemoveCertificate(ctx, "nothing.example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("remove what is not there: %v", err)
	}
	if err := c.RemoveCertificate(ctx, "old.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveCertificate(ctx, "gone.example.com"); err != nil {
		t.Fatal(err)
	}
	st, _ = c.DomainState(ctx)
	if len(st.Others) != 0 || st.Certificate == nil {
		t.Fatalf("after removing: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(c.panelACMEDir, "acme_account+key")); err != nil {
		t.Fatal("the account key went too")
	}
}
