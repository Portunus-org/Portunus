// Package config loads runtime configuration from environment variables.
//
// Portunus is configured exclusively through the environment so that a bare
// `docker compose up` works with zero files to edit. Every variable has a safe
// default; see docs/DEPLOY.md for the full reference.
package config

import (
	"fmt"
	"log/slog"
	"net"
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
	// DataDir holds the SQLite database and, in later stages, keys,
	// rule-set cache and ACME certificates.
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
}

const (
	DefaultListen = "127.0.0.1:2053"
)

func Load() (*Config, error) {
	c := &Config{
		DataDir:       env("PORTUNUS_DATA_DIR", "./data"),
		Listen:        env("PORTUNUS_LISTEN", DefaultListen),
		LogFormat:     strings.ToLower(env("PORTUNUS_LOG_FORMAT", "text")),
		CookieSecure:  CookieSecure(strings.ToLower(env("PORTUNUS_COOKIE_SECURE", string(CookieSecureAuto)))),
		SessionTTL:    24 * time.Hour,
		SessionMaxAge: 7 * 24 * time.Hour,
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
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return nil, fmt.Errorf("PORTUNUS_LISTEN: %w", err)
	}

	dir, err := filepath.Abs(c.DataDir)
	if err != nil {
		return nil, fmt.Errorf("PORTUNUS_DATA_DIR: %w", err)
	}
	c.DataDir = dir
	return c, nil
}

// DBPath is the location of the SQLite database file.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "portunus.db") }

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
