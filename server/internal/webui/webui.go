// Package webui serves the statically exported frontend with a strict,
// per-page Content-Security-Policy.
//
// Next.js static export emits inline bootstrap <script> blocks. Instead of
// allowing 'unsafe-inline', the handler hashes every inline script of every
// HTML page at startup and pins exactly those hashes in that page's CSP.
package webui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Handler struct {
	files fs.FS
	pages map[string]*page // clean path of .html file -> page
	// gz holds the compressed form of text files, made on first use: the
	// files never change while the process runs, so each is compressed
	// once, as well as it can be.
	gz sync.Map // name -> []byte
	// build identifies this build of the interface: a hash of its pages.
	// A tab left open across an upgrade compares it and reloads itself
	// (web: FreshBuild), instead of going on with the old scripts.
	build string
}

// BuildPath is where the build's identifier is served.
const BuildPath = "build-id"

type page struct {
	body []byte
	gz   []byte
	csp  string
}

// compress gzips text at the best ratio; it runs once per file.
func compress(b []byte) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

// compressible lists what is worth compressing: text. Fonts (woff2) and
// images are compressed already.
var compressible = map[string]bool{".js": true, ".css": true, ".html": true, ".txt": true, ".json": true, ".svg": true, ".map": true, ".xml": true}

func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if name, _, _ := strings.Cut(strings.TrimSpace(enc), ";"); name == "gzip" {
			return true
		}
	}
	return false
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
		h.pages[p] = &page{body: body, gz: compress(body), csp: pageCSP(p, body)}
		return nil
	})
	names := make([]string, 0, len(h.pages))
	for name := range h.pages {
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		sum.Write(h.pages[name].body)
	}
	h.build = hex.EncodeToString(sum.Sum(nil)[:8])
	return h, err
}

func pageCSP(name string, body []byte) string {
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

	// The first setup and the settings check from the browser that the
	// panel can be reached by its domain over HTTPS, which is another
	// origin. That is allowed on every page and not on those two alone: the
	// panel moves between its pages without loading a new document, so the
	// policy in force is that of whichever page was opened first, usually
	// the sign-in. What a page can run is still pinned by hash above, so
	// there is no foreign script to make use of the wider reach.
	connect := "connect-src 'self' https:"

	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' " + strings.Join(hashes, " "),
		// React and Radix set inline style attributes for positioning.
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		connect,
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

	if name == BuildPath {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		// nosemgrep: go.lang.security.audit.xss.no-io-writestring-to-responsewriter.no-io-writestring-to-responsewriter -- plain text: the identifier of this build, fixed when it was built
		io.WriteString(w, h.build)
		return
	}

	// Routes are exported with trailingSlash, i.e. /login/ -> login/index.html.
	if st, err := fs.Stat(h.files, name); err == nil && st.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			// Built from the cleaned name, never from the raw path: a
			// request for //evil.example must not redirect off-site.
			target := "/" + name + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently) // nosemgrep: go.lang.security.injection.open-redirect.open-redirect -- target is a cleaned local path, always "/name/"
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
	w.Header().Add("Vary", "Accept-Encoding")
	if ext := path.Ext(name); compressible[ext] && acceptsGzip(r) && r.Header.Get("Range") == "" {
		gz, ok := h.gz.Load(name)
		if !ok {
			raw, err := io.ReadAll(f)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			gz, _ = h.gz.LoadOrStore(name, compress(raw))
		}
		body := gz.([]byte)
		hdr := w.Header()
		if ct := mime.TypeByExtension(ext); ct != "" {
			hdr.Set("Content-Type", ct)
		}
		hdr.Set("Content-Encoding", "gzip")
		hdr.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != http.MethodHead {
			w.Write(body) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- an embedded build file, compressed; no request data
		}
		return
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
	hdr.Add("Vary", "Accept-Encoding")
	body := p.body
	if acceptsGzip(r) {
		body = p.gz
		hdr.Set("Content-Encoding", "gzip")
	}
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(body) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- the embedded, prebuilt page; no request data
	}
}
