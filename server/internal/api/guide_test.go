package api

import (
	"net/http"
	"testing"
)

// The second half of the first setup is offered once the address is saved,
// until it is declined or there are both a connection and a client.
func TestSetupGuide(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	guide := func() bool {
		t.Helper()
		resp, out := e.do(t, "GET", "/api/setup", nil, h)
		if resp.StatusCode != 200 {
			t.Fatalf("setup state: %d", resp.StatusCode)
		}
		return out["guide"] == true
	}
	if guide() {
		t.Fatal("offered before the first half is done")
	}
	if resp, out := e.do(t, "POST", "/api/setup/basics", map[string]any{"publicHost": "203.0.113.10", "domain": ""}, h); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("basics: %d %v", resp.StatusCode, out)
	}
	if !guide() {
		t.Fatal("not offered after the address was saved")
	}
	// A connection alone does not end it; a client with it does.
	if resp, out := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "shadowsocks", "port": freeTCPPort(t)}, h); resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound: %d %v", resp.StatusCode, out)
	}
	if !guide() {
		t.Fatal("ended with a connection and no client")
	}
	if resp, out := e.do(t, "POST", "/api/clients", map[string]any{"name": "first"}, h); resp.StatusCode != http.StatusCreated {
		t.Fatalf("client: %d %v", resp.StatusCode, out)
	}
	if guide() {
		t.Fatal("still offered with a connection and a client")
	}

	// Declined: not offered again, whatever is missing.
	e2 := newTestEnv(t)
	h2 := map[string]string{csrfHeader: e2.loginFresh(t)}
	e2.do(t, "POST", "/api/setup/basics", map[string]any{"publicHost": "203.0.113.10", "domain": ""}, h2)
	if resp, _ := e2.do(t, "POST", "/api/setup/guide/done", nil, h2); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("decline: %d", resp.StatusCode)
	}
	if _, out := e2.do(t, "GET", "/api/setup", nil, h2); out["guide"] == true {
		t.Fatal("offered again after being declined")
	}
	// Skipping the first half altogether offers nothing either.
	e3 := newTestEnv(t)
	h3 := map[string]string{csrfHeader: e3.loginFresh(t)}
	e3.do(t, "POST", "/api/setup/skip", nil, h3)
	if _, out := e3.do(t, "GET", "/api/setup", nil, h3); out["guide"] == true || out["completed"] != true {
		t.Fatalf("after skipping: %v", out)
	}
}
