// Package cover makes the cover site: the small, ordinary site a stranger
// finds at the panel's domain. Nothing has to be set up for it. It is built
// from a seed, so one installation always shows the same site and two
// installations show different ones: another author, subject, set of notes,
// addresses and look.
package cover

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"html/template"
	"math/rand/v2" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- a repeatable generator on purpose: the same seed must give the same site
	"net/http"
	"strconv"
	"strings"
	"time"
)

type page struct {
	ctype string
	body  []byte
}

// Site is a generated site, ready to serve.
type Site struct {
	pages    map[string]page
	missing  []byte
	modified time.Time
}

type post struct {
	Title, Path, Date, Stamp string
	Paragraphs               []string
}

type view struct {
	Lang, Site, Tagline, Author, Style, Heading, Year string
	NotesPath, NotesWord, AboutWord                   string
	Posts                                             []post
	Post                                              *post
	About                                             []string
	Missing                                           bool
}

var layout = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .Post}}{{.Post.Title}} | {{end}}{{if .About}}{{.AboutWord}} | {{end}}{{if .Missing}}Not found | {{end}}{{.Site}}</title>
<meta name="description" content="{{.Tagline}}">
<link rel="stylesheet" href="{{.Style}}">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
</head>
<body>
<header>
<a href="/">{{.Site}}</a>
<nav><a href="/">{{.NotesWord}}</a> <a href="/about">{{.AboutWord}}</a></nav>
</header>
<main>
{{if .Missing}}<h1>Page not found</h1>
<p>There is nothing at this address. <a href="/">Back to the front page</a>.</p>
{{else if .Post}}<article>
<h1>{{.Post.Title}}</h1>
<time datetime="{{.Post.Stamp}}">{{.Post.Date}}</time>
{{range .Post.Paragraphs}}<p>{{.}}</p>
{{end}}</article>
{{else if .About}}<h1>{{.Heading}}</h1>
{{range .About}}<p>{{.}}</p>
{{end}}{{else}}<p>{{.Tagline}}</p>
{{range .Posts}}<article>
<h2><a href="{{.Path}}">{{.Title}}</a></h2>
<time datetime="{{.Stamp}}">{{.Date}}</time>
<p>{{index .Paragraphs 0}}</p>
</article>
{{end}}{{end}}</main>
<footer>&copy; {{.Year}} {{.Author}}</footer>
</body>
</html>
`))

// New builds the site that belongs to seed and domain.
func New(seed []byte, domain string) *Site {
	sum := sha256.Sum256(append(append([]byte("cover site\x00"), seed...), domain...))
	r := rand.New(rand.NewPCG(binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])))
	pick := func(from []string) string { return from[r.IntN(len(from))] }

	sub := subjects[r.IntN(len(subjects))]
	author := pick(firstNames) + " " + pick(lastNames)
	notes := pick([]string{"notes", "posts", "journal", "log", "entries"})
	v := view{
		Site:      strings.ReplaceAll(pick(sub.names), "{name}", strings.Fields(author)[0]),
		Tagline:   pick(sub.taglines),
		Author:    author,
		Style:     "/" + pick([]string{"style", "main", "site", "screen", "base"}) + ".css",
		NotesPath: "/" + notes,
		NotesWord: pick([]string{"Notes", "Journal", "Writing", "Archive"}),
		AboutWord: pick([]string{"About", "About me", "Who writes this"}),
	}

	// The newest note is a few weeks to a few months old, the rest go back
	// from it at uneven intervals.
	day := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -r.IntN(120))
	s := &Site{pages: map[string]page{}, modified: day}
	v.Year = strconv.Itoa(day.Year())
	titles := r.Perm(len(sub.titles))
	// Every note opens with a sentence of its own, so that the front page,
	// which shows the openings, does not repeat itself.
	leads := r.Perm(len(sub.sentences))
	for n, ti := range titles[:5+r.IntN(len(titles)-4)] {
		order := []int{leads[n]}
		for _, i := range r.Perm(len(sub.sentences)) {
			if i != leads[n] {
				order = append(order, i)
			}
		}
		var paragraphs []string
		for n := 2 + r.IntN(3); n > 0 && len(order) >= 2; n-- {
			size := min(2+r.IntN(3), len(order))
			var text []string
			for _, i := range order[:size] {
				text = append(text, sub.sentences[i])
			}
			order = order[size:]
			paragraphs = append(paragraphs, strings.Join(text, " "))
		}
		title := sub.titles[ti]
		v.Posts = append(v.Posts, post{
			Title:      title,
			Path:       v.NotesPath + "/" + slug(title),
			Date:       day.Format("2 January 2006"),
			Stamp:      day.Format("2006-01-02"),
			Paragraphs: paragraphs,
		})
		day = day.AddDate(0, 0, -(9 + r.IntN(70)))
	}

	render := func(v view) []byte {
		var b bytes.Buffer
		if err := layout.Execute(&b, v); err != nil {
			panic(err) // the template is fixed: this cannot happen
		}
		return b.Bytes()
	}
	html := func(path string, v view) { s.pages[path] = page{"text/html; charset=utf-8", render(v)} }
	html("/", v)
	for i := range v.Posts {
		pv := v
		pv.Post = &v.Posts[i]
		html(pv.Post.Path, pv)
	}
	av := v
	av.Heading = v.AboutWord
	av.About = []string{
		fmt.Sprintf(pick(aboutOpenings), author),
		pick(sub.abouts),
		fmt.Sprintf(pick(aboutEndings), "hello@"+domain),
	}
	html("/about", av)
	mv := v
	mv.Missing = true
	s.missing = render(mv)

	hue := r.IntN(360)
	s.pages[v.Style] = page{"text/css; charset=utf-8", []byte(stylesheet(r, hue))}
	s.pages["/favicon.svg"] = page{"image/svg+xml", fmt.Appendf(nil,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="%d" fill="hsl(%d 45%% 42%%)"/><text x="16" y="23" font-family="Georgia,serif" font-size="20" text-anchor="middle" fill="#fff">%s</text></svg>`,
		4+r.IntN(12), hue, template.HTMLEscapeString(v.Site[:1]))}
	s.pages["/robots.txt"] = page{"text/plain; charset=utf-8", []byte("User-agent: *\nAllow: /\n")}
	return s
}

func stylesheet(r *rand.Rand, hue int) string {
	fonts := []string{
		`Georgia, "Times New Roman", serif`,
		`system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`,
		`"Iowan Old Style", "Palatino Linotype", Palatino, serif`,
		`"Helvetica Neue", Helvetica, Arial, sans-serif`,
		`ui-monospace, "SF Mono", Menlo, Consolas, monospace`,
	}
	align := []string{"left", "center"}[r.IntN(2)]
	rule := []string{"1px solid var(--line)", "0", "3px solid var(--accent)"}[r.IntN(3)]
	return fmt.Sprintf(`:root{--accent:hsl(%[1]d 45%% 38%%);--text:#1c1c1c;--muted:#6b6b6b;--line:#e4e4e4;--back:hsl(%[1]d 20%% %[2]d%%)}
@media (prefers-color-scheme:dark){:root{--accent:hsl(%[1]d 55%% 70%%);--text:#e8e8e8;--muted:#9a9a9a;--line:#333;--back:hsl(%[1]d 8%% 11%%)}}
*{box-sizing:border-box}
body{margin:0 auto;padding:%[3]dpx 20px;max-width:%[4]dpx;font:%[5]dpx/%[6]s %[7]s;color:var(--text);background:var(--back)}
header{display:flex;flex-wrap:wrap;gap:12px 24px;align-items:baseline;justify-content:%[8]s;padding-bottom:16px;border-bottom:%[9]s;margin-bottom:32px}
header>a{font-size:1.3em;font-weight:600;color:var(--text);text-decoration:none}
nav a{margin-right:14px}
a{color:var(--accent)}
h1{font-size:1.7em;line-height:1.25;margin:0 0 6px}
h2{font-size:1.25em;margin:0 0 4px}
h2 a{text-decoration:none}
article{margin-bottom:36px}
time{display:block;color:var(--muted);font-size:.85em;margin-bottom:12px}
footer{margin-top:56px;padding-top:16px;border-top:1px solid var(--line);color:var(--muted);font-size:.85em;text-align:%[10]s}
`, hue, 96+r.IntN(4), 28+r.IntN(40), 600+20*r.IntN(9), 16+r.IntN(3), []string{"1.6", "1.7", "1.55"}[r.IntN(3)],
		fonts[r.IntN(len(fonts))], map[string]string{"left": "space-between", "center": "center"}[align], rule, align)
}

func slug(title string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(title) {
		switch {
		case c >= 'a' && c <= 'z' || c >= '0' && c <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(c)
		case c != '\'':
			dash = true
		}
	}
	return b.String()
}

func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "405 method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := r.URL.Path
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		// Only to a page of this site: the target is one of its own paths.
		if to := strings.TrimRight(path, "/"); s.pages[to].body != nil {
			http.Redirect(w, r, to, http.StatusMovedPermanently) // nosemgrep: go.lang.security.injection.open-redirect.open-redirect -- a key of s.pages, never an outside address
			return
		}
	}
	p, ok := s.pages[path]
	if !ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(s.missing)))
		w.WriteHeader(http.StatusNotFound)
		if r.Method == http.MethodGet {
			w.Write(s.missing) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- rendered by html/template
		}
		return
	}
	w.Header().Set("Content-Type", p.ctype)
	w.Header().Set("Cache-Control", "public, max-age=600")
	http.ServeContent(w, r, "", s.modified, bytes.NewReader(p.body))
}
