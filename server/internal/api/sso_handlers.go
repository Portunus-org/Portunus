package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PaulichP/Portunus/server/internal/core"
	"github.com/PaulichP/Portunus/server/internal/oidc"
	"github.com/PaulichP/Portunus/server/internal/store"
)

// Sign-in through an OIDC provider, and the accounts of the panel.

const (
	ssoFlowCookie = "portunus_sso"
	ssoPath       = "/api/auth/sso"
)

func (s *Server) routesSSO(mux *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleAdmin, h)) }

	// Before signing in: what the sign-in page offers, and the two ends of
	// the trip to the provider.
	mux.HandleFunc("GET /api/auth/methods", s.handleAuthMethods)
	mux.HandleFunc("GET "+ssoPath+"/start", s.handleSSOStart)
	mux.HandleFunc("GET "+ssoPath+"/callback", s.handleSSOCallback)

	mux.Handle("GET /api/sso", admin(s.handleGetSSO))
	mux.Handle("PUT /api/sso", admin(s.handlePutSSO))
	mux.Handle("POST /api/sso/test", admin(s.handleTestSSO))

	mux.Handle("GET /api/users", admin(s.handleListUsers))
	mux.Handle("POST /api/users", admin(s.handleCreateUser))
	mux.Handle("PUT /api/users/{id}", admin(s.handleUpdateUser))
	mux.Handle("POST /api/users/{id}/reset-password", admin(s.handleResetUserPassword))
	mux.Handle("DELETE /api/users/{id}", admin(s.handleDeleteUser))
}

// handleAuthMethods tells the sign-in page whether to offer the provider.
// It says the provider's display name and nothing else.
func (s *Server) handleAuthMethods(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	if sso, err := s.core.SSO(r.Context()); err == nil && sso.Enabled {
		name := sso.Name
		if name == "" {
			name = "SSO"
		}
		out["sso"] = map[string]string{"name": name}
	}
	writeJSON(w, http.StatusOK, out)
}

// callbackURL is where the provider sends the browser back to: this very
// panel, as the browser reached it. It must be listed at the provider, so a
// forged Host header gets an attempt refused there, not sent astray.
func (s *Server) callbackURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + ssoPath + "/callback"
}

func (s *Server) setFlowCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	// nosemgrep: go.lang.security.audit.net.cookie-missing-secure.cookie-missing-secure -- Secure is decided per request by cookieSecure, as for the session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     ssoFlowCookie,
		Value:    value,
		Path:     ssoPath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.cookieSecure(r),
		// Lax, not Strict: it has to come back with the browser when the
		// provider, another site, sends it to the callback.
		SameSite: http.SameSiteLaxMode,
	})
}

// ssoBack sends the browser to the sign-in page with the reason the
// attempt ended; the page puts it into words.
func ssoBack(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/login/?sso="+url.QueryEscape(reason), http.StatusSeeOther)
}

func (s *Server) handleSSOStart(w http.ResponseWriter, r *http.Request) {
	p, _, err := s.core.SSOProvider(r.Context())
	if errors.Is(err, core.ErrSSOOff) {
		ssoBack(w, r, "off")
		return
	}
	if err != nil {
		s.log.Warn("sign-in through the provider cannot start", "err", err)
		ssoBack(w, r, "unreachable")
		return
	}
	to, flow := p.Begin(s.callbackURL(r), time.Now())
	s.setFlowCookie(w, r, s.core.SealFlow(flow), int(oidc.FlowLifetime.Seconds()))
	// Never cached: every attempt has its own state.
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, to, http.StatusFound) // nosemgrep: go.lang.security.injection.open-redirect.open-redirect -- the provider's own authorization endpoint, read from its discovery document
}

func (s *Server) handleSSOCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	c, err := r.Cookie(ssoFlowCookie)
	// One attempt, one use: the cookie goes whatever happens next.
	s.setFlowCookie(w, r, "", -1)
	if err != nil {
		ssoBack(w, r, "expired")
		return
	}
	flow, ok := s.core.OpenFlow(c.Value)
	if !ok {
		ssoBack(w, r, "expired")
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		// The person said no at the provider, or the provider refused.
		s.log.Info("sign-in refused at the provider", "error", clipText(q.Get("error"), 64))
		ssoBack(w, r, "refused")
		return
	}
	p, sso, err := s.core.SSOProvider(ctx)
	if err != nil {
		ssoBack(w, r, "off")
		return
	}
	id, err := p.Finish(ctx, flow, q.Get("state"), q.Get("code"), time.Now())
	if errors.Is(err, oidc.ErrFlow) {
		ssoBack(w, r, "expired")
		return
	}
	if err != nil {
		s.log.Warn("sign-in through the provider failed", "err", err, "ip", clientIP(r))
		s.audit(r, 0, "", "auth.sso_failed", "")
		ssoBack(w, r, "failed")
		return
	}
	role, allowed := sso.Role(id.Groups)
	if !allowed {
		s.log.Warn("sign-in refused: no group gives access", "subject", id.Subject, "username", id.Username)
		s.audit(r, 0, id.Username, "auth.sso_denied", "")
		ssoBack(w, r, "denied")
		return
	}
	u, err := s.accountFor(r, p.Issuer(), id, role)
	if errors.Is(err, errAccountDisabled) {
		s.audit(r, 0, id.Username, "auth.sso_denied", "")
		ssoBack(w, r, "denied")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	sess, err := s.auth.OpenSession(ctx, u, clientIP(r), r.UserAgent())
	if err != nil {
		ssoBack(w, r, "denied")
		return
	}
	s.setSessionCookie(w, r, sess)
	s.audit(r, u.ID, u.Username, "auth.sso_login", "")
	// Not a redirect: the browser got here from another site, and would
	// leave the session cookie (SameSite=Strict) out of a redirect that
	// continues that trip. A page that then goes to the panel by itself
	// starts a trip of its own, from this site.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/"><title>Portunus</title><a href="/">Portunus</a>`)
}

var errAccountDisabled = errors.New("account disabled")

// accountFor finds the account of a provider identity, or makes one. An
// identity is matched by issuer and subject only, and is called what the
// provider calls it, at every sign-in: the name is the provider's to give
// and to change. It is never joined to an account that exists here, even
// one of the same name, which stays somebody else.
func (s *Server) accountFor(r *http.Request, issuer string, id oidc.Identity, role store.Role) (*store.User, error) {
	ctx := r.Context()
	name := providerName(id)
	u, err := s.store.UserByIdentity(ctx, issuer, id.Subject)
	if errors.Is(err, store.ErrNotFound) {
		u = &store.User{Username: name, Role: role, OIDCIssuer: issuer, OIDCSubject: id.Subject, DisplayName: id.Name}
		switch err = s.store.CreateUser(ctx, u); {
		case err == nil:
			s.log.Info("account created for a provider identity", "username", name, "role", role)
		case errors.Is(err, store.ErrConflict):
			// Two sign-ins of the same new person at once: the other one
			// made the account.
			u, err = s.store.UserByIdentity(ctx, issuer, id.Subject)
		}
	}
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		return nil, errAccountDisabled
	}
	if err := s.store.NoteLogin(ctx, u.ID, role, id.Name, name); err != nil {
		return nil, err
	}
	u.Role, u.DisplayName, u.Username = role, id.Name, name
	return u, nil
}

// providerName is the login name the provider gives, as it gives it, less
// what cannot be shown; someone it gives no name at all is called by the
// start of their subject.
func providerName(id oidc.Identity) string {
	name := strings.TrimSpace(strings.Map(func(c rune) rune {
		if c < 0x20 || c == 0x7f {
			return -1
		}
		return c
	}, id.Username))
	if name == "" {
		name = clipText(id.Subject, 16)
	}
	return name
}

// cleanUsername keeps what a login name may hold; a provider can put
// anything into a claim.
func cleanUsername(name string) string {
	var b strings.Builder
	for _, c := range strings.TrimSpace(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-', c == '@', c == '+':
			b.WriteRune(c)
		case c == ' ':
			b.WriteByte('.')
		}
	}
	out := clipText(b.String(), 64)
	if out == "" {
		return "user"
	}
	return out
}

func clipText(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------------------------------------------------------------- settings

func (s *Server) handleGetSSO(w http.ResponseWriter, r *http.Request) {
	sso, err := s.core.SSO(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": sso, "callback": s.callbackURL(r)})
}

func (s *Server) handlePutSSO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		core.SSO
		// ClientSecret: absent keeps the stored one, "" removes it.
		ClientSecret *string `json:"clientSecret"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ClientSecret != nil && len(*req.ClientSecret) > 512 {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := s.core.SetSSO(r.Context(), req.SSO, req.ClientSecret); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "settings.sso", "", map[string]any{"enabled": req.Enabled, "issuer": req.Issuer})
	s.handleGetSSO(w, r)
}

func (s *Server) handleTestSSO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Issuer string `json:"issuer"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.core.TestSSO(r.Context(), req.Issuer); err != nil {
		// What came back from that address is not repeated to the browser:
		// the address is the administrator's to choose, and may be one of
		// this machine's own services, whose answers are nobody's to read
		// this way.
		s.log.Info("no provider at the address checked", "err", clipText(err.Error(), 300))
		writeJSON(w, http.StatusOK, map[string]any{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- accounts

type accountDTO struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName,omitempty"`
	Role        store.Role `json:"role"`
	Disabled    bool       `json:"disabled"`
	// SSO: the account signs in through the provider and has no password.
	SSO         bool       `json:"sso"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
	// Self: the account that asks.
	Self bool `json:"self"`
}

func toAccount(u *store.User, self int64) accountDTO {
	a := accountDTO{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role, Disabled: u.Disabled,
		SSO: u.OIDCSubject != "", CreatedAt: u.CreatedAt.UTC(), Self: u.ID == self}
	if !u.LastLoginAt.IsZero() {
		t := u.LastLoginAt.UTC()
		a.LastLoginAt = &t
	}
	return a
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.Users(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	self := sessionFrom(r.Context()).User.ID
	out := make([]accountDTO, 0, len(users))
	for _, u := range users {
		out = append(out, toAccount(u, self))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string     `json:"username"`
		Role     store.Role `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Username)
	if name == "" || len(name) > 64 || name != cleanUsername(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "username", "code": "bad_username"})
		return
	}
	if !req.Role.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "role", "code": "unsupported"})
		return
	}
	u, password, err := s.auth.CreateLocalUser(r.Context(), name, req.Role)
	if errors.Is(err, store.ErrConflict) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "username", "code": "username_taken"})
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditAction(r, "user.create", u.Username, map[string]any{"role": u.Role})
	// The password is shown once, here; the account replaces it at its
	// first sign-in.
	writeJSON(w, http.StatusCreated, map[string]any{"user": toAccount(u, sessionFrom(r.Context()).User.ID), "password": password})
}

// lastAdmin reports whether taking u's administrator rights away (by a
// role, by disabling, by deleting) would leave the panel without an
// administrator who signs in with a password. One such account is always
// kept: it is the way in when the provider is down, misconfigured, or has
// moved everyone out of the administrators' group.
func (s *Server) lastAdmin(r *http.Request, u *store.User) (bool, error) {
	if u.Role != store.RoleAdmin || u.Disabled || u.OIDCSubject != "" {
		return false, nil
	}
	n, err := s.store.ActiveAdmins(r.Context())
	return n <= 1, err
}

func refuse(w http.ResponseWriter, detail string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid", "field": "user", "code": detail})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req struct {
		Role     store.Role `json:"role"`
		Disabled bool       `json:"disabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.Role.Valid() {
		refuse(w, "unsupported")
		return
	}
	u, err := s.store.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	loses := req.Disabled || req.Role != store.RoleAdmin
	if loses {
		// Nobody takes their own rights away by a slip of the hand, and the
		// panel is never left without an administrator.
		if u.ID == sessionFrom(r.Context()).User.ID {
			refuse(w, "own_account")
			return
		}
		if last, err := s.lastAdmin(r, u); err != nil {
			s.internalError(w, r, err)
			return
		} else if last {
			refuse(w, "last_admin")
			return
		}
	}
	if err := s.store.SetUserAccess(r.Context(), id, req.Role, req.Disabled); err != nil {
		s.internalError(w, r, err)
		return
	}
	if req.Disabled {
		s.store.DeleteUserSessions(r.Context(), id, nil)
	}
	s.auditAction(r, "user.update", u.Username, map[string]any{"role": req.Role, "disabled": req.Disabled})
	u.Role, u.Disabled = req.Role, req.Disabled
	writeJSON(w, http.StatusOK, toAccount(u, sessionFrom(r.Context()).User.ID))
}

func (s *Server) handleResetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	u, err := s.store.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if u.OIDCSubject != "" {
		// Its password lives at the provider.
		refuse(w, "sso_account")
		return
	}
	if u.ID == sessionFrom(r.Context()).User.ID {
		// One's own password is changed on the account page, knowing the
		// current one.
		refuse(w, "own_account")
		return
	}
	password, err := s.auth.ResetPassword(r.Context(), u.Username)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditAction(r, "user.reset_password", u.Username, nil)
	writeJSON(w, http.StatusOK, map[string]string{"password": password})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	u, err := s.store.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if u.ID == sessionFrom(r.Context()).User.ID {
		refuse(w, "own_account")
		return
	}
	if last, err := s.lastAdmin(r, u); err != nil {
		s.internalError(w, r, err)
		return
	} else if last {
		refuse(w, "last_admin")
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditAction(r, "user.delete", u.Username, nil)
	w.WriteHeader(http.StatusNoContent)
}
