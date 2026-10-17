// Package config loads runtime configuration from environment variables.
//
// Portunus is configured exclusively through the environment so that a bare
// `docker compose up` works with zero files to edit. Every variable has a safe
// default; see docs/DEPLOY.md for the full reference.
package config

import (
	"fmt"
	"github.com/Portunus-org/Portunus/server/internal/selfupdate"
	"github.com/Portunus-org/Portunus/server/internal/version"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CookieSecure controls the Secure attribute of the session cookie.
type CookieSecure string

const (
	// CookieSecureAuto sets Secure when the request arrived over TLS.
	CookieSecureAuto   CookieSecure = "auto"
	CookieSecureAlways CookieSecure = "always"
	CookieSecureNever  CookieSecure = "never"
)

type Config struct {
	// DataDir holds the SQLite database, the master key, the generated
	// sing-box config and (later) rule-set cache and ACME certificates.
	DataDir string
	// Listen is the panel's HTTP listen address. Defaults to loopback only:
	// the panel is reached through an SSH tunnel unless explicitly exposed.
	Listen string

	LogLevel  slog.Level
	LogFormat string // "json" | "text"

	CookieSecure CookieSecure
	// SessionTTL is the idle lifetime of a session; SessionMaxAge caps its
	// absolute lifetime regardless of activity.
	SessionTTL    time.Duration
	SessionMaxAge time.Duration

	// SecretKeyFile holds the 32-byte master key that encrypts key material
	// in the database. Empty: generated on first start in DataDir.
	SecretKeyFile string

	// SingBoxBin is the sing-box executable to supervise.
	SingBoxBin string
	// SingBoxMemory is the Go soft memory limit (GOMEMLIMIT) given to
	// sing-box. The panel's own limit is small; the proxy needs room for
	// its connections, or its garbage collector runs without pause once
	// hundreds of them are open.
	SingBoxMemory string
	// SingBoxCtl is the loopback address of sing-box's control API, which
	// the panel uses to measure exits.
	SingBoxCtl string
	// NodeImage is the container image the join command for new nodes
	// uses: by default the published node image of this very version. A
	// name without a registry is a local build, and the panel then also
	// shows how to copy it to the server.
	NodeImage string
	// UpdateCheck is the image repository whose tags tell the panel that a
	// newer release is out; "off" turns the check off.
	UpdateCheck string
	// UpdateDir is the directory shared with the updater, the container
	// that installs updates (internal/selfupdate).
	UpdateDir string
	// GeoLookup is the service that tells where a server is, for the map of
	// nodes: the panel requests GeoLookup + IP for its own public address
	// and for each node's. "off" disables it; servers can still be placed
	// on the map by hand.
	GeoLookup string
	// MapTiles, when set, replaces the panel's own drawn map of nodes with
	// map images from a tile server. It is where the panel fetches them:
	// a URL template with {z}, {x}, {y} and optionally {style} ("light" or
	// "dark") for a provider that has both, or "esri" for Esri's gray canvas. The
	// panel caches what it fetches and the browser gets tiles from the panel
	// only. "off" (the default) keeps the drawn map.
	MapTiles string
	// MapDetail is where the drawn map gets what it shows when zoomed in
	// past the scale of countries (coasts, borders of regions, names of
	// towns): TileJSON addresses of vector tile sets, separated by commas,
	// in the OpenMapTiles or the Shortbread layout. The first one that the
	// server can reach is used (see MapDetailSources); the panel fetches
	// and caches the tiles itself. "off" keeps the map at the scale of
	// countries.
	MapDetail string
	// MapLabels is an optional second layer drawn over the tiles: place
	// names, for a provider that keeps them apart from the base map. Same
	// template format; "off" or empty for none.
	MapLabels string
	// MapCredit is the attribution shown on the map.
	MapCredit string
	// SingBoxAPI is the loopback address of sing-box's stats API (v2ray_api),
	// used for per-client traffic accounting.
	SingBoxAPI string

	// ACMEDirectory overrides the ACME server (default: Let's Encrypt).
	// Used for tests against Pebble, or for another CA.
	ACMEDirectory string

	// SubListen, when set, serves client subscriptions (/sub/…) on a separate
	// listener that may be public. Nothing else is reachable on it.
	SubListen string
}

const (
	DefaultListen = "127.0.0.1:2053"
	// By default the map of nodes is drawn by the panel itself from country
	// outlines it carries, and nothing is fetched. PORTUNUS_MAP_TILES=esri
	// switches to Esri's gray canvas instead: map images with more detail,
	// in a light and a dark version, with place names as a separate layer.
	EsriMapTiles  = "https://services.arcgisonline.com/ArcGIS/rest/services/Canvas/World_{style}_Gray_Base/MapServer/tile/{z}/{y}/{x}"
	EsriMapLabels = "https://services.arcgisonline.com/ArcGIS/rest/services/Canvas/World_{style}_Gray_Reference/MapServer/tile/{z}/{y}/{x}"
	// PanelImageRepository is where releases publish the panel's image: the
	// panel reads its tags to learn that a newer version is out.
	PanelImageRepository = "ghcr.io/portunus-org/portunus"
	// NodeImageRepository is where releases publish the node image.
	NodeImageRepository = "ghcr.io/portunus-org/portunus-node"
)

// DefaultNodeImage is the published node image that goes with this build:
// the same release, or the newest one for a build that is not a release.
func DefaultNodeImage() string {
	if strings.HasPrefix(version.Version, "v") {
		return NodeImageRepository + ":" + version.Version
	}
	return NodeImageRepository + ":latest"
}

func Load() (*Config, error) {
	c := &Config{
		DataDir:       env("PORTUNUS_DATA_DIR", "./data"),
		Listen:        env("PORTUNUS_LISTEN", DefaultListen),
		LogFormat:     strings.ToLower(env("PORTUNUS_LOG_FORMAT", "text")),
		CookieSecure:  CookieSecure(strings.ToLower(env("PORTUNUS_COOKIE_SECURE", string(CookieSecureAuto)))),
		SessionTTL:    24 * time.Hour,
		SessionMaxAge: 7 * 24 * time.Hour,
		SecretKeyFile: env("PORTUNUS_SECRET_KEY_FILE", ""),
		SingBoxBin:    env("PORTUNUS_SINGBOX_BIN", "sing-box"),
		SingBoxAPI:    env("PORTUNUS_SINGBOX_API", "127.0.0.1:2054"),
		SingBoxCtl:    env("PORTUNUS_SINGBOX_CTL", "127.0.0.1:2055"),
		SingBoxMemory: env("PORTUNUS_SINGBOX_MEMLIMIT", "256MiB"),
		NodeImage:     env("PORTUNUS_NODE_IMAGE", DefaultNodeImage()),
		GeoLookup:     env("PORTUNUS_GEO_LOOKUP", "auto"),
		UpdateCheck:   env("PORTUNUS_UPDATE_CHECK", PanelImageRepository),
		UpdateDir:     env("PORTUNUS_UPDATE_DIR", selfupdate.DefaultDir),
		MapTiles:      env("PORTUNUS_MAP_TILES", "off"),
		MapDetail:     env("PORTUNUS_MAP_DETAIL", DefaultMapDetail),
		MapLabels:     os.Getenv("PORTUNUS_MAP_LABELS"),
		MapCredit:     os.Getenv("PORTUNUS_MAP_CREDIT"),
		SubListen:     env("PORTUNUS_SUB_LISTEN", ""),
		ACMEDirectory: env("PORTUNUS_ACME_DIRECTORY", ""),
	}

	if err := c.LogLevel.UnmarshalText([]byte(env("PORTUNUS_LOG_LEVEL", "info"))); err != nil {
		return nil, fmt.Errorf("PORTUNUS_LOG_LEVEL: %w", err)
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return nil, fmt.Errorf("PORTUNUS_LOG_FORMAT: must be json or text, got %q", c.LogFormat)
	}
	switch c.CookieSecure {
	case CookieSecureAuto, CookieSecureAlways, CookieSecureNever:
	default:
		return nil, fmt.Errorf("PORTUNUS_COOKIE_SECURE: must be auto, always or never, got %q", c.CookieSecure)
	}
	if strings.EqualFold(c.MapDetail, "off") {
		c.MapDetail = ""
	}
	for _, src := range c.MapDetailSources() {
		if u, err := url.Parse(src); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("PORTUNUS_MAP_DETAIL: must be addresses of TileJSON documents, separated by commas, or \"off\"; got %q", src)
		}
	}
	// "esri" is short for Esri's gray canvas, which comes with its own
	// layer of names and its own credit; another provider brings neither
	// unless told.
	if strings.EqualFold(c.MapTiles, "esri") {
		c.MapTiles = EsriMapTiles
	}
	if c.MapTiles == EsriMapTiles {
		if c.MapLabels == "" {
			c.MapLabels = EsriMapLabels
		}
		if c.MapCredit == "" {
			c.MapCredit = "Esri, HERE, Garmin, © OpenStreetMap"
		}
	}
	if strings.EqualFold(c.MapLabels, "off") || strings.EqualFold(c.MapTiles, "off") {
		c.MapLabels = ""
	} else if c.MapLabels != "" {
		if u, err := url.Parse(c.MapLabels); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || !strings.Contains(c.MapLabels, "{z}") {
			return nil, fmt.Errorf("PORTUNUS_MAP_LABELS: must be a tile URL template or \"off\", got %q", c.MapLabels)
		}
	}
	if strings.EqualFold(c.MapTiles, "off") {
		c.MapTiles = ""
	} else if u, err := url.Parse(c.MapTiles); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || !strings.Contains(c.MapTiles, "{z}") {
		return nil, fmt.Errorf("PORTUNUS_MAP_TILES: must be a tile URL template or \"off\", got %q", c.MapTiles)
	}
	if strings.EqualFold(c.GeoLookup, "off") {
		c.GeoLookup = ""
	} else if strings.EqualFold(c.GeoLookup, "auto") {
		c.GeoLookup = "auto"
	} else if u, err := url.Parse(c.GeoLookup); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("PORTUNUS_GEO_LOOKUP: must be a URL or \"off\", got %q", c.GeoLookup)
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return nil, fmt.Errorf("PORTUNUS_LISTEN: %w", err)
	}
	if host, _, err := net.SplitHostPort(c.SingBoxCtl); err != nil {
		return nil, fmt.Errorf("PORTUNUS_SINGBOX_CTL: %w", err)
	} else if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf("PORTUNUS_SINGBOX_CTL: must be a loopback address, got %q", host)
	}
	if host, _, err := net.SplitHostPort(c.SingBoxAPI); err != nil {
		return nil, fmt.Errorf("PORTUNUS_SINGBOX_API: %w", err)
	} else if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		// The stats API is unauthenticated; it must never leave the host.
		return nil, fmt.Errorf("PORTUNUS_SINGBOX_API: must be a loopback IP address, got %q", host)
	}
	if c.ACMEDirectory != "" && !strings.HasPrefix(c.ACMEDirectory, "https://") {
		return nil, fmt.Errorf("PORTUNUS_ACME_DIRECTORY: must be an https:// URL")
	}
	if c.SubListen != "" {
		if _, _, err := net.SplitHostPort(c.SubListen); err != nil {
			return nil, fmt.Errorf("PORTUNUS_SUB_LISTEN: %w", err)
		}
	}

	dir, err := filepath.Abs(c.DataDir)
	if err != nil {
		return nil, fmt.Errorf("PORTUNUS_DATA_DIR: %w", err)
	}
	c.DataDir = dir
	return c, nil
}

// DBPath is the location of the SQLite database file.
// InitialLoginPath holds the first password of the admin account until it
// is replaced (see cmd/portunus: showInitialLogin).
func (c *Config) InitialLoginPath() string { return filepath.Join(c.DataDir, "initial-password") }

func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "portunus.db") }

// BeforeUpdatePath is where the copy of the database made just before an
// update is kept, for the previous version to return to (db.PutBack).
func (c *Config) BeforeUpdatePath() string { return filepath.Join(c.DataDir, "before-update.db") }

// MasterKeyPath is where the database encryption key is read from.
func (c *Config) MasterKeyPath() string {
	if c.SecretKeyFile != "" {
		return c.SecretKeyFile
	}
	return filepath.Join(c.DataDir, "master.key")
}

// SingBoxDir holds the generated sing-box config and its runtime files.
func (c *Config) SingBoxDir() string { return filepath.Join(c.DataDir, "sing-box") }

// ListenIsLoopback reports whether the panel is only reachable locally.
func (c *Config) ListenIsLoopback() bool {
	host, _, _ := net.SplitHostPort(c.Listen)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// DefaultMapDetail are the tile sets the detailed map is taken from, in the
// order they are tried. All three are free to use and made from
// OpenStreetMap. They live with different hosts on purpose, so that one
// out of reach from a network leaves the others.
const DefaultMapDetail = "https://tiles.openfreemap.org/planet," +
	"https://tiles.versatiles.org/tiles/osm/tiles.json," +
	"https://vector.openstreetmap.org/shortbread_v1/tilejson.json"

// MapDetailSources are the addresses in MapDetail, in the order to try them.
func (c *Config) MapDetailSources() []string {
	var out []string
	for _, src := range strings.FieldsFunc(c.MapDetail, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, src)
	}
	return out
}
