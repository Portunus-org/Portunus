package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PaulichP/Portunus/server/internal/auth"
	"github.com/PaulichP/Portunus/server/internal/config"
	"github.com/PaulichP/Portunus/server/internal/store"
	"github.com/PaulichP/Portunus/server/internal/sysinfo"
	"github.com/PaulichP/Portunus/server/internal/version"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		s.log.Error("health check: database unreachable", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type userDTO struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	Role               store.Role `json:"role"`
	MustChangePassword bool       `json:"mustChangePassword"`
}

type sessionDTO struct {
	User      userDTO   `json:"user"`
	CSRFToken string    `json:"csrfToken"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func toSessionDTO(sess *auth.Session) sessionDTO {
	u := sess.User
	return sessionDTO{
		User: userDTO{
			ID:                 u.ID,
			Username:           u.Username,
			Role:               u.Role,
			MustChangePassword: u.MustChangePassword,
		},
		CSRFToken: sess.CSRFToken,
		ExpiresAt: sess.ExpiresAt.UTC(),
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || len(req.Username) > 64 || req.Password == "" || len(req.Password) > auth.MaxPasswordLength*4 {
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	}

	ip := clientIP(r)
	sess, err := s.auth.Login(r.Context(), req.Username, req.Password, ip, r.UserAgent())
	var rl *auth.RateLimitError
	switch {
	case errors.As(err, &rl):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(rl.RetryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.log.Warn("failed login", "username", req.Username, "ip", ip)
		s.audit(r, 0, req.Username, "auth.login_failed", "")
		writeError(w, http.StatusUnauthorized, "invalid_credentials")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}

	s.setSessionCookie(w, r, sess)
	s.audit(r, sess.User.ID, sess.User.Username, "auth.login", "")
	writeJSON(w, http.StatusOK, toSessionDTO(sess))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	if err := s.auth.Logout(r.Context(), sess); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.clearSessionCookie(w, r)
	s.audit(r, sess.User.ID, sess.User.Username, "auth.logout", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toSessionDTO(sessionFrom(r.Context())))
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	sess := sessionFrom(r.Context())
	switch err := s.auth.ChangePassword(r.Context(), sess, req.CurrentPassword, req.NewPassword); {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusBadRequest, "current_password_invalid")
		return
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, "password_too_weak")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	s.audit(r, sess.User.ID, sess.User.Username, "auth.password_changed", "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	var dbSize int64
	for _, suffix := range []string{"", "-wal"} {
		if st, err := os.Stat(s.cfg.DBPath() + suffix); err == nil {
			dbSize += st.Size()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       version.Version,
		"commit":        version.Commit,
		"goVersion":     version.GoVersion(),
		"startedAt":     s.started.UTC(),
		"uptimeSeconds": int64(time.Since(s.started).Seconds()),
		"memory":        sysinfo.ReadMemory(),
		"database":      map[string]any{"sizeBytes": dbSize},
		"listen":        s.cfg.Listen,
		"listenLocal":   s.cfg.ListenIsLoopback(),
	})
}

func (s *Server) cookieSecure(r *http.Request) bool {
	switch s.cfg.CookieSecure {
	case config.CookieSecureAlways:
		return true
	case config.CookieSecureNever:
		return false
	}
	// Auto: Secure whenever the browser reached the panel over HTTPS, either
	// directly or through a TLS-terminating reverse proxy. A forged
	// X-Forwarded-Proto can only make the cookie stricter. Plain HTTP (an SSH
	// tunnel to localhost) stays without it: not every browser keeps Secure
	// cookies on http://localhost.
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure is decided per request by cookieSecure (TLS or a TLS-terminating proxy); a plain-HTTP deployment needs it off
	http.SetCookie(w, &http.Cookie{
		Name:  sessionCookie,
		Value: sess.Token,
		Path:  "/",
		// The session slides server-side; the cookie just has to outlive it.
		Expires:  time.Now().Add(s.cfg.SessionMaxAge),
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure is decided per request by cookieSecure (TLS or a TLS-terminating proxy); a plain-HTTP deployment needs it off
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) audit(r *http.Request, actorID int64, actor, action, target string) {
	if err := s.store.Audit(r.Context(), store.AuditEntry{
		ActorID: actorID, Actor: actor, Action: action, Target: target, IP: clientIP(r),
	}); err != nil {
		s.log.Error("write audit log", "action", action, "err", err)
	}
}
