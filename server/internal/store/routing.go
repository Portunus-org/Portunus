package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// NodeRow is another server (or an imported proxy) as stored; Settings is
// public JSON and Secret is ciphertext produced by the secrets package.
type NodeRow struct {
	ID        int64
	Name      string
	Kind      string
	Address   string
	IsExit    bool
	IsEntry   bool
	Enabled   bool
	Settings  string
	Secret    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

const nodeColumns = `id, name, kind, address, is_exit, is_entry, enabled, settings, secret, created_at, updated_at`

func scanNode(row interface{ Scan(...any) error }) (*NodeRow, error) {
	var (
		n                NodeRow
		created, updated int64
	)
	err := row.Scan(&n.ID, &n.Name, &n.Kind, &n.Address, &n.IsExit, &n.IsEntry, &n.Enabled, &n.Settings, &n.Secret, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	n.CreatedAt = time.Unix(created, 0)
	n.UpdatedAt = time.Unix(updated, 0)
	return &n, nil
}

func (s *Store) ListNodes(ctx context.Context) ([]*NodeRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nodeColumns+` FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*NodeRow
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) NodeByID(ctx context.Context, id int64) (*NodeRow, error) {
	return scanNode(s.db.QueryRowContext(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id))
}

// CreateNode inserts n. The secret is bound to the row ID, so seal runs
// inside the transaction once the ID is known.
func (s *Store) CreateNode(ctx context.Context, n *NodeRow, seal func(id int64) (string, error)) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := s.now().Unix()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO nodes (name, kind, address, is_exit, is_entry, enabled, settings, secret, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
		n.Name, n.Kind, n.Address, n.IsExit, n.IsEntry, n.Enabled, n.Settings, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	if n.ID, err = res.LastInsertId(); err != nil {
		return err
	}
	if n.Secret, err = seal(n.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET secret = ? WHERE id = ?`, n.Secret, n.ID); err != nil {
		return err
	}
	n.CreatedAt = time.Unix(now, 0)
	n.UpdatedAt = n.CreatedAt
	return tx.Commit()
}

func (s *Store) UpdateNode(ctx context.Context, n *NodeRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE nodes SET name = ?, address = ?, is_exit = ?, is_entry = ?, enabled = ?, settings = ?, secret = ?, updated_at = ? WHERE id = ?`,
		n.Name, n.Address, n.IsExit, n.IsEntry, n.Enabled, n.Settings, n.Secret, s.now().Unix(), n.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	return expectOne(res)
}

func (s *Store) DeleteNode(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

// RouteListRow is a named set of domains and addresses.
type RouteListRow struct {
	ID      int64
	Name    string
	Kind    string
	URL     string
	Content string
	// Format is "text" (one entry per line) or "ruleset" (Content is the
	// JSON array of sing-box rules).
	Format     string
	FetchedAt  *time.Time
	FetchError string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const routeListColumns = `id, name, kind, url, content, format, fetched_at, fetch_error, created_at, updated_at`

func scanRouteList(row interface{ Scan(...any) error }) (*RouteListRow, error) {
	var (
		l                RouteListRow
		fetched          sql.NullInt64
		created, updated int64
	)
	err := row.Scan(&l.ID, &l.Name, &l.Kind, &l.URL, &l.Content, &l.Format, &fetched, &l.FetchError, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if fetched.Valid {
		t := time.Unix(fetched.Int64, 0)
		l.FetchedAt = &t
	}
	l.CreatedAt = time.Unix(created, 0)
	l.UpdatedAt = time.Unix(updated, 0)
	return &l, nil
}

func (s *Store) ListRouteLists(ctx context.Context) ([]*RouteListRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+routeListColumns+` FROM route_lists ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RouteListRow
	for rows.Next() {
		l, err := scanRouteList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) RouteListByID(ctx context.Context, id int64) (*RouteListRow, error) {
	return scanRouteList(s.db.QueryRowContext(ctx, `SELECT `+routeListColumns+` FROM route_lists WHERE id = ?`, id))
}

func (s *Store) CreateRouteList(ctx context.Context, l *RouteListRow) error {
	now := s.now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO route_lists (name, kind, url, content, format, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		l.Name, l.Kind, l.URL, l.Content, l.Format, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	l.ID, err = res.LastInsertId()
	l.CreatedAt = time.Unix(now, 0)
	l.UpdatedAt = l.CreatedAt
	return err
}

func (s *Store) UpdateRouteList(ctx context.Context, l *RouteListRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE route_lists SET name = ?, url = ?, content = ?, format = ?, updated_at = ? WHERE id = ?`,
		l.Name, l.URL, l.Content, l.Format, s.now().Unix(), l.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	return expectOne(res)
}

// SetRouteListFetch records the outcome of a download attempt. On success
// content replaces the stored list; on failure the previous content is kept
// and the reason noted.
func (s *Store) SetRouteListFetch(ctx context.Context, id int64, content *string, format, fetchErr string) error {
	now := s.now().Unix()
	if content != nil {
		_, err := s.db.ExecContext(ctx,
			`UPDATE route_lists SET content = ?, format = ?, fetched_at = ?, fetch_error = '' WHERE id = ?`, *content, format, now, id)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE route_lists SET fetched_at = ?, fetch_error = ? WHERE id = ?`, now, truncate(fetchErr, 300), id)
	return err
}

func (s *Store) DeleteRouteList(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM route_lists WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

// RouteRuleRow sends what a list matches to a destination.
type RouteRuleRow struct {
	ID        int64
	Position  int
	ListID    int64
	Action    string
	NodeID    *int64
	Enabled   bool
	CreatedAt time.Time
}

func (s *Store) ListRouteRules(ctx context.Context) ([]*RouteRuleRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, position, list_id, action, node_id, enabled, created_at FROM route_rules ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RouteRuleRow
	for rows.Next() {
		var (
			r       RouteRuleRow
			node    sql.NullInt64
			created int64
		)
		if err := rows.Scan(&r.ID, &r.Position, &r.ListID, &r.Action, &node, &r.Enabled, &created); err != nil {
			return nil, err
		}
		if node.Valid {
			r.NodeID = &node.Int64
		}
		r.CreatedAt = time.Unix(created, 0)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// CreateRouteRule appends r after the existing rules.
func (s *Store) CreateRouteRule(ctx context.Context, r *RouteRuleRow) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO route_rules (position, list_id, action, node_id, enabled, created_at)
		 VALUES ((SELECT COALESCE(MAX(position), 0) + 1 FROM route_rules), ?, ?, ?, ?, ?)`,
		r.ListID, r.Action, r.NodeID, r.Enabled, s.now().Unix())
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	return err
}

func (s *Store) UpdateRouteRule(ctx context.Context, r *RouteRuleRow) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE route_rules SET list_id = ?, action = ?, node_id = ?, enabled = ? WHERE id = ?`,
		r.ListID, r.Action, r.NodeID, r.Enabled, r.ID)
	if err != nil {
		return err
	}
	return expectOne(res)
}

func (s *Store) DeleteRouteRule(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM route_rules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return expectOne(res)
}

// OrderRouteRules sets the evaluation order to ids (first decides first).
func (s *Store) OrderRouteRules(ctx context.Context, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE route_rules SET position = ? WHERE id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
