package api

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
)

func TestAuditLog(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	_, created := e.do(t, "POST", "/api/users", map[string]any{"username": "vera", "role": "viewer"}, h)
	e.do(t, "POST", "/api/clients", map[string]any{"name": "<b>Anna</b>", "enabled": true}, h)

	var log struct {
		Entries []struct {
			Action, Actor, Target string
			Details               map[string]any
		}
		Limit int
	}
	if code := e.getJSON(t, "/api/audit", &log); code != 200 {
		t.Fatalf("admin reads the audit log: %d", code)
	}
	if len(log.Entries) < 3 || log.Limit != auditShown {
		t.Fatalf("entries: %+v", log)
	}
	// Newest first: the client, then the account, then the sign-in.
	if a := log.Entries[0]; a.Action != "client.create" || a.Target != "<b>Anna</b>" || a.Actor != "admin" {
		t.Fatalf("newest entry: %+v", a)
	}
	if a := log.Entries[1]; a.Action != "user.create" || a.Details["role"] != "viewer" {
		t.Fatalf("second entry: %+v", a)
	}

	// Nobody but an administrator reads it.
	jar, _ := cookiejar.New(nil)
	viewer := &testEnv{srv: e.srv, client: &http.Client{Jar: jar}, password: created["password"].(string)}
	_, body := viewer.do(t, "POST", "/api/auth/login", map[string]string{"username": "vera", "password": viewer.password}, nil)
	viewer.do(t, "POST", "/api/auth/password", map[string]string{"currentPassword": viewer.password, "newPassword": "vera-has-a-long-password"},
		map[string]string{csrfHeader: body["csrfToken"].(string)})
	if code := viewer.getJSON(t, "/api/audit", nil); code != 403 {
		t.Fatalf("viewer reads the audit log: %d", code)
	}
	anon := &testEnv{srv: e.srv, client: &http.Client{}}
	if code := anon.getJSON(t, "/api/audit", nil); code != 401 {
		t.Fatalf("anonymous reads the audit log: %d", code)
	}
}
