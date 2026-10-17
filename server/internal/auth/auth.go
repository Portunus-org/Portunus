// Package auth implements local accounts and server-side sessions.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRateLimited        = errors.New("too many attempts")
	ErrNoSession          = errors.New("no valid session")
)

// RateLimitError carries how long the client must wait.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

type Service struct {
	store   *store.Store
	log     *slog.Logger
	limiter *LoginLimiter
	ttl     time.Duration
	maxAge  time.Duration
	now     func() time.Time

	// dummyHash is verified against when the username does not exist, so a
	// failed login takes the same time either way (no user enumeration).
	dummyHash string
}

func NewService(st *store.Store, log *slog.Logger, ttl, maxAge time.Duration) (*Service, error) {
	dummy, err := HashPassword(GeneratePassword())
	if err != nil {
		return nil, err
	}
	return &Service{
		store:     st,
		log:       log,
		limiter:   NewLoginLimiter(),
		ttl:       ttl,
		maxAge:    maxAge,
		now:       time.Now,
		dummyHash: dummy,
	}, nil
}

// Session is an authenticated session as seen by request handlers.
type Session struct {
	Token     string // raw cookie value; only set right after login
	IDHash    []byte
	User      *store.User
	CSRFToken string
	ExpiresAt time.Time
}

// Login verifies credentials and opens a new session.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (*Session, error) {
	key := strings.ToLower(strings.TrimSpace(username))
	if ok, wait := s.limiter.Allow(ip, key); !ok {
		return nil, &RateLimitError{RetryAfter: wait}
	}

	u, err := s.store.UserByUsername(ctx, key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		VerifyPassword(s.dummyHash, password)
		s.limiter.Failure(key)
		return nil, ErrInvalidCredentials
	case err != nil:
		return nil, err
	}

	hash := u.PasswordHash
	if hash == "" {
		hash = s.dummyHash // OIDC-only account: burn the same time, then fail
	}
	ok, err := VerifyPassword(hash, password)
	if err != nil {
		return nil, fmt.Errorf("verify password for user %d: %w", u.ID, err)
	}
	if !ok || u.PasswordHash == "" || u.Disabled {
		s.limiter.Failure(key)
		return nil, ErrInvalidCredentials
	}
	s.limiter.Success(key)
	if err := s.store.NoteLogin(ctx, u.ID, u.Role, u.DisplayName, ""); err != nil {
		s.log.Warn("note login", "err", err)
	}
	return s.openSession(ctx, u, ip, userAgent)
}

// OpenSession signs in an account whose identity was established some other
// way than by its password: by an OIDC provider.
func (s *Service) OpenSession(ctx context.Context, u *store.User, ip, userAgent string) (*Session, error) {
	if u.Disabled {
		return nil, ErrInvalidCredentials
	}
	return s.openSession(ctx, u, ip, userAgent)
}

// CreateLocalUser adds an account with a password of its own. The password
// is generated and returned once; the account has to replace it at its
// first sign-in.
func (s *Service) CreateLocalUser(ctx context.Context, username string, role store.Role) (*store.User, string, error) {
	pw := GeneratePassword()
	hash, err := HashPassword(pw)
	if err != nil {
		return nil, "", err
	}
	u := &store.User{Username: username, PasswordHash: hash, Role: role, MustChangePassword: true}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, "", err
	}
	return u, pw, nil
}

func (s *Service) openSession(ctx context.Context, u *store.User, ip, userAgent string) (*Session, error) {
	token := randomToken(32)
	csrf := randomToken(32)
	now := s.now()
	sess := &store.Session{
		IDHash:     hashToken(token),
		UserID:     u.ID,
		CSRFToken:  csrf,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.ttl),
		IP:         ip,
		UserAgent:  userAgent,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return &Session{Token: token, IDHash: sess.IDHash, User: u, CSRFToken: csrf, ExpiresAt: sess.ExpiresAt}, nil
}

// Authenticate resolves a cookie token to a live session, sliding its idle
// expiry forward (bounded by the absolute max age).
func (s *Service) Authenticate(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		return nil, ErrNoSession
	}
	idHash := hashToken(token)
	sess, err := s.store.SessionByHash(ctx, idHash)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}

	now := s.now()
	hardLimit := sess.CreatedAt.Add(s.maxAge)
	if !now.Before(hardLimit) {
		s.store.DeleteSession(ctx, idHash)
		return nil, ErrNoSession
	}

	u, err := s.store.UserByID(ctx, sess.UserID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if u.Disabled {
		s.store.DeleteSession(ctx, idHash)
		return nil, ErrNoSession
	}

	// Write at most once a minute to keep the hot path read-only.
	expires := sess.ExpiresAt
	if now.Sub(sess.LastSeenAt) > time.Minute {
		expires = now.Add(s.ttl)
		if expires.After(hardLimit) {
			expires = hardLimit
		}
		if err := s.store.TouchSession(ctx, idHash, now, expires); err != nil {
			s.log.Warn("touch session", "err", err)
		}
	}
	return &Session{IDHash: idHash, User: u, CSRFToken: sess.CSRFToken, ExpiresAt: expires}, nil
}

func (s *Service) Logout(ctx context.Context, sess *Session) error {
	return s.store.DeleteSession(ctx, sess.IDHash)
}

// ChangePassword verifies the current password, sets a new one and revokes
// every other session of the user.
func (s *Service) ChangePassword(ctx context.Context, sess *Session, current, next string) error {
	u := sess.User
	if u.PasswordHash == "" {
		return ErrInvalidCredentials
	}
	ok, err := VerifyPassword(u.PasswordHash, current)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.store.SetPassword(ctx, u.ID, hash, false); err != nil {
		return err
	}
	return s.store.DeleteUserSessions(ctx, u.ID, sess.IDHash)
}

// ResetPassword assigns a fresh random password (forcing a change on next
// login) and revokes all sessions. Used by the `reset-password` command.
func (s *Service) ResetPassword(ctx context.Context, username string) (string, error) {
	u, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return "", err
	}
	pw := GeneratePassword()
	hash, err := HashPassword(pw)
	if err != nil {
		return "", err
	}
	if err := s.store.SetPassword(ctx, u.ID, hash, true); err != nil {
		return "", err
	}
	return pw, s.store.DeleteUserSessions(ctx, u.ID, nil)
}

// Bootstrap creates the initial admin account on an empty database and
// returns its generated password; it returns "" when users already exist.
func (s *Service) Bootstrap(ctx context.Context) (username, password string, err error) {
	n, err := s.store.CountUsers(ctx)
	if err != nil || n > 0 {
		return "", "", err
	}
	password = GeneratePassword()
	hash, err := HashPassword(password)
	if err != nil {
		return "", "", err
	}
	u := &store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, MustChangePassword: true}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return "", "", err
	}
	return u.Username, password, nil
}

// RunJanitor periodically purges expired sessions and stale limiter state.
// AuditKept is how long entries of the audit log are kept.
const AuditKept = 365 * 24 * time.Hour

func (s *Service) RunJanitor(ctx context.Context) {
	t := time.NewTicker(15 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.limiter.GC()
			if n, err := s.store.DeleteExpiredSessions(ctx); err != nil {
				s.log.Warn("purge expired sessions", "err", err)
			} else if n > 0 {
				s.log.Debug("purged expired sessions", "count", n)
			}
			// The audit log keeps a year.
			if n, err := s.store.PruneAudit(ctx, time.Now().Add(-AuditKept)); err != nil {
				s.log.Warn("prune audit log", "err", err)
			} else if n > 0 {
				s.log.Debug("pruned audit log", "count", n)
			}
		}
	}
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
