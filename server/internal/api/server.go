// Package api wires the HTTP surface of the panel: JSON API, health check
// and the embedded frontend.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/PaulichP/Portunus/server/internal/auth"
	"github.com/PaulichP/Portunus/server/internal/config"
	"github.com/PaulichP/Portunus/server/internal/store"
)

const (
	sessionCookie = "portunus_session"
	csrfHeader    = "X-Portunus-CSRF"
	maxBodyBytes  = 1 << 20
)

type Server struct {
	cfg     *config.Config
	log     *slog.Logger
	store   *store.Store
	auth    *auth.Service
	started time.Time
}

func New(cfg *config.Config, log *slog.Logger, st *store.Store, as *auth.Service) *Server {
	return &Server{cfg: cfg, log: log, store: st, auth: as, started: time.Now()}
}

// Handler returns the root handler; ui serves every non-API path.
func (s *Server) Handler(ui http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.Handle("POST /api/auth/logout", s.requireSession(s.handleLogout))
	mux.Handle("GET /api/auth/me", s.requireSession(s.handleMe))
	mux.Handle("POST /api/auth/password", s.requireSession(s.handleChangePassword))

	mux.Handle("GET /api/system", s.requireSession(s.handleSystem))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})
	mux.Handle("/", ui)

	return s.recoverer(s.logRequests(securityHeaders(mux)))
}

type ctxKey struct{}

func sessionFrom(ctx context.Context) *auth.Session {
	sess, _ := ctx.Value(ctxKey{}).(*auth.Session)
	return sess
}

// requireSession authenticates the request and, for state-changing methods,
// enforces the CSRF token bound to the session.
func (s *Server) requireSession(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		sess, err := s.auth.Authenticate(r.Context(), c.Value)
		if errors.Is(err, auth.ErrNoSession) {
			s.clearSessionCookie(w, r)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		// A generated password was printed to the logs; until it is replaced,
		// the session may only change it (or leave).
		if sess.User.MustChangePassword && !passwordChangeAllowed[r.URL.Path] {
			writeError(w, http.StatusForbidden, "password_change_required")
			return
		}
		if !isSafeMethod(r.Method) {
			got := r.Header.Get(csrfHeader)
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
				writeError(w, http.StatusForbidden, "csrf_token_invalid")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	})
}

var passwordChangeAllowed = map[string]bool{
	"/api/auth/me":       true,
	"/api/auth/password": true,
	"/api/auth/logout":   true,
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cache-Control", "no-store")
		}

		// Reject cross-site state-changing requests outright. Browsers always
		// send Origin on such requests; non-browser clients may omit it.
		if !isSafeMethod(r.Method) {
			if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, r) {
				writeError(w, http.StatusForbidden, "cross_origin_forbidden")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// Static asset hits are noise at info level.
		level := slog.LevelInfo
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			level = slog.LevelDebug
		}
		if rec.status >= 500 {
			level = slog.LevelError
		}
		s.log.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).Round(time.Microsecond),
			"ip", clientIP(r))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic in handler", "panic", v, "path", r.URL.Path, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal_error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("request failed", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal_error")
}

// clientIP is the TCP peer address. X-Forwarded-For is deliberately ignored:
// the panel binds to loopback by default and trusted-proxy support will be
// added explicitly when a reverse-proxy deployment mode exists.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

// decodeJSON reads a size-limited JSON body with unknown fields rejected.
// Requiring application/json also means a cross-site HTML form can never
// reach these endpoints without a CORS preflight.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "json_required")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}
