package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/PaulichP/Portunus/server/internal/auth"
	"github.com/PaulichP/Portunus/server/internal/config"
	"github.com/PaulichP/Portunus/server/internal/db"
	"github.com/PaulichP/Portunus/server/internal/store"
)

type testEnv struct {
	srv      *httptest.Server
	client   *http.Client
	password string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	cfg := &config.Config{
		DataDir:       t.TempDir(),
		Listen:        config.DefaultListen,
		CookieSecure:  config.CookieSecureAuto,
		SessionTTL:    time.Hour,
		SessionMaxAge: 24 * time.Hour,
	}
	sqlDB, err := db.Open(ctx, filepath.Join(cfg.DataDir, "portunus.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	st := store.New(sqlDB)
	as, err := auth.NewService(st, log, cfg.SessionTTL, cfg.SessionMaxAge)
	if err != nil {
		t.Fatal(err)
	}
	_, pw, err := as.Bootstrap(ctx)
	if err != nil || pw == "" {
		t.Fatalf("bootstrap: %q, %v", pw, err)
	}
	if _, again, _ := as.Bootstrap(ctx); again != "" {
		t.Fatal("bootstrap ran twice")
	}

	srv := httptest.NewServer(New(cfg, log, st, as).Handler(http.NotFoundHandler()))
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &testEnv{srv: srv, client: &http.Client{Jar: jar}, password: pw}
}

func (e *testEnv) do(t *testing.T, method, path string, body any, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestAuthFlow(t *testing.T) {
	e := newTestEnv(t)

	if resp, _ := e.do(t, "GET", "/healthz", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "GET", "/api/auth/me", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("me without session: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong-password"}, nil); resp.StatusCode != 401 {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": e.password},
		map[string]string{"Origin": "https://evil.example"}); resp.StatusCode != 403 {
		t.Fatalf("cross-origin login: %d", resp.StatusCode)
	}

	resp, body := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "ADMIN", "password": e.password}, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d %v", resp.StatusCode, body)
	}
	c := resp.Cookies()
	if len(c) != 1 || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie attributes: %+v", c)
	}
	csrf, _ := body["csrfToken"].(string)
	user, _ := body["user"].(map[string]any)
	if csrf == "" || user["mustChangePassword"] != true {
		t.Fatalf("login body: %v", body)
	}

	if resp, body := e.do(t, "GET", "/api/system", nil, nil); resp.StatusCode != 403 || body["error"] != "password_change_required" {
		t.Fatalf("system before password change: %d %v", resp.StatusCode, body)
	}

	change := map[string]string{"currentPassword": e.password, "newPassword": "a-much-better-password"}
	if resp, _ := e.do(t, "POST", "/api/auth/password", change, nil); resp.StatusCode != 403 {
		t.Fatalf("password change without CSRF: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "POST", "/api/auth/password", change, map[string]string{csrfHeader: "nope"}); resp.StatusCode != 403 {
		t.Fatalf("password change with bad CSRF: %d", resp.StatusCode)
	}
	short := map[string]string{"currentPassword": e.password, "newPassword": "short"}
	if resp, body := e.do(t, "POST", "/api/auth/password", short, map[string]string{csrfHeader: csrf}); resp.StatusCode != 400 || body["error"] != "password_too_weak" {
		t.Fatalf("weak password: %d %v", resp.StatusCode, body)
	}
	if resp, _ := e.do(t, "POST", "/api/auth/password", change, map[string]string{csrfHeader: csrf}); resp.StatusCode != 204 {
		t.Fatalf("password change: %d", resp.StatusCode)
	}
	if resp, body := e.do(t, "GET", "/api/auth/me", nil, nil); resp.StatusCode != 200 || body["user"].(map[string]any)["mustChangePassword"] != false {
		t.Fatalf("me after change: %d %v", resp.StatusCode, body)
	}

	if resp, _ := e.do(t, "GET", "/api/system", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("system: %d", resp.StatusCode)
	}

	if resp, _ := e.do(t, "POST", "/api/auth/logout", nil, map[string]string{csrfHeader: csrf}); resp.StatusCode != 204 {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "GET", "/api/auth/me", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("me after logout: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "a-much-better-password"}, nil); resp.StatusCode != 200 {
		t.Fatalf("login with new password: %d", resp.StatusCode)
	}
}

func TestLoginRequiresJSON(t *testing.T) {
	e := newTestEnv(t)
	resp, err := e.client.Post(e.srv.URL+"/api/auth/login", "application/x-www-form-urlencoded",
		bytes.NewBufferString("username=admin&password=x"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form login: %d", resp.StatusCode)
	}
}

func TestLoginRateLimited(t *testing.T) {
	e := newTestEnv(t)
	var last *http.Response
	for range 8 {
		last, _ = e.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "wrong-password"}, nil)
	}
	if last.StatusCode != http.StatusTooManyRequests || last.Header.Get("Retry-After") == "" {
		t.Fatalf("expected 429 with Retry-After, got %d", last.StatusCode)
	}
}
