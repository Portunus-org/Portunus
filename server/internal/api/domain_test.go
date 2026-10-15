package api

import (
	"net/http"
	"testing"
)

// The domain section: its state, a move refused at the field it is about,
// and the server's own certificate kept.
func TestDomainEndpoints(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

	resp, out := e.do(t, "GET", "/api/domain", nil, h)
	if resp.StatusCode != http.StatusOK || out["domain"] != "" || out["others"] == nil {
		t.Fatalf("state: %d %v", resp.StatusCode, out)
	}
	// A name that leads nowhere would leave the panel behind it.
	resp, out = e.do(t, "POST", "/api/domain", map[string]any{"domain": "nowhere.invalid"}, h)
	if resp.StatusCode != http.StatusBadRequest || out["field"] != "domain" || out["code"] != "points_elsewhere" {
		t.Fatalf("move: %d %v", resp.StatusCode, out)
	}
	if _, out = e.do(t, "GET", "/api/domain", nil, h); out["domain"] != "" || out["next"] != nil {
		t.Fatalf("moved after a refusal: %v", out)
	}
	resp, out = e.do(t, "DELETE", "/api/certificates/..%2Fpanel-acme", nil, h)
	if resp.StatusCode != http.StatusBadRequest || out["code"] != "invalid_hostname" {
		t.Fatalf("remove by a path: %d %v", resp.StatusCode, out)
	}
	resp, _ = e.do(t, "DELETE", "/api/certificates/gone.example.com", nil, h)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("remove what is not there: %d", resp.StatusCode)
	}
}
