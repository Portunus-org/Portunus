package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/Portunus-org/Portunus/server/internal/auth"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

// loginFresh signs in as admin and replaces the generated password, returning
// the CSRF token of the resulting session.
func (e *testEnv) loginFresh(t *testing.T) string {
	t.Helper()
	_, body := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": e.password}, nil)
	csrf := body["csrfToken"].(string)
	resp, _ := e.do(t, "POST", "/api/auth/password",
		map[string]string{"currentPassword": e.password, "newPassword": "a-much-better-password"},
		map[string]string{csrfHeader: csrf})
	if resp.StatusCode != 204 {
		t.Fatalf("password change: %d", resp.StatusCode)
	}
	return csrf
}

func (e *testEnv) getJSON(t *testing.T, path string, v any) int {
	t.Helper()
	resp, err := e.client.Get(e.srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		json.NewDecoder(resp.Body).Decode(v)
	}
	return resp.StatusCode
}

func TestProxyFlow(t *testing.T) {
	e := newTestEnv(t)
	csrf := e.loginFresh(t)
	h := map[string]string{csrfHeader: csrf}

	// Settings.
	if resp, body := e.do(t, "PUT", "/api/settings", map[string]string{"publicHost": "bad host!"}, h); resp.StatusCode != 400 || body["field"] != "publicHost" {
		t.Fatalf("invalid host accepted: %d %v", resp.StatusCode, body)
	}
	if resp, _ := e.do(t, "PUT", "/api/settings", map[string]string{"publicHost": "Proxy.Example.com", "subscriptionBaseUrl": "https://sub.example.com/"}, h); resp.StatusCode != 200 {
		t.Fatalf("settings: %d", resp.StatusCode)
	}

	// Inbounds: VLESS on 443/tcp and Hysteria2 on 443/udp may coexist.
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": 443}, h); resp.StatusCode != 201 || body["realityPublicKey"] == "" {
		t.Fatalf("create vless: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "hysteria2", "port": 443, "serverName": "proxy.example.com", "masqueradeUrl": "ftp://x"}, h); resp.StatusCode != 400 || body["field"] != "masqueradeUrl" {
		t.Fatalf("bad masquerade accepted: %d %v", resp.StatusCode, body)
	}
	resp, hy := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "hysteria2", "port": 443, "serverName": "proxy.example.com", "masqueradeUrl": "https://www.bing.com", "obfs": true}, h)
	if resp.StatusCode != 201 || hy["obfs"] != true || hy["masqueradeUrl"] != "https://www.bing.com" {
		t.Fatalf("create hysteria2: %d %v", resp.StatusCode, hy)
	}
	hyID := int(hy["id"].(float64))
	// Turning obfuscation off keeps the certificate.
	resp, hy2 := e.do(t, "PUT", "/api/inbounds/"+itoa(hyID), map[string]any{"port": 443, "serverName": "proxy.example.com", "obfs": false}, h)
	if resp.StatusCode != 200 || hy2["obfs"] == true || hy2["certSha256"] != hy["certSha256"] || hy2["masqueradeUrl"] != nil {
		t.Fatalf("update hysteria2: %d %v", resp.StatusCode, hy2)
	}
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "trojan", "port": 443}, h); resp.StatusCode != 400 || body["code"] != "in_use" {
		t.Fatalf("tcp port clash not detected: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": 8443, "realityServer": "not a host"}, h); resp.StatusCode != 400 {
		t.Fatalf("bad reality server accepted: %d %v", resp.StatusCode, body)
	}
	var body map[string]any
	var inbounds []map[string]any
	if code := e.getJSON(t, "/api/inbounds", &inbounds); code != 200 || len(inbounds) != 2 {
		t.Fatalf("list inbounds: %d %v", code, inbounds)
	}
	vlessID := int(inbounds[0]["id"].(float64))
	oldKey := inbounds[0]["realityPublicKey"]
	resp, body = e.do(t, "POST", "/api/inbounds/"+itoa(vlessID)+"/rotate-keys", nil, h)
	if resp.StatusCode != 200 || body["realityPublicKey"] == oldKey {
		t.Fatalf("rotate keys: %d %v", resp.StatusCode, body)
	}

	// Clients.
	resp, body = e.do(t, "POST", "/api/clients", map[string]any{"name": "alice", "trafficLimit": 1000}, h)
	if resp.StatusCode != 201 || body["status"] != "active" {
		t.Fatalf("create client: %d %v", resp.StatusCode, body)
	}
	clientID := int(body["id"].(float64))
	if resp, body := e.do(t, "POST", "/api/clients", map[string]any{"name": "ALICE"}, h); resp.StatusCode != 400 || body["code"] != "taken" {
		t.Fatalf("duplicate client name: %d %v", resp.StatusCode, body)
	}

	var access struct {
		SubscriptionURL string `json:"subscriptionUrl"`
		Links           []struct {
			URL string `json:"url"`
			QR  string `json:"qr"`
		} `json:"links"`
	}
	if code := e.getJSON(t, "/api/clients/"+itoa(clientID)+"/access", &access); code != 200 || len(access.Links) != 2 {
		t.Fatalf("access: %d %+v", code, access)
	}
	if !strings.HasPrefix(access.Links[0].URL, "vless://") || !strings.Contains(access.Links[0].URL, "@proxy.example.com:443") ||
		!strings.HasPrefix(access.Links[0].QR, "<svg") {
		t.Fatalf("vless link: %+v", access.Links[0])
	}
	if !strings.HasPrefix(access.SubscriptionURL, "https://sub.example.com/sub/") {
		t.Fatalf("subscription url: %q", access.SubscriptionURL)
	}
	subPath := strings.TrimPrefix(access.SubscriptionURL, "https://sub.example.com")

	// Subscription (no session needed): base64 links by default…
	anon := &http.Client{}
	sub, err := anon.Get(e.srv.URL + subPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(sub.Body)
	sub.Body.Close()
	decoded, _ := base64.StdEncoding.DecodeString(string(raw))
	if sub.StatusCode != 200 || strings.Count(string(decoded), "://") != 2 ||
		!strings.Contains(sub.Header.Get("Subscription-Userinfo"), "total=1000") {
		t.Fatalf("subscription: %d %q %v", sub.StatusCode, decoded, sub.Header)
	}
	// …and a sing-box profile for sing-box apps.
	req, _ := http.NewRequest("GET", e.srv.URL+subPath, nil)
	req.Header.Set("User-Agent", "SFA/1.14.1 (Android)")
	sub, err = anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	json.NewDecoder(sub.Body).Decode(&profile)
	sub.Body.Close()
	if sub.StatusCode != 200 || profile["outbounds"] == nil {
		t.Fatalf("sing-box profile: %d %v", sub.StatusCode, profile)
	}
	if resp, _ := anon.Get(e.srv.URL + "/sub/" + strings.Repeat("x", 43)); resp.StatusCode != 404 {
		t.Fatalf("unknown token: %d", resp.StatusCode)
	}

	// Traffic over the limit marks the client "limited".
	e.store.AddTraffic(context.Background(), []store.TrafficDelta{{ClientID: int64(clientID), Upload: 600, Download: 500}})
	var clients []map[string]any
	e.getJSON(t, "/api/clients", &clients)
	if clients[0]["status"] != "limited" {
		t.Fatalf("status after exceeding limit: %v", clients[0])
	}
	// Over the limit the client is cut off, subscription included: the same
	// 404 as for an unknown token. So is one whose time has run out.
	if resp, _ := anon.Get(e.srv.URL + subPath); resp.StatusCode != 404 {
		t.Fatalf("subscription over the limit: %d", resp.StatusCode)
	}
	_, gone := e.do(t, "POST", "/api/clients", map[string]any{"name": "Gone", "enabled": true, "expiresAt": "2020-01-01T00:00:00Z"}, h)
	var goneAccess struct {
		SubscriptionURL string `json:"subscriptionUrl"`
	}
	e.getJSON(t, "/api/clients/"+itoa(int(gone["id"].(float64)))+"/access", &goneAccess)
	if resp, _ := anon.Get(e.srv.URL + goneAccess.SubscriptionURL[strings.Index(goneAccess.SubscriptionURL, "/sub/"):]); resp.StatusCode != 404 {
		t.Fatalf("subscription of an expired client: %d", resp.StatusCode)
	}
	if resp, body := e.do(t, "POST", "/api/clients/"+itoa(clientID)+"/reset-traffic", nil, h); resp.StatusCode != 200 || body["status"] != "active" {
		t.Fatalf("reset traffic: %d %v", resp.StatusCode, body)
	}
	// …and with the counter reset it is back.
	if resp, _ := anon.Get(e.srv.URL + subPath); resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Subscription-Userinfo"), "download=0") {
		t.Fatalf("subscription after the reset: %d %q", resp.StatusCode, resp.Header.Get("Subscription-Userinfo"))
	}

	// Rotation revokes the old subscription URL.
	if resp, _ := e.do(t, "POST", "/api/clients/"+itoa(clientID)+"/rotate", nil, h); resp.StatusCode != 200 {
		t.Fatalf("rotate client: %d", resp.StatusCode)
	}
	if resp, _ := anon.Get(e.srv.URL + subPath); resp.StatusCode != 404 {
		t.Fatalf("old subscription still valid: %d", resp.StatusCode)
	}

	// Disabling is a plain update.
	if resp, _ := e.do(t, "PUT", "/api/clients/"+itoa(clientID), map[string]any{"name": "alice", "enabled": false}, h); resp.StatusCode != 200 {
		t.Fatalf("disable client: %d", resp.StatusCode)
	}

	if resp, _ := e.do(t, "DELETE", "/api/inbounds/"+itoa(vlessID), nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete inbound: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "DELETE", "/api/clients/"+itoa(clientID), nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete client: %d", resp.StatusCode)
	}
}

func TestViewerIsReadOnly(t *testing.T) {
	e := newTestEnv(t)
	hash, _ := auth.HashPassword("viewer-password-1")
	if err := e.store.CreateUser(context.Background(), &store.User{Username: "viewer", PasswordHash: hash, Role: store.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	e.client = &http.Client{Jar: jar}
	_, body := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "viewer", "password": "viewer-password-1"}, nil)
	h := map[string]string{csrfHeader: body["csrfToken"].(string)}

	if code := e.getJSON(t, "/api/inbounds", nil); code != 200 {
		t.Fatalf("viewer list: %d", code)
	}
	if resp, _ := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": 443}, h); resp.StatusCode != 403 {
		t.Fatalf("viewer create: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "PUT", "/api/settings", map[string]any{"publicHost": "x.example.com"}, h); resp.StatusCode != 403 {
		t.Fatalf("viewer settings: %d", resp.StatusCode)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestCertificateModes(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

	// ACME needs a real domain.
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "hysteria2", "port": 443, "tlsMode": "acme", "serverName": "203.0.113.10"}, h); resp.StatusCode != 400 || body["code"] != "domain_required" {
		t.Fatalf("acme with IP: %d %v", resp.StatusCode, body)
	}
	resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "hysteria2", "port": 443, "tlsMode": "acme", "serverName": "proxy.example.com"}, h)
	if resp.StatusCode != 201 || body["tlsMode"] != "acme" || body["certificate"] != nil || body["certSha256"] != nil {
		t.Fatalf("acme inbound: %d %v", resp.StatusCode, body)
	}
	// HTTP-01 owns TCP 80 once an ACME inbound exists.
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": 80}, h); resp.StatusCode != 400 || body["code"] != "reserved" {
		t.Fatalf("tcp 80 not reserved: %d %v", resp.StatusCode, body)
	}

	// Challenge settings.
	if resp, body := e.do(t, "PUT", "/api/settings", map[string]any{"publicHost": "proxy.example.com", "acmeChallenge": "cloudflare"}, h); resp.StatusCode != 400 || body["code"] != "required" {
		t.Fatalf("cloudflare without token: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "PUT", "/api/settings", map[string]any{"publicHost": "proxy.example.com", "acmeChallenge": "cloudflare", "cloudflareToken": "tok", "acmeEmail": "ops@example.com"}, h); resp.StatusCode != 200 || body["cloudflareTokenSet"] != true || body["acmeChallenge"] != "cloudflare" {
		t.Fatalf("cloudflare: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "PUT", "/api/settings", map[string]any{"publicHost": "proxy.example.com", "acmeChallenge": "proxy", "acmeHttpPort": 8088}, h); resp.StatusCode != 200 || body["acmeHttpPort"] != float64(8088) {
		t.Fatalf("proxy challenge: %d %v", resp.StatusCode, body)
	}
	// With the proxy challenge, 80 is free again.
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": 80}, h); resp.StatusCode != 201 {
		t.Fatalf("tcp 80 with proxy challenge: %d %v", resp.StatusCode, body)
	}

	// File certificates must be readable and match the server name.
	if resp, body := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "trojan", "port": 8443, "tlsMode": "file", "serverName": "proxy.example.com", "certificatePath": "/nonexistent.pem", "keyPath": "/nonexistent.key"}, h); resp.StatusCode != 400 || body["field"] != "certificatePath" {
		t.Fatalf("missing cert file: %d %v", resp.StatusCode, body)
	}
}
