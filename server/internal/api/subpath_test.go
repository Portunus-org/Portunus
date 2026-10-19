package api

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// On the panel's domain, subscriptions are under their path, which the
// operator can change; anything else under it shows the cover site. The
// CA certificate is there too, and under /sub/ on the subscription port.
func TestSubscriptionsOnTheDomain(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	ctx := context.Background()
	settings := func(path string) (int, map[string]any) {
		resp, out := e.do(t, "PUT", "/api/settings", map[string]any{"publicHost": "vpn.example.com", "subscriptionBaseUrl": "https://sub.example.com", "subscriptionPath": path}, h)
		return resp.StatusCode, out
	}
	if code, out := settings("sub"); code != 200 || out["subscriptionPath"] != "sub" {
		t.Fatalf("settings: %d %v", code, out)
	}
	if code, out := settings("api"); code != 400 || out["field"] != "subscriptionPath" {
		t.Fatalf("a path of the panel: %d %v", code, out)
	}
	_, client := e.do(t, "POST", "/api/clients", map[string]any{"name": "anna"}, h)
	var access struct {
		SubscriptionURL string `json:"subscriptionUrl"`
	}
	e.getJSON(t, "/api/clients/"+itoa(int(client["id"].(float64)))+"/access", &access)
	token := strings.TrimPrefix(access.SubscriptionURL, "https://sub.example.com/sub/")
	if len(token) < 32 || strings.Contains(token, "/") {
		t.Fatalf("subscription url %q", access.SubscriptionURL)
	}
	if code, _ := settings("my-feed"); code != 200 {
		t.Fatal("own path refused")
	}

	s := e.api
	s.syncPanelAccess(ctx)
	s.panel.mu.Lock()
	s.panel.gate, s.panel.token = "lantern", e.core.PanelGateToken("lantern")
	s.panel.domain, s.panel.url = "vpn.example.com", "https://vpn.example.com/lantern"
	sub := s.panel.subPath
	s.panel.mu.Unlock()
	if sub != "my-feed" {
		t.Fatalf("the listener's path: %q", sub)
	}
	panel := s.gated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("panel")) }))
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://vpn.example.com"+path, nil)
		r.TLS = &tls.ConnectionState{ServerName: "vpn.example.com"}
		r.RemoteAddr = "203.0.113.9:4000"
		w := httptest.NewRecorder()
		panel.ServeHTTP(w, r)
		return w
	}
	if w := get("/my-feed/" + token); w.Code != 200 || w.Header().Get("Subscription-Userinfo") == "" {
		t.Fatalf("subscription by the domain: %d %v", w.Code, w.Header())
	}
	for _, p := range []string{"/my-feed/" + strings.Repeat("a", 43), "/sub/" + token, "/my-feed/", "/my-feed/" + token + "/x"} {
		if w := get(p); w.Header().Get("Subscription-Userinfo") != "" || w.Body.String() == "panel" {
			t.Fatalf("%s: answered as a subscription or the panel (%d)", p, w.Code)
		}
	}
	if w := get("/my-feed/ca.crt"); w.Code != 200 || !strings.Contains(w.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatalf("CA by the domain: %d", w.Code)
	}
	// The subscription port: the CA under /sub/ as well.
	port := httptest.NewServer(s.SubscriptionHandler())
	t.Cleanup(port.Close)
	resp, err := http.Get(port.URL + "/sub/ca.crt")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("CA on the subscription port: %v %v", err, resp)
	}
	resp.Body.Close()
	if resp, err := http.Get(port.URL + "/sub/" + token); err != nil || resp.StatusCode != 200 {
		t.Fatalf("subscription on its port: %v %v", err, resp)
	}
}
