package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PaulichP/Portunus/server/internal/store"
	"github.com/PaulichP/Portunus/server/internal/version"
)

// Map tiles are fetched by the panel and handed to the browser, so the
// browser keeps talking to the panel only (the CSP allows nothing else) and
// the tile provider sees the server, not the operator. Tiles are kept on
// disk: a map of a handful of servers is looked at in the same places.

const (
	tileMaxZoom   = 12
	tileCacheAge  = 30 * 24 * time.Hour
	tileCacheKeep = 6000 // files; the oldest go first beyond this
)

var tileStyles = map[string]bool{"light": true, "dark": true}

type tileCache struct {
	dir  string
	mu   sync.Mutex
	n    int
	sem  chan struct{}
	http *http.Client
}

func newTileCache(dir string) *tileCache {
	return &tileCache{
		dir:  dir,
		n:    -1,
		sem:  make(chan struct{}, 8),
		http: &http.Client{Timeout: 12 * time.Second},
	}
}

func (s *Server) routesMap(mux *http.ServeMux) {
	view := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleViewer, h)) }
	mux.Handle("GET /api/map/tiles/{style}/{z}/{x}/{y}", view(s.handleMapTile))
}

func (s *Server) handleMapTile(w http.ResponseWriter, r *http.Request) {
	style := r.PathValue("style")
	ok := tileStyles[style]
	// One set of tiles serves both themes unless the provider has two.
	if !strings.Contains(s.cfg.MapTiles, "{style}") {
		style = "light"
	}
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".png"))
	if s.cfg.MapTiles == "" || !ok || errZ != nil || errX != nil || errY != nil || z < 0 || z > tileMaxZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		http.NotFound(w, r)
		return
	}
	data, err := s.tiles.get(r.Context(), s.cfg.MapTiles, style, z, x, y)
	if err != nil {
		s.log.Debug("map tile", "err", err)
		http.Error(w, "tile unavailable", http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "private, max-age=604800")
	w.Write(data) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- PNG bytes with an image content type
}

func (c *tileCache) path(style string, z, x, y int) string {
	return filepath.Join(c.dir, style, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
}

func (c *tileCache) get(ctx context.Context, template, style string, z, x, y int) ([]byte, error) {
	path := c.path(style, z, x, y)
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < tileCacheAge {
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		}
	}
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	url := strings.NewReplacer("{style}", style, "{z}", strconv.Itoa(z), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y)).Replace(template)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Tile servers ask applications to say who they are.
	req.Header.Set("User-Agent", "Portunus/"+version.Version+" (+https://github.com/PaulichP/Portunus)")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		return nil, fmt.Errorf("tile server answered %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	c.store(path, data)
	return data, nil
}

// store writes a tile and keeps the cache within its size.
func (c *tileCache) store(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) != nil || os.Rename(tmp, path) != nil {
		os.Remove(tmp)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n < 0 {
		c.n = 0
		filepath.WalkDir(c.dir, func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				c.n++
			}
			return nil
		})
	} else {
		c.n++
	}
	if c.n > tileCacheKeep {
		// Rare and cheap enough: start over rather than track ages.
		os.RemoveAll(c.dir)
		c.n = 0
	}
}
