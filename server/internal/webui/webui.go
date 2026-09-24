// Package webui serves the statically exported frontend with a strict,
// per-page Content-Security-Policy.
//
// Next.js static export emits inline bootstrap <script> blocks. Instead of
// allowing 'unsafe-inline', the handler hashes every inline script of every
// HTML page at startup and pins exactly those hashes in that page's CSP.
package webui

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Handler struct {
	files fs.FS
	pages map[string]*page // clean path of .html file -> page
}

type page struct {
	body []byte
	csp  string
}

var scriptRE = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)

// New indexes all HTML pages in files. files may be nil (headless build),
// in which case every request gets a 404.
func New(files fs.FS) (*Handler, error) {
	h := &Handler{files: files, pages: map[string]*page{}}
	if files == nil {
		return h, nil
	}
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		body, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		h.pages[p] = &page{body: body, csp: pageCSP(body)}
		return nil
	})
	return h, err
}

func pageCSP(body []byte) string {
	seen := map[string]bool{}
	for _, m := range scriptRE.FindAllSubmatch(body, -1) {
		attrs, content := m[1], m[2]
		if bytes.Contains(bytes.ToLower(attrs), []byte("src=")) || len(content) == 0 {
			continue
		}
		sum := sha256.Sum256(content)
		seen["'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'"] = true
	}
	hashes := make([]string, 0, len(seen))
	for k := range seen {
		hashes = append(hashes, k)
	}
	sort.Strings(hashes)

	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' " + strings.Join(hashes, " "),
		// React and Radix set inline style attributes for positioning.
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.files == nil {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "."
	}

	// Routes are exported with trailingSlash, i.e. /login/ -> login/index.html.
	if st, err := fs.Stat(h.files, name); err == nil && st.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		name = path.Join(name, "index.html")
	}

	if p, ok := h.pages[name]; ok {
		h.servePage(w, r, p, http.StatusOK)
		return
	}
	f, err := h.files.Open(name)
	if err != nil {
		if p, ok := h.pages["404.html"]; ok {
			h.servePage(w, r, p, http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}

	// Build output under _next/static is content-hashed and safe to cache forever.
	if strings.HasPrefix(name, "_next/static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, time.Time{}, rs)
}

func (h *Handler) servePage(w http.ResponseWriter, r *http.Request, p *page, status int) {
	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Content-Security-Policy", p.csp)
	hdr.Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(p.body)
	}
}
