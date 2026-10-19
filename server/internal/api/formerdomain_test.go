package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// panelCertFile writes a certificate for domain the way the panel's cache
// keeps it: the key, then the chain.
func panelCertFile(t *testing.T, path, domain string, notAfter time.Time) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: domain}, DNSNames: []string{domain},
		NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	k, _ := x509.MarshalECPrivateKey(key)
	out := append(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: k}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// After a move, the old domain still answers with its kept certificate, and
// sends a browser that had come through the gate on to the new address; a
// page still open there is told by the API. Strangers see the cover site,
// and plain HTTP goes over to HTTPS for the panel's own names only.
func TestFormerDomainSendsOn(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if err := e.core.SetDomain(ctx, "new.example.com"); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(e.api.cfg.DataDir, "panel-acme")
	panelCertFile(t, filepath.Join(cache, "old.example.com"), "old.example.com", time.Now().Add(60*24*time.Hour))
	panelCertFile(t, filepath.Join(cache, "stale.example.com"), "stale.example.com", time.Now().Add(-time.Hour))

	// The kept certificate, and no other.
	if c, err := e.core.FormerPanelCertificate(ctx, "old.example.com"); err != nil || c == nil {
		t.Fatalf("old domain: %v", err)
	}
	for _, d := range []string{"new.example.com", "stale.example.com", "none.example.com", "../panel-acme/old.example.com"} {
		if _, err := e.core.FormerPanelCertificate(ctx, d); err == nil {
			t.Fatalf("%s: a certificate was served", d)
		}
	}

	s := e.api
	s.panel.mu.Lock()
	s.panel.gate, s.panel.token = "lantern", e.core.PanelGateToken("lantern")
	s.panel.domain, s.panel.url = "new.example.com", "https://new.example.com/lantern"
	token := s.panel.token
	s.panel.mu.Unlock()
	panel := s.gated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("panel")) }))
	ask := func(sni, path string, opened bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://"+sni+path, nil)
		r.TLS = &tls.ConnectionState{ServerName: sni}
		r.RemoteAddr = "203.0.113.9:4000"
		if opened {
			r.AddCookie(&http.Cookie{Name: gateCookie, Value: token})
		}
		w := httptest.NewRecorder()
		panel.ServeHTTP(w, r)
		return w
	}
	if w := ask("old.example.com", "/lantern", false); w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "https://new.example.com/lantern" {
		t.Fatalf("secret path on the old domain: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := ask("old.example.com", "/clients/", true); w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "https://new.example.com/lantern" {
		t.Fatalf("page on the old domain: %d %q", w.Code, w.Header().Get("Location"))
	}
	w := ask("old.example.com", "/api/domain", true)
	var moved map[string]string
	json.NewDecoder(w.Body).Decode(&moved)
	if w.Code != http.StatusConflict || moved["error"] != "moved" || moved["url"] != "https://new.example.com/lantern" {
		t.Fatalf("API on the old domain: %d %v", w.Code, moved)
	}
	if w := ask("old.example.com", "/", false); w.Code == http.StatusTemporaryRedirect || w.Body.String() == "panel" {
		t.Fatalf("a stranger on the old domain was let through: %d", w.Code)
	}
	if w := ask("new.example.com", "/clients/", true); w.Body.String() != "panel" {
		t.Fatalf("the new domain: %d %q", w.Code, w.Body.String())
	}

	// Plain HTTP.
	toHTTPS := s.toHTTPS(443)
	for _, c := range []struct{ host, path, want string }{
		{"new.example.com", "/lantern?x=1", "https://new.example.com/lantern?x=1"},
		{"old.example.com:80", "/", "https://old.example.com/"},
		{"evil.example.org", "/", ""},
		{"new.example.com", "/.well-known/acme-challenge/abc", ""},
	} {
		r := httptest.NewRequest("GET", "http://"+c.host+c.path, nil)
		w := httptest.NewRecorder()
		toHTTPS.ServeHTTP(w, r)
		if got := w.Header().Get("Location"); got != c.want || (c.want == "" && w.Code != http.StatusNotFound) {
			t.Fatalf("http %s%s: %d %q, want %q", c.host, c.path, w.Code, got, c.want)
		}
	}
}
