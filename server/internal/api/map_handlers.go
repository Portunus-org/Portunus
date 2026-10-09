package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	dir string
	mu  sync.Mutex
	n   int
	// detail is the resolved tile address of the detailed map.
	detail   string
	detailAt time.Time
	sem      chan struct{}
	http     *http.Client
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
	mux.Handle("GET /api/map/tiles/{style}/{z}/{x}/{y}", view(func(w http.ResponseWriter, r *http.Request) { s.handleMapTile(w, r, s.cfg.MapTiles) }))
	mux.Handle("GET /api/map/labels/{style}/{z}/{x}/{y}", view(func(w http.ResponseWriter, r *http.Request) { s.handleMapTile(w, r, s.cfg.MapLabels) }))
	mux.Handle("GET /api/map/detail/{z}/{x}/{y}", view(s.handleMapDetail))
}

// detailMaxZoom is as deep as vector tile sets go; the browser draws
// closer views from the same tiles.
const detailMaxZoom = 14

// handleMapDetail serves one vector tile of the detailed map.
func (s *Server) handleMapDetail(w http.ResponseWriter, r *http.Request) {
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".pbf"))
	if s.cfg.MapDetail == "" || errZ != nil || errX != nil || errY != nil || z < 0 || z > detailMaxZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		http.NotFound(w, r)
		return
	}
	template, err := s.tiles.detailTemplate(r.Context(), s.cfg.MapDetail)
	var data []byte
	if err == nil {
		// Cached under the tile set's address, not the dated template it
		// resolves to: a new weekly build is not a new map.
		data, err = s.tiles.fetch(r.Context(), s.tiles.path(s.cfg.MapDetail, "vector", z, x, y), tileURL(template, "", z, x, y), false)
	}
	if err != nil {
		s.log.Debug("map detail tile", "err", err)
		http.Error(w, "tile unavailable", http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/x-protobuf")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=604800")
	w.Write(data) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- binary vector tile with a protobuf content type and nosniff
}

// detailTemplate resolves the tile set's TileJSON to its tile address, and
// remembers it for a few hours: providers move it with every new build.
func (c *tileCache) detailTemplate(ctx context.Context, tileJSON string) (string, error) {
	c.mu.Lock()
	if c.detail != "" && time.Since(c.detailAt) < 6*time.Hour {
		defer c.mu.Unlock()
		return c.detail, nil
	}
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tileJSON, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Portunus/"+version.Version+" (+https://github.com/PaulichP/Portunus)")
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var doc struct {
		Tiles []string `json:"tiles"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&doc) != nil || len(doc.Tiles) == 0 {
		return "", fmt.Errorf("map detail: no tiles named by %s (%s)", tileJSON, res.Status)
	}
	u, err := url.Parse(doc.Tiles[0])
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.Contains(doc.Tiles[0], "{z}") {
		return "", fmt.Errorf("map detail: unusable tile address %q", doc.Tiles[0])
	}
	c.mu.Lock()
	c.detail, c.detailAt = doc.Tiles[0], time.Now()
	c.mu.Unlock()
	return doc.Tiles[0], nil
}

func tileURL(template, style string, z, x, y int) string {
	return strings.NewReplacer("{style}", style, "{z}", strconv.Itoa(z), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y)).Replace(template)
}

// handleMapTile serves one tile of the layer whose upstream is template.
func (s *Server) handleMapTile(w http.ResponseWriter, r *http.Request, template string) {
	style := r.PathValue("style")
	ok := tileStyles[style]
	// One set of tiles serves both themes unless the provider has two.
	if !strings.Contains(template, "{style}") {
		style = "light"
	}
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".png"))
	if template == "" || !ok || errZ != nil || errX != nil || errY != nil || z < 0 || z > tileMaxZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		http.NotFound(w, r)
		return
	}
	data, err := s.tiles.get(r.Context(), template, style, z, x, y)
	if err != nil {
		s.log.Debug("map tile", "err", err)
		http.Error(w, "tile unavailable", http.StatusBadGateway)
		return
	}
	h := w.Header()
	// Providers answer in PNG or JPEG; say which. Anything else is not
	// passed on as an image.
	ct := http.DetectContentType(data)
	if !strings.HasPrefix(ct, "image/") {
		http.Error(w, "tile unavailable", http.StatusBadGateway)
		return
	}
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, max-age=604800")
	w.Write(data) // nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter -- PNG bytes with an image content type
}

// path keeps the tiles of every provider and layer apart (by a short hash
// of the template), so changing the map does not show the old one from the
// cache.
func (c *tileCache) path(template, style string, z, x, y int) string {
	sum := sha256.Sum256([]byte(template))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:6]), style, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
}

func (c *tileCache) get(ctx context.Context, template, style string, z, x, y int) ([]byte, error) {
	return c.fetch(ctx, c.path(template, style, z, x, y), tileURL(template, style, z, x, y), true)
}

// fetch returns the tile kept at path, or gets it from url and keeps it.
// image tiles must be images; vector tiles are taken as they are, unpacked
// if the provider sends them packed, and a tile the provider does not have
// (open sea) is an empty one.
func (c *tileCache) fetch(ctx context.Context, path, url string, image bool) ([]byte, error) {
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
	if !image && (res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound) {
		c.store(path, nil)
		return nil, nil
	}
	if res.StatusCode != http.StatusOK || (image && !strings.HasPrefix(res.Header.Get("Content-Type"), "image/")) {
		return nil, fmt.Errorf("tile server answered %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if !image && len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if data, err = io.ReadAll(io.LimitReader(zr, 16<<20)); err != nil {
			return nil, err
		}
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
