package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// InboundRow is an inbound as stored; Settings is public JSON and Secret is
// ciphertext produced by the secrets package.
type InboundRow struct {
	ID        int64
	Tag       string
	Name      string
	Protocol  string
	Port      int
	Enabled   bool
	Settings  string
	Secret    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const inboundColumns = `id, tag, name, protocol, port, enabled, settings, secret, created_at, updated_at`

func scanInbound(row interface{ Scan(...any) error }) (*InboundRow, error) {
	var (
		in               InboundRow
		created, updated int64
	)
	err := row.Scan(&in.ID, &in.Tag, &in.Name, &in.Protocol, &in.Port, &in.Enabled, &in.Settings, &in.Secret, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	in.CreatedAt = time.Unix(created, 0)
	in.UpdatedAt = time.Unix(updated, 0)
	return &in, nil
}

func (s *Store) ListInbounds(ctx context.Context) ([]*InboundRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+inboundColumns+` FROM inbounds ORDER BY port, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*InboundRow
	for rows.Next() {
		in, err := scanInbound(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Store) InboundByID(ctx context.Context, id int64) (*InboundRow, error) {
	return scanInbound(s.db.QueryRowContext(ctx, `SELECT `+inboundColumns+` FROM inbounds WHERE id = ?`, id))
}

// CreateInbound inserts in. The secret usually has to be bound to the row
// ID, so seal runs inside the transaction once the ID is known.
func (s *Store) CreateInbound(ctx context.Context, in *InboundRow, seal func(id int64) (string, error)) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := s.now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO inbounds (tag, name, protocol, port, enabled, settings, secret, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)`,
		in.Tag, in.Name, in.Protocol, in.Port, in.Enabled, in.Settings, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	if in.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	if in.Secret, err = seal(in.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE inbounds SET secret = ? WHERE id = ?`, in.Secret, in.ID); err != nil {
		return err
	}
	in.CreatedAt = time.Unix(now, 0)
	in.UpdatedAt = in.CreatedAt
	return tx.Commit()
}

func (s *Store) UpdateInbound(ctx context.Context, in *InboundRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE inbounds SET tag = ?, name = ?, port = ?, enabled = ?, settings = ?, secret = ?, updated_at = ? WHERE id = ?`,
		in.Tag, in.Name, in.Port, in.Enabled, in.Settings, in.Secret, s.now().Unix(), in.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	return expectOne(res)
}

func (s *Store) DeleteInbound(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM inbounds WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

type ClientRow struct {
	ID           int64
	Name         string
	Enabled      bool
	UUID         string
	Password     string
	SubToken     string
	TrafficLimit int64
	ExpiresAt    *time.Time
	Upload       int64
	Download     int64
	Note         string
	// ExitID is the exit the client is bound to (nil: none yet).
	ExitID *int64
	// LastSeen is when traffic of the client was last counted (nil: never).
	LastSeen  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

const clientColumns = `id, name, enabled, uuid, password, sub_token, traffic_limit, expires_at, upload, download, note, exit_node_id, last_seen_at, created_at, updated_at`

func scanClient(row interface{ Scan(...any) error }) (*ClientRow, error) {
	var (
		c                ClientRow
		expires, exit    sql.NullInt64
		seen             sql.NullInt64
		created, updated int64
	)
	err := row.Scan(&c.ID, &c.Name, &c.Enabled, &c.UUID, &c.Password, &c.SubToken, &c.TrafficLimit,
		&expires, &c.Upload, &c.Download, &c.Note, &exit, &seen, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if expires.Valid {
		t := time.Unix(expires.Int64, 0)
		c.ExpiresAt = &t
	}
	if exit.Valid {
		c.ExitID = &exit.Int64
	}
	if seen.Valid {
		t := time.Unix(seen.Int64, 0)
		c.LastSeen = &t
	}
	c.CreatedAt = time.Unix(created, 0)
	c.UpdatedAt = time.Unix(updated, 0)
	return &c, nil
}

func unixOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func (s *Store) ListClients(ctx context.Context) ([]*ClientRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+clientColumns+` FROM clients ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ClientRow
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ClientByID(ctx context.Context, id int64) (*ClientRow, error) {
	return scanClient(s.db.QueryRowContext(ctx, `SELECT `+clientColumns+` FROM clients WHERE id = ?`, id))
}

func (s *Store) ClientBySubToken(ctx context.Context, token string) (*ClientRow, error) {
	return scanClient(s.db.QueryRowContext(ctx, `SELECT `+clientColumns+` FROM clients WHERE sub_token = ?`, token))
}

func (s *Store) CreateClient(ctx context.Context, c *ClientRow) error {
	now := s.now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO clients (name, enabled, uuid, password, sub_token, traffic_limit, expires_at, note, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Enabled, c.UUID, c.Password, c.SubToken, c.TrafficLimit, unixOrNil(c.ExpiresAt), c.Note, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	c.ID, err = res.LastInsertId()
	c.CreatedAt = time.Unix(now, 0)
	c.UpdatedAt = c.CreatedAt
	return err
}

// UpdateClient writes the editable fields and credentials; traffic counters
// are only changed through AddTraffic and ResetTraffic.
func (s *Store) UpdateClient(ctx context.Context, c *ClientRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE clients SET name = ?, enabled = ?, uuid = ?, password = ?, sub_token = ?, traffic_limit = ?,
		        expires_at = ?, note = ?, updated_at = ?
		 WHERE id = ?`,
		c.Name, c.Enabled, c.UUID, c.Password, c.SubToken, c.TrafficLimit, unixOrNil(c.ExpiresAt), c.Note,
		s.now().Unix(), c.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	return expectOne(res)
}

// SetClientExit binds a client to an exit (nil: to none). It does not count
// as an edit of the client.
func (s *Store) SetClientExit(ctx context.Context, id int64, exit *int64) error {
	var v any
	if exit != nil {
		v = *exit
	}
	res, err := s.db.ExecContext(ctx, `UPDATE clients SET exit_node_id = ? WHERE id = ?`, v, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

func (s *Store) DeleteClient(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM clients WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

// TrafficDelta is traffic observed for one client since the last poll.
type TrafficDelta struct {
	ClientID int64
	Upload   int64
	Download int64
}

// trafficKept is how far back the hourly history of traffic goes.
const trafficKept = 92 * 24 * time.Hour

// AddTraffic adds what was counted since the last poll to its clients and
// to the hour it was counted in.
func (s *Store) AddTraffic(ctx context.Context, deltas []TrafficDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `UPDATE clients SET upload = upload + ?, download = download + ?, last_seen_at = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := s.now()
	var up, down int64
	for _, d := range deltas {
		// A client deleted between polls simply matches no row.
		if _, err := stmt.ExecContext(ctx, d.Upload, d.Download, now.Unix(), d.ClientID); err != nil {
			return err
		}
		up, down = up+d.Upload, down+d.Download
	}
	hour := now.Truncate(time.Hour).Unix()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO traffic_hours (hour, upload, download) VALUES (?, ?, ?)
		 ON CONFLICT (hour) DO UPDATE SET upload = upload + excluded.upload, download = download + excluded.download`,
		hour, up, down); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_hours WHERE hour < ?`, now.Add(-trafficKept).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// TrafficHour is the traffic of all clients within one hour.
type TrafficHour struct {
	Hour     time.Time
	Upload   int64
	Download int64
}

// TrafficSince returns the hours with traffic from since on, oldest first.
// Hours in which nothing passed are simply absent.
func (s *Store) TrafficSince(ctx context.Context, since time.Time) ([]TrafficHour, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hour, upload, download FROM traffic_hours WHERE hour >= ? ORDER BY hour`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficHour
	for rows.Next() {
		var (
			h    TrafficHour
			hour int64
		)
		if err := rows.Scan(&hour, &h.Upload, &h.Download); err != nil {
			return nil, err
		}
		h.Hour = time.Unix(hour, 0)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) ResetTraffic(ctx context.Context, clientID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE clients SET upload = 0, download = 0, updated_at = ? WHERE id = ?`, s.now().Unix(), clientID)
	if err != nil {
		return err
	}
	return expectOne(res)
}

// Setting returns a panel-wide setting, or "" when unset.
func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Settings returns every setting as it is stored.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// RestoreSettings puts the settings back exactly as a call to Settings
// found them: what was changed since is undone, what was added is removed.
func (s *Store) RestoreSettings(ctx context.Context, was map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings`); err != nil {
		return err
	}
	for k, v := range was {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}
