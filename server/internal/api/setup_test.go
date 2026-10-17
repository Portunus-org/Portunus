package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWelcomeBasics(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

	var state map[string]bool
	if e.getJSON(t, "/api/setup", &state); state["completed"] {
		t.Fatal("fresh install reports setup completed")
	}
	if resp, body := e.do(t, "POST", "/api/setup/basics", map[string]string{"publicHost": "203.0.113.10", "domain": "not a domain"}, h); resp.StatusCode != 400 || body["field"] != "domain" {
		t.Fatalf("bad domain accepted: %d %v", resp.StatusCode, body)
	}
	if resp, _ := e.do(t, "POST", "/api/setup/basics", map[string]string{"publicHost": "203.0.113.10", "domain": "vpn.example.com"}, h); resp.StatusCode != 204 {
		t.Fatalf("basics: %d", resp.StatusCode)
	}
	var settings map[string]any
	e.getJSON(t, "/api/settings", &settings)
	if settings["publicHost"] != "203.0.113.10" || settings["domain"] != "vpn.example.com" || settings["subscriptionPort"] == float64(0) {
		t.Fatalf("settings after basics: %v", settings)
	}
	if e.getJSON(t, "/api/setup", &state); !state["completed"] {
		t.Fatal("basics did not complete the welcome")
	}
}

func TestInboundSuggestionAndCA(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

	resp, sug := e.do(t, "POST", "/api/inbounds/suggest", map[string]any{"protocol": "hysteria2"}, h)
	if resp.StatusCode != 200 || sug["port"] == nil || sug["certificate"] == nil || sug["masquerade"] == "" {
		t.Fatalf("suggest: %d %v", resp.StatusCode, sug)
	}
	if plan := sug["certificate"].(map[string]any); plan["recommended"] != "self-signed" {
		t.Fatalf("without a domain the plan must be self-signed: %v", plan)
	}
	if resp, _ := e.do(t, "POST", "/api/inbounds/suggest", map[string]any{"protocol": "nope"}, h); resp.StatusCode != 400 {
		t.Fatalf("bad protocol: %d", resp.StatusCode)
	}

	// A private certificate is issued by the installation's CA.
	resp, in := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "trojan", "port": 8443, "serverName": "vpn.example.com", "strictTls": true}, h)
	if resp.StatusCode != 201 || in["strictTls"] != true || in["privateCertExpires"] == nil {
		t.Fatalf("create: %d %v", resp.StatusCode, in)
	}
	res, err := e.client.Get(e.srv.URL + "/api/ca.crt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	block, _ := pem.Decode(body)
	if res.StatusCode != 200 || block == nil || !strings.Contains(res.Header.Get("Content-Disposition"), "portunus-ca.crt") {
		t.Fatalf("ca.crt: %d %q", res.StatusCode, body)
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil || !ca.IsCA {
		t.Fatalf("not a CA certificate: %v", err)
	}
	res, _ = e.client.Get(e.srv.URL + "/api/ca.crt?format=der")
	der, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if _, err := x509.ParseCertificate(der); err != nil {
		t.Fatalf("der: %v", err)
	}
	resp, info := e.do(t, "GET", "/api/ca", nil, h)
	sum := sha256.Sum256(der)
	if resp.StatusCode != 200 || info["fingerprint"] != strings.ToUpper(hex.EncodeToString(sum[:])) {
		t.Fatalf("ca info: %d %v", resp.StatusCode, info)
	}
}

func selfSignedTLS(t *testing.T) tls.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestSniffListenerServesHTTPAndHTTPS(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cert := selfSignedTLS(t)
	srv := newSubHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			io.WriteString(w, "https")
			return
		}
		io.WriteString(w, "http")
	}))
	go srv.Serve(&sniffListener{Listener: ln, tls: &tls.Config{Certificates: []tls.Certificate{cert}}})
	defer srv.Close()

	get := func(c *http.Client, url string) string {
		resp, err := c.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	addr := ln.Addr().String()
	if got := get(http.DefaultClient, "http://"+addr+"/"); got != "http" {
		t.Fatalf("plain: %q", got)
	}
	insecure := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if got := get(insecure, "https://"+addr+"/"); got != "http" && got != "https" {
		t.Fatalf("tls: %q", got)
	}
}
