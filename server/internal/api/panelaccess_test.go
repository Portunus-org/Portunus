package api

import (
	"context"
	"crypto/tls"
	"github.com/PaulichP/Portunus/server/internal/core"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The panel by its domain must look like nothing in particular to anyone
// who has not been through the gate.
func TestPanelAccessGate(t *testing.T) {
	core.PanelInnerPort = freeTCPPort(t)
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	ctx := context.Background()
	const domain = "panel.example.com"
	// The cover site: some other site, here a local one that says what it
	// was asked and what it was told about the visitor.
	cover := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=1")
		w.Header().Set("Set-Cookie", "theirs=1")
		io.WriteString(w, "cover site; cookie="+r.Header.Get("Cookie")+"; xff="+r.Header.Get("X-Forwarded-For"))
	}))
	t.Cleanup(cover.Close)

	// Not without a domain.
	if resp, out := e.do(t, "PUT", "/api/panel-access", map[string]any{"enabled": true, "mode": "own", "port": 8443}, h); resp.StatusCode != 400 || out["field"] != "domain" {
		t.Fatalf("without a domain: %d %v", resp.StatusCode, out)
	}
	if err := e.core.SetDomain(ctx, domain); err != nil {
		t.Fatal(err)
	}
	// Let's Encrypt only on 443.
	if resp, out := e.do(t, "PUT", "/api/panel-access", map[string]any{"enabled": true, "mode": "acme", "port": 8443}, h); resp.StatusCode != 400 || out["code"] != "acme_needs_443" {
		t.Fatalf("acme off 443: %d %v", resp.StatusCode, out)
	}
	ca, err := e.core.CA(ctx)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, _, _, err := ca.Issue(domain)
	if err != nil {
		t.Fatal(err)
	}
	other, otherKey, _, _, _ := ca.Issue("other.example.com")
	port := freeTCPPort(t)
	enable := func(cert, key string) (int, map[string]any) {
		resp, out := e.do(t, "PUT", "/api/panel-access", map[string]any{"enabled": true, "mode": "own", "port": port, "certificate": cert, "key": key, "decoy": cover.URL}, h)
		return resp.StatusCode, out
	}
	if code, out := enable(other, otherKey); code != 400 || out["code"] != "certificate_other_domain" {
		t.Fatalf("certificate for another domain: %d %v", code, out)
	}
	if code, out := enable(certPEM, "not a key"); code != 400 || out["code"] != "invalid_certificate" {
		t.Fatalf("broken key: %d %v", code, out)
	}
	code, out := enable(certPEM, keyPEM)
	if code != 200 || out["enabled"] != true || out["error"] != nil {
		t.Fatalf("enable: %d %v", code, out)
	}
	entry := out["url"].(string)
	origin := "https://" + net.JoinHostPort(domain, strconv.Itoa(port))
	if !strings.HasPrefix(entry, origin+"/") || len(strings.TrimPrefix(entry, origin+"/")) < 4 || strings.ContainsAny(strings.TrimPrefix(entry, origin+"/"), "/-") {
		t.Fatalf("entry address %q", entry)
	}
	gate := strings.TrimPrefix(entry, origin)

	// A browser: the domain's name, whatever certificate, this machine.
	client := func(sni string) *http.Client {
		return &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{ServerName: sni, InsecureSkipVerify: true},
				DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
				},
			},
		}
	}
	stranger := client(domain)
	fetch := func(c *http.Client, method, path, cookie string) (int, string, http.Header) {
		t.Helper()
		req, _ := http.NewRequest(method, origin+path, nil)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), resp.Header
	}

	// Everything a probe might ask for is answered by the cover site, and
	// by nothing of the panel's: same body whatever is asked, none of the
	// panel's headers, nothing of the visitor passed along.
	const covered = "cover site; cookie=; xff="
	for _, probe := range [][2]string{
		{"GET", "/"}, {"GET", "/login/"}, {"GET", "/welcome/"}, {"GET", "/healthz"}, {"GET", "/build-id"},
		{"GET", "/api/auth/me"}, {"POST", "/api/auth/login"}, {"GET", "/api/setup/state"}, {"GET", "/sub/" + strings.Repeat("a", 43)},
		{"GET", "/icon.svg"}, {"GET", gate + "x"}, {"POST", gate}, {"GET", gate + "/"},
	} {
		code, body, hdr := fetch(stranger, probe[0], probe[1], "theirs=2; "+gateCookie+"=guess")
		if code != 200 || body != covered {
			t.Fatalf("%s %s gives the panel away: %d %q", probe[0], probe[1], code, body)
		}
		for _, name := range []string{"Set-Cookie", "Content-Security-Policy", "X-Frame-Options", "Strict-Transport-Security", "Permissions-Policy"} {
			if hdr.Get(name) != "" {
				t.Fatalf("%s %s: header %s shown to a stranger: %v", probe[0], probe[1], name, hdr)
			}
		}
	}
	// A made-up cookie is no better.
	if code, body, _ := fetch(stranger, "GET", "/login/", gateCookie+"="+strings.Repeat("0", 64)); code != 200 || body != covered {
		t.Fatalf("forged gate cookie: %d %q", code, body)
	}
	// No name, or another name: no handshake at all.
	for _, sni := range []string{"", "other.example.com"} {
		conn, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &tls.Config{ServerName: sni, InsecureSkipVerify: true})
		if err == nil {
			conn.Close()
			t.Fatalf("handshake with name %q succeeded", sni)
		}
	}

	// The gate marks the browser and sends it on.
	code, _, hdr := fetch(stranger, "GET", gate, "")
	set := hdr.Get("Set-Cookie")
	if code != 303 || hdr.Get("Location") != "/" || !strings.Contains(set, gateCookie+"=") || !strings.Contains(set, "Secure") || !strings.Contains(set, "HttpOnly") {
		t.Fatalf("gate: %d %v", code, hdr)
	}
	cookie := strings.SplitN(set, ";", 2)[0]
	if code, _, hdr := fetch(stranger, "GET", "/healthz", cookie); code != 200 || hdr.Get("X-Content-Type-Options") == "" {
		t.Fatalf("through the gate: %d", code)
	}
	if code, _, _ := fetch(stranger, "GET", "/api/auth/me", cookie); code != 401 {
		t.Fatalf("the gate is not a login: %d", code)
	}

	// Without a site of the operator's choice the panel shows its own: an
	// ordinary small site, with a "not found" page for what it has not got.
	own := func(decoy string) {
		t.Helper()
		if resp, out := e.do(t, "PUT", "/api/panel-access", map[string]any{"enabled": true, "mode": "own", "port": port, "decoy": decoy}, h); resp.StatusCode != 200 {
			t.Fatalf("cover site %q: %d %v", decoy, resp.StatusCode, out)
		}
	}
	own("")
	if code, body, hdr := fetch(stranger, "GET", "/", ""); code != 200 || !strings.Contains(body, "<article>") || strings.Contains(strings.ToLower(body), "portunus") || hdr.Get("Set-Cookie") != "" {
		t.Fatalf("generated cover site: %d %q", code, body)
	}
	for _, path := range []string{"/login/", "/api/auth/me", "/healthz"} {
		if code, body, _ := fetch(stranger, "GET", path, ""); code != 404 || !strings.Contains(body, "Page not found") {
			t.Fatalf("generated cover site, %s: %d %q", path, code, body)
		}
	}
	own(cover.URL)

	// The setup's one-off probe address answers a stranger, once known.
	resp, probe := e.do(t, "POST", "/api/panel-access/probe", nil, h)
	if resp.StatusCode != 200 || !strings.HasPrefix(probe["url"].(string), origin+"/") {
		t.Fatalf("probe: %d %v", resp.StatusCode, probe)
	}
	if code, body, hdr := fetch(stranger, "GET", strings.TrimPrefix(probe["url"].(string), origin), ""); code != 200 || body != probe["expect"] || hdr.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("probe address: %d %q %v", code, body, hdr)
	}
	// The panel's own check sees the certificate (not a public one here).
	if resp, check := e.do(t, "POST", "/api/panel-access/check", nil, h); resp.StatusCode != 200 || check["ok"] != true || check["trusted"] != false {
		t.Fatalf("check: %d %v", resp.StatusCode, check)
	}

	// The port is the panel's: no other TCP listener may take it.
	if resp, out := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "trojan", "port": port, "serverName": domain}, h); resp.StatusCode != 400 || out["code"] != "panel_port" {
		t.Fatalf("inbound on the panel's port: %d %v", resp.StatusCode, out)
	}

	// A new gate shuts out the old address and the browsers it let in.
	resp, rotated := e.do(t, "POST", "/api/panel-access/rotate", nil, h)
	if resp.StatusCode != 200 || rotated["url"] == entry {
		t.Fatalf("rotate: %d %v", resp.StatusCode, rotated)
	}
	if _, body, _ := fetch(stranger, "GET", "/healthz", cookie); body != covered {
		t.Fatalf("old gate cookie after rotation: %q", body)
	}
	if _, body, _ := fetch(stranger, "GET", gate, ""); body != covered {
		t.Fatalf("old gate after rotation: %q", body)
	}

	// VLESS may take the panel's port: the two share it. The connection
	// then presents the panel's domain, the panel leaves the public port
	// and stays reachable on its inner one; nothing else may take the port.
	resp, vless := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": port, "realityServer": "www.example.org"}, h)
	if resp.StatusCode != 201 || vless["realityServer"] != domain {
		t.Fatalf("vless on the panel's port: %d %v", resp.StatusCode, vless)
	}
	_, shared := e.do(t, "GET", "/api/panel-access", nil, h)
	if shared["shared"] != true {
		t.Fatalf("not shared: %v", shared)
	}
	if conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err == nil {
		conn.Close()
		t.Fatal("the panel still holds the port it shares")
	}
	innerConn, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(core.PanelInnerPort)), &tls.Config{ServerName: domain, InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("inner listener: %v", err)
	}
	innerConn.Close()
	if resp, out := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "trojan", "port": port, "serverName": domain}, h); resp.StatusCode != 400 {
		t.Fatalf("trojan on the shared port: %d %v", resp.StatusCode, out)
	}
	// Back to the panel alone.
	if resp, _ := e.do(t, "DELETE", "/api/inbounds/"+strconv.Itoa(int(vless["id"].(float64))), nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete vless: %d", resp.StatusCode)
	}
	e.core.Reconcile(ctx)
	if _, body, _ := fetch(stranger, "GET", "/", ""); body != covered {
		t.Fatalf("panel did not take its port back: %q", body)
	}

	// A connection that would ask Let's Encrypt for a certificate of the
	// same domain serves the panel's instead: no second certificate.
	hyPort := freeTCPPort(t)
	resp, hy := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "hysteria2", "port": hyPort, "serverName": domain, "tlsMode": "acme"}, h)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create hysteria2: %d %v", resp.StatusCode, hy)
	}
	certFile, keyFile := e.core.PanelCertFiles()
	for _, f := range []string{certFile, keyFile} {
		if st, err := os.Stat(f); err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("shared certificate file %s: %v", f, err)
		}
	}
	if info, _ := hy["certificate"].(map[string]any); info == nil || info["issuer"] == "" {
		t.Fatalf("the connection does not report the panel's certificate: %v", hy["certificate"])
	}
	// Another domain is not covered by it and keeps its own way.
	_, otherIn := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "tuic", "port": freeTCPPort(t), "serverName": "other.example.com", "tlsMode": "acme"}, h)
	if otherIn["certificate"] != nil {
		t.Fatalf("a connection on another domain got the panel's certificate: %v", otherIn["certificate"])
	}
	for _, in := range []map[string]any{hy, otherIn} {
		if id, ok := in["id"].(float64); ok {
			e.do(t, "DELETE", "/api/inbounds/"+strconv.Itoa(int(id)), nil, h)
		}
	}

	// Guessing the secret word shuts the gate to the guesser: after too
	// many word-like paths that are not it, the right one shows the cover
	// site as well. A browser already through is not affected.
	_, now := e.do(t, "GET", "/api/panel-access", nil, h)
	word := strings.TrimPrefix(now["url"].(string), origin)
	if code, _, _ := fetch(stranger, "GET", word, ""); code != 303 {
		t.Fatalf("the gate before any guessing: %d", code)
	}
	for i := range gateGuesses + 1 {
		fetch(stranger, "GET", "/guess"+string(rune('a'+i)), "")
	}
	if code, body, hdr := fetch(stranger, "GET", word, ""); code != 200 || body != covered || hdr.Get("Set-Cookie") != "" {
		t.Fatalf("the gate after %d wrong guesses: %d %q", gateGuesses+1, code, body)
	}
	if code, _, _ := fetch(stranger, "GET", "/healthz", cookieNow(t, e, h, origin, stranger)); code != 200 {
		t.Fatalf("a browser already through is shut out: %d", code)
	}

	// Off again: nothing listens.
	if resp, out := e.do(t, "PUT", "/api/panel-access", map[string]any{"enabled": false}, h); resp.StatusCode != 200 || out["enabled"] != false {
		t.Fatalf("disable: %d %v", resp.StatusCode, out)
	}
	if conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err == nil {
		conn.Close()
		t.Fatal("still listening after being switched off")
	}
}

// cookieNow is the gate cookie a browser let in earlier would carry.
func cookieNow(t *testing.T, e *testEnv, h map[string]string, origin string, c *http.Client) string {
	t.Helper()
	pa, err := e.core.PanelAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return gateCookie + "=" + e.core.PanelGateToken(pa.Gate)
}
