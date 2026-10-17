// Package store is the data-access layer. Everything above it talks to the
// database only through these methods, so the SQLite backend can be swapped
// without touching business logic.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	// OIDCIssuer and OIDCSubject identify an account that signs in through
	// an OIDC provider; both empty for a local one.
	OIDCIssuer  string
	OIDCSubject string
	// DisplayName is the person's name as the provider gives it.
	DisplayName string
	// LastLoginAt is zero for an account that never signed in.
	LastLoginAt time.Time
}

const userColumns = `id, username, COALESCE(password_hash, ''), role, must_change_password, disabled, created_at, updated_at,
	COALESCE(oidc_issuer, ''), COALESCE(oidc_subject, ''), display_name, COALESCE(last_login_at, 0), oidc_username`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var (
		u                       User
		created, updated, login int64
		providerName            string
	)
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.MustChangePassword, &u.Disabled, &created, &updated,
		&u.OIDCIssuer, &u.OIDCSubject, &u.DisplayName, &login, &providerName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.CreatedAt = time.Unix(created, 0)
	u.UpdatedAt = time.Unix(updated, 0)
	if login > 0 {
		u.LastLoginAt = time.Unix(login, 0)
	}
	// An account of a provider is called what the provider calls it; what
	// the username column holds for it is a handle (see CreateUser).
	if u.OIDCSubject != "" && providerName != "" {
		u.Username = providerName
	}
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ssoHandle is what the unique username column holds for an account of a
// provider: something no person can type as a login name (a local name has
// no colon), derived from the identity, so the name the provider uses can
// be anything, including a name taken here.
func ssoHandle(issuer, subject string) string {
	sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return "sso:" + hex.EncodeToString(sum[:12])
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	now := s.now().Unix()
	name, providerName := u.Username, ""
	if u.OIDCSubject != "" {
		name, providerName = ssoHandle(u.OIDCIssuer, u.OIDCSubject), u.Username
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role, must_change_password, disabled, created_at, updated_at,
		                    oidc_issuer, oidc_subject, display_name, oidc_username)
		 VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		name, u.PasswordHash, u.Role, u.MustChangePassword, u.Disabled, now, now,
		u.OIDCIssuer, u.OIDCSubject, u.DisplayName, providerName)
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

// UserByIdentity finds the account of an OIDC identity.
func (s *Store) UserByIdentity(ctx context.Context, issuer, subject string) (*User, error) {
	if issuer == "" || subject == "" {
		return nil, ErrNotFound
	}
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`, issuer, subject))
}

func (s *Store) Users(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetUserAccess changes what an account may do: its role and whether it may
// sign in at all.
func (s *Store) SetUserAccess(ctx context.Context, id int64, role Role, disabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET role = ?, disabled = ?, updated_at = ? WHERE id = ?`, role, disabled, s.now().Unix(), id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

// NoteLogin records a sign-in, and for an account of a provider what the
// provider says about it now: its login name, the person's name and the
// role its groups give it. providerName is "" for a local account.
func (s *Store) NoteLogin(ctx context.Context, id int64, role Role, displayName, providerName string) error {
	now := s.now().Unix()
	res, err := s.db.ExecContext(ctx, `UPDATE users SET role = ?, display_name = ?, oidc_username = ?, last_login_at = ?, updated_at = ? WHERE id = ?`, role, displayName, providerName, now, now, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

// ActiveAdmins is how many accounts can administer the panel whatever
// happens at the provider: administrators with a password of their own. An
// administrator who signs in through the provider is one only as long as
// their group there says so, and stops being one, or being reachable, the
// moment it or the provider changes.
func (s *Store) ActiveAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND disabled = 0 AND oidc_subject IS NULL`).Scan(&n)
	return n, err
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

// AuditRow is one entry of the audit log as it is read back.
type AuditRow struct {
	ID      int64
	At      time.Time
	Actor   string
	Action  string
	Target  string
	IP      string
	Details string // a JSON object
}

// ListAudit returns the latest entries of the audit log, newest first.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor, action, target, ip, details FROM audit_log ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var a AuditRow
		var at int64
		if err := rows.Scan(&a.ID, &at, &a.Actor, &a.Action, &a.Target, &a.IP, &a.Details); err != nil {
			return nil, err
		}
		a.At = time.Unix(at, 0).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}

// PruneAudit deletes the entries older than before.
func (s *Store) PruneAudit(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_log WHERE at < ?`, before.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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

// IntegrityCheck asks SQLite whether the database file is sound.
func (s *Store) IntegrityCheck(ctx context.Context) (bool, error) {
	var verdict string
	err := s.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&verdict)
	return verdict == "ok", err
}

// tables are the ones CountRows may be asked about: a table name cannot be
// a placeholder, so it is only ever one of these.
var tables = map[string]string{
	"users": `SELECT COUNT(*) FROM users`, "sessions": `SELECT COUNT(*) FROM sessions`, "clients": `SELECT COUNT(*) FROM clients`,
	"inbounds": `SELECT COUNT(*) FROM inbounds`, "nodes": `SELECT COUNT(*) FROM nodes`, "route_lists": `SELECT COUNT(*) FROM route_lists`,
	"route_rules": `SELECT COUNT(*) FROM route_rules`, "settings": `SELECT COUNT(*) FROM settings`, "audit_log": `SELECT COUNT(*) FROM audit_log`,
}

// CountRows is how many rows a table holds.
func (s *Store) CountRows(ctx context.Context, table string) (int, error) {
	q, ok := tables[table]
	if !ok {
		return 0, ErrNotFound
	}
	var n int
	err := s.db.QueryRowContext(ctx, q).Scan(&n)
	return n, err
}

// Snapshot copies the whole database, as of one moment, into a new file
// while it stays in use.
func (s *Store) Snapshot(ctx context.Context, to string) error {
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, to)
	return err
}
