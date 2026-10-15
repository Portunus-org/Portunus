package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Portunus-org/Portunus/server/internal/selfupdate"
	"github.com/Portunus-org/Portunus/server/internal/version"
)

// registry is an image registry as the public ones behave: nothing without
// a token, a token for anyone who asks where the challenge points, and a
// tag list that comes in pages.
func registry(t *testing.T, pages ...[]string) (*httptest.Server, *int) {
	t.Helper()
	asked := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:owner/panel:pull" || r.URL.Query().Get("service") != "registry" {
				t.Errorf("token asked with %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"token":"anonymous"}`)
		case r.URL.Path == "/v2/owner/panel/tags/list":
			if r.Header.Get("Authorization") != "Bearer anonymous" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="registry",scope="repository:owner/panel:pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			asked++
			page := 0
			fmt.Sscan(r.URL.Query().Get("page"), &page)
			if page+1 < len(pages) {
				w.Header().Set("Link", fmt.Sprintf(`</v2/owner/panel/tags/list?n=1000&page=%d>; rel="next"`, page+1))
			}
			fmt.Fprintf(w, `{"name":"owner/panel","tags":["%s"]}`, strings.Join(pages[page], `","`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func TestThePanelLearnsOfANewRelease(t *testing.T) {
	defer func(v string) { version.Version = v }(version.Version)
	// The newest release is on the second page, and is not the last tag in
	// any order of letters: v0.10.0 is newer than v0.9.3.
	srv, asked := registry(t, []string{"latest", "v0.1.0", "v0.9.3", "v0.2.4-rc1", "nightly"}, []string{"v0.10.0", "v0.2.4"})
	c, _ := newBindingCore(t)
	c.updates.source, c.updates.client = srv.URL+"/owner/panel", srv.Client()

	version.Version = "v0.2.4"
	if st := c.UpdateState(context.Background()); st.Available || st.Latest != "" || st.Current != "v0.2.4" || st.Off {
		t.Fatalf("before the first check: %+v", st)
	}
	st := c.CheckUpdate(context.Background())
	if !st.Available || st.Latest != "v0.10.0" || st.Error != "" || st.CheckedAt.IsZero() {
		t.Fatalf("a newer release went unnoticed: %+v", st)
	}
	if *asked != 2 {
		t.Fatalf("the tag list was read in %d requests, there are 2 pages", *asked)
	}
	// Asked again at once, the answer comes from memory.
	if st := c.CheckUpdate(context.Background()); !st.Available || *asked != 2 {
		t.Fatalf("the registry was asked again: %d requests", *asked)
	}

	for current, behind := range map[string]bool{"v0.10.0": false, "v0.10.1": false, "v1.0.0": false, "v0.9.9": true, "dev": false} {
		if selfupdate.Newer("v0.10.0", current) != behind {
			t.Errorf("%s: behind v0.10.0 = %v, want %v", current, !behind, behind)
		}
	}
}

func TestAFailedCheckKeepsWhatIsKnown(t *testing.T) {
	defer func(v string) { version.Version = v }(version.Version)
	version.Version = "v0.2.4"
	srv, _ := registry(t, []string{"v0.2.5"})
	c, _ := newBindingCore(t)
	c.updates.source, c.updates.client = srv.URL+"/owner/panel", srv.Client()
	if st := c.CheckUpdate(context.Background()); !st.Available {
		t.Fatalf("%+v", st)
	}
	// The registry goes away: the release found earlier is still there to
	// install, and the reason is reported.
	srv.Close()
	c.updates.asked = c.updates.asked.Add(-updateFresh)
	st := c.CheckUpdate(context.Background())
	if !st.Available || st.Latest != "v0.2.5" || st.Error == "" {
		t.Fatalf("after a failed check: %+v", st)
	}

	// Turned off: nothing is asked, and the state says so.
	off, _ := newBindingCore(t)
	if st := off.CheckUpdate(context.Background()); !st.Off || st.Available || !st.CheckedAt.IsZero() {
		t.Fatalf("turned off: %+v", st)
	}
}
