package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	files := fstest.MapFS{
		"index.html":                 {Data: []byte(`<html><script src="/_next/static/a.js"></script><script>boot()</script></html>`)},
		"login/index.html":           {Data: []byte(`<html><script>login()</script></html>`)},
		"404.html":                   {Data: []byte(`<html>not here</html>`)},
		"_next/static/chunks/app.js": {Data: []byte(`console.log(1)`)},
		"icon.svg":                   {Data: []byte(`<svg/>`)},
		"evil.example/index.html":    {Data: []byte(`<html></html>`)},
	}
	h, err := New(files)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path       string
		status     int
		wantCSP    string // substring
		wantCache  string
		wantHeader string
		location   string
	}{
		// sha256("boot()")
		{path: "/", status: 200, wantCSP: "script-src 'self' 'sha256-"},
		{path: "/login/", status: 200, wantCSP: "frame-ancestors 'none'"},
		{path: "/login", status: 301, location: "/login/"},
		// The redirect stays on this site whatever the raw path looks like.
		{path: "//evil.example", status: 301, location: "/evil.example/"},
		{path: "/_next/static/chunks/app.js", status: 200, wantCache: "immutable"},
		{path: "/icon.svg", status: 200, wantCache: "no-cache"},
		{path: "/missing", status: 404, wantCSP: "default-src 'self'"},
		{path: "/../../etc/passwd", status: 404},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if loc := rec.Header().Get("Location"); loc != c.location {
			t.Errorf("%s: Location %q, want %q", c.path, loc, c.location)
		}
		if c.wantCSP != "" && !strings.Contains(rec.Header().Get("Content-Security-Policy"), c.wantCSP) {
			t.Errorf("%s: CSP %q lacks %q", c.path, rec.Header().Get("Content-Security-Policy"), c.wantCSP)
		}
		if c.wantCache != "" && !strings.Contains(rec.Header().Get("Cache-Control"), c.wantCache) {
			t.Errorf("%s: Cache-Control %q lacks %q", c.path, rec.Header().Get("Cache-Control"), c.wantCache)
		}
	}
}

func TestPageCSPHashesOnlyInlineScripts(t *testing.T) {
	csp := pageCSP([]byte(`<script src="x.js"></script><script async>a()</script><SCRIPT>a()</SCRIPT><script></script>`))
	// Two identical inline scripts collapse to one hash; external and empty ones are skipped.
	if n := strings.Count(csp, "'sha256-"); n != 1 {
		t.Fatalf("want 1 hash, got %d in %q", n, csp)
	}
	// sha256("a()") in base64
	if !strings.Contains(csp, "'sha256-qVpDBgj7bpq5hMAcGp3AOc79J3Y1Z4HvySTwKrWDoy4='") {
		t.Fatalf("unexpected hash in %q", csp)
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatal("script-src must not allow unsafe-inline")
	}
}
