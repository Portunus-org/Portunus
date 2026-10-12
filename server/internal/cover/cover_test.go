package cover

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func get(s *Site, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w
}

// One installation always shows the same site, another shows a different
// one, and every link on it leads somewhere.
func TestSite(t *testing.T) {
	a, again, b := New([]byte("one"), "a.example.com"), New([]byte("one"), "a.example.com"), New([]byte("two"), "a.example.com")
	front := get(a, "GET", "/").Body.String()
	if front != get(again, "GET", "/").Body.String() {
		t.Fatal("the site changes between starts")
	}
	if front == get(b, "GET", "/").Body.String() {
		t.Fatal("two installations show the same site")
	}
	if strings.Contains(strings.ToLower(front), "portunus") {
		t.Fatal("the site names the panel")
	}
	links := regexp.MustCompile(`href="(/[^"]*)"`).FindAllStringSubmatch(front, -1)
	if len(links) < 8 {
		t.Fatalf("only %d links on the front page", len(links))
	}
	for _, l := range links {
		if w := get(a, "GET", l[1]); w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("%s: %d", l[1], w.Code)
		}
	}
	if w := get(a, "GET", "/login/"); w.Code != 404 || !strings.Contains(w.Body.String(), "Page not found") {
		t.Fatalf("unknown page: %d", w.Code)
	}
	if w := get(a, "POST", "/"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
	if w := get(a, "HEAD", "/about"); w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: %d, %d bytes", w.Code, w.Body.Len())
	}
	if w := get(a, "GET", "/about/"); w.Code != 301 {
		t.Fatalf("trailing slash: %d", w.Code)
	}
	// Many seeds: nothing panics, every site has its pages.
	for i := range 500 {
		s := New([]byte{byte(i), byte(i >> 8)}, "x.example.org")
		if get(s, "GET", "/").Code != 200 || get(s, "GET", "/about").Code != 200 || len(s.pages) < 10 {
			t.Fatalf("seed %d: incomplete site", i)
		}
	}
}
