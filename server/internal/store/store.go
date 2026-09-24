// Package store is the data-access layer. Everything above it talks to the
// database only through these methods, so the SQLite backend can be swapped
// without touching business logic.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Store {
	return &Store{db: db, now: time.Now}
}

// Ping verifies the database is reachable; used by the health endpoint.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	}
	return false
}

type User struct {
	ID                 int64
	Username           string
	PasswordHash       string // empty when the account has no local password
	Role               Role
	MustChangePassword bool
	Disabled           bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

const userColumns = `id, username, COALESCE(password_hash, ''), role, must_change_password, disabled, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var (
		u                User
		created, updated int64
	)
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.MustChangePassword, &u.Disabled, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	u.UpdatedAt = time.Unix(updated, 0)
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	now := s.now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, must_change_password, disabled, created_at, updated_at)
		 VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?)`,
		u.Username, u.PasswordHash, u.Role, u.MustChangePassword, u.Disabled, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	u.ID, err = res.LastInsertId()
	u.CreatedAt = time.Unix(now, 0)
	u.UpdatedAt = u.CreatedAt
	return err
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

func (s *Store) UserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = ?`, username))
}

func (s *Store) SetPassword(ctx context.Context, userID int64, hash string, mustChange bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_password = ?, updated_at = ? WHERE id = ?`,
		hash, mustChange, s.now().Unix(), userID)
	if err != nil {
		return err
	}
	return expectOne(res)
}

type Session struct {
	IDHash     []byte
	UserID     int64
	CSRFToken  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IP         string
	UserAgent  string
}

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id_hash, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.IDHash, sess.UserID, sess.CSRFToken, sess.CreatedAt.Unix(), sess.LastSeenAt.Unix(),
		sess.ExpiresAt.Unix(), sess.IP, truncate(sess.UserAgent, 256))
	return err
}

// SessionByHash returns an unexpired session.
func (s *Store) SessionByHash(ctx context.Context, idHash []byte) (*Session, error) {
	var (
		sess                   Session
		created, seen, expires int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id_hash, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent
		 FROM sessions WHERE id_hash = ? AND expires_at > ?`, idHash, s.now().Unix()).
		Scan(&sess.IDHash, &sess.UserID, &sess.CSRFToken, &created, &seen, &expires, &sess.IP, &sess.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt = time.Unix(created, 0)
	sess.LastSeenAt = time.Unix(seen, 0)
	sess.ExpiresAt = time.Unix(expires, 0)
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, idHash []byte, seen, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id_hash = ?`,
		seen.Unix(), expires.Unix(), idHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, idHash []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, idHash)
	return err
}

// DeleteUserSessions revokes every session of a user except keep (may be nil).
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64, keep []byte) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND id_hash IS NOT ?`, userID, keep)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type AuditEntry struct {
	ActorID int64 // 0 when the actor is not a known user
	Actor   string
	Action  string
	Target  string
	IP      string
	Details map[string]any
}

func (s *Store) Audit(ctx context.Context, e AuditEntry) error {
	details := []byte("{}")
	if len(e.Details) > 0 {
		var err error
		if details, err = json.Marshal(e.Details); err != nil {
			return err
		}
	}
	var actorID any
	if e.ActorID != 0 {
		actorID = e.ActorID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (at, actor_id, actor, action, target, ip, details) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.now().Unix(), actorID, e.Actor, e.Action, e.Target, e.IP, string(details))
	return err
}

func expectOne(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
