package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ChainRow is a stored chain: a named directed graph over the nodes. The
// edges themselves live in chain_edges (ChainEdgeRow); a source node marked
// as a backup entry lives in chain_entry_flags. Clients reference chains by
// id; their per-exit credentials are derived from the client's own uuid and
// the chain's sinks, so nothing extra is stored per client.
type ChainRow struct {
	ID        int64
	Name      string
	Color     string // display color on the map; empty means the default
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChainEdgeRow is one directed edge of a chain. FromNodeID 0 means "every
// panel server" (the main server); ToNodeID always names a real node. The
// material (port, uuid, sealed settings and secret) is created when the edge
// is added and kept until it is deleted.
type ChainEdgeRow struct {
	ID         int64
	ChainID    int64
	FromNodeID int64
	ToNodeID   int64
	HopPort    int
	UUID       string
	Settings   string
	Secret     string
	CreatedAt  time.Time
}

func scanChain(row interface{ Scan(...any) error }) (ChainRow, error) {
	var (
		c       ChainRow
		created int64
		updated int64
	)
	err := row.Scan(&c.ID, &c.Name, &c.Color, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ChainRow{}, ErrNotFound
	}
	if err != nil {
		return ChainRow{}, err
	}
	c.CreatedAt = time.Unix(created, 0)
	c.UpdatedAt = time.Unix(updated, 0)
	return c, nil
}

func scanEdge(row interface{ Scan(...any) error }) (ChainEdgeRow, error) {
	var (
		e       ChainEdgeRow
		created int64
	)
	if err := row.Scan(&e.ID, &e.ChainID, &e.FromNodeID, &e.ToNodeID, &e.HopPort, &e.UUID, &e.Settings, &e.Secret, &created); err != nil {
		return ChainEdgeRow{}, err
	}
	e.CreatedAt = time.Unix(created, 0)
	return e, nil
}

// ListChains returns every chain in name order.
func (s *Store) ListChains(ctx context.Context) ([]ChainRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, color, created_at, updated_at FROM chains ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChainRow
	for rows.Next() {
		c, err := scanChain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChainByID returns one chain.
func (s *Store) ChainByID(ctx context.Context, id int64) (ChainRow, error) {
	c, err := scanChain(s.db.QueryRowContext(ctx, "SELECT id, name, color, created_at, updated_at FROM chains WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return ChainRow{}, ErrNotFound
	}
	return c, err
}

// CreateChain stores a new chain and returns its id.
func (s *Store) CreateChain(ctx context.Context, name, color string) (int64, error) {
	now := s.now().Unix()
	res, err := s.db.ExecContext(ctx, "INSERT INTO chains (name, color, created_at, updated_at) VALUES (?, ?, ?, ?)", name, color, now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrConflict
		}
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateChain renames a chain and sets its color.
func (s *Store) UpdateChain(ctx context.Context, id int64, name, color string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE chains SET name = ?, color = ?, updated_at = ? WHERE id = ?", name, color, s.now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteChain removes a chain and its edges.
func (s *Store) DeleteChain(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM chains WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return ErrNotFound
	}
	return nil
}

// ChainEdges returns the edges of one chain in a stable order (from node,
// to node).
func (s *Store) ChainEdges(ctx context.Context, chainID int64) ([]ChainEdgeRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at FROM chain_edges WHERE chain_id = ? ORDER BY from_node_id, to_node_id", chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChainEdgeRow
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AllChainEdges returns every edge of every chain.
func (s *Store) AllChainEdges(ctx context.Context) ([]ChainEdgeRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at FROM chain_edges ORDER BY chain_id, from_node_id, to_node_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChainEdgeRow
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ChainEdgesFrom returns the edges leaving one node (0: every panel server).
func (s *Store) ChainEdgesFrom(ctx context.Context, nodeID int64) ([]ChainEdgeRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at FROM chain_edges WHERE from_node_id = ? ORDER BY chain_id, to_node_id", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChainEdgeRow
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ChainEdgesTo returns the edges entering one real node.
func (s *Store) ChainEdgesTo(ctx context.Context, nodeID int64) ([]ChainEdgeRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at FROM chain_edges WHERE to_node_id = ? ORDER BY chain_id, from_node_id", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ChainEdgeRow
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateChainEdge stores a new edge; the caller supplies its material. The
// sealed secret is what goes into the row: the closure gets the fresh id so
// it can seal with the right associated data.
func (s *Store) CreateChainEdge(ctx context.Context, e ChainEdgeRow, seal func(id int64) (string, error)) (int64, error) {
	res, err := s.db.ExecContext(ctx, "INSERT INTO chain_edges (chain_id, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at) VALUES (?, ?, ?, ?, ?, ?, '', ?)",
		e.ChainID, e.FromNodeID, e.ToNodeID, e.HopPort, e.UUID, e.Settings, s.now().Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	sealed, err := seal(id)
	if err != nil {
		s.db.ExecContext(ctx, "DELETE FROM chain_edges WHERE id = ?", id) // leave no unsealed row behind
		return 0, err
	}
	res, err = s.db.ExecContext(ctx, "UPDATE chain_edges SET secret = ? WHERE id = ?", sealed, id)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	return id, nil
}

// DeleteChainEdge removes one edge.
func (s *Store) DeleteChainEdge(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM chain_edges WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return ErrNotFound
	}
	return nil
}

// ChainEntryFlags returns the backup-entry node ids of one chain.
func (s *Store) ChainEntryFlags(ctx context.Context, chainID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT node_id FROM chain_entry_flags WHERE chain_id = ? ORDER BY node_id", chainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetChainEntryFlags replaces the backup-entry markers of one chain.
func (s *Store) SetChainEntryFlags(ctx context.Context, chainID int64, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM chain_entry_flags WHERE chain_id = ?", chainID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, "INSERT INTO chain_entry_flags (chain_id, node_id) VALUES (?, ?)", chainID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetClientChain binds a client to a chain (nil clears it). The binding is
// mutually exclusive with the client's direct exit.
func (s *Store) SetClientChain(ctx context.Context, clientID int64, chainID *int64) error {
	var v any
	if chainID != nil {
		v = *chainID
	}
	res, err := s.db.ExecContext(ctx, "UPDATE clients SET chain_id = ? WHERE id = ?", v, clientID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClientChain returns the chain bound to a client.
func (s *Store) ClientChain(ctx context.Context, clientID int64) (*int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT chain_id FROM clients WHERE id = ?", clientID).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !id.Valid {
		return nil, nil
	}
	v := id.Int64
	return &v, nil
}

// ClientChainCount returns how many clients are bound to a chain.
func (s *Store) ClientChainCount(ctx context.Context, chainID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM clients WHERE chain_id = ?", chainID).Scan(&n)
	return n, err
}

// ChainExists reports whether a chain id exists.
func (s *Store) ChainExists(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chains WHERE id = ?", id).Scan(&n)
	return n > 0, err
}

// NodeInChain reports whether a node participates in any chain (edge end or
// backup flag), so it can be protected from deletion.
func (s *Store) NodeInChain(ctx context.Context, nodeID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chain_edges WHERE from_node_id = ? OR to_node_id = ?", nodeID, nodeID).Scan(&n)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chain_entry_flags WHERE node_id = ?", nodeID).Scan(&n)
	return n > 0, err
}

var _ = fmt.Sprintf // keep fmt for future use
