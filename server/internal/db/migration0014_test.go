package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// The old ordered paths must come back as edge graphs with the same shape:
// one edge per hop, backup entries as extra flagged sources, backup exits as
// extra sinks.
func TestChainGraphConversion(t *testing.T) {
	ctx := context.Background()
	d, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "portunus.db")+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ms, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	idx := -1
	for i, m := range ms {
		if m.version == 14 {
			idx = i
		}
	}
	if idx < 0 || ms[len(ms)-1].version < 14 {
		t.Fatalf("migration 0014 must be present and not the newest, got up to %s", ms[len(ms)-1].name)
	}
	for _, m := range ms[:idx] {
		if _, err := d.ExecContext(ctx, m.sql); err != nil {
			t.Fatalf("migration %s: %v", m.name, err)
		}
	}

	seed := `
	INSERT INTO nodes (id, name, kind, address, is_exit, is_entry, enabled, settings, secret, created_at, updated_at) VALUES
		(1, 'N1', 'agent', 'n1.example', 0, 1, 1, '{}', '', 0, 0),
		(2, 'N2', 'agent', 'n2.example', 1, 0, 1, '{}', '', 0, 0),
		(3, 'N3', 'agent', 'n3.example', 1, 0, 1, '{}', '', 0, 0),
		(4, 'N4', 'agent', 'n4.example', 0, 1, 1, '{}', '', 0, 0),
		(5, 'N5', 'agent', 'n5.example', 1, 0, 1, '{}', '', 0, 0);
	INSERT INTO chains (id, name, nodes, created_at, updated_at) VALUES
		(1, 'Two', '[0, 1, 3]', 0, 0),
		(2, 'Solo', '[2]', 0, 0),
		(3, 'Plain', '[1, 2]', 0, 0);
	UPDATE chains SET backup_entry_node_id = 4, backup_exit_node_id = 5 WHERE id = 2;
	INSERT INTO chain_links (chain_id, position, from_node_id, to_node_id, hop_port, uuid, settings, secret, created_at) VALUES
		(1, 0, 0, 1, 18001, 'u-1', '{}', '', 0),
		(1, 1, 1, 3, 18002, 'u-2', '{}', '', 0),
		(2, 0, 0, 2, 18003, 'u-3', '{}', '', 0),
		(3, 0, 0, 1, 18004, 'u-4', '{}', '', 0),
		(3, 1, 1, 2, 18005, 'u-5', '{}', '', 0);
	INSERT INTO clients (id, name, enabled, uuid, password, sub_token, created_at, updated_at) VALUES
		(1, 'c1', 1, 'uuid-1', 'pass-1', 'sub-1', 0, 0);
	INSERT INTO client_routes (client_id, chain_id, uuid) VALUES (1, 1, 'uuid-1');
	`
	if _, err := d.ExecContext(ctx, seed); err != nil {
		t.Fatal(err)
	}

	if _, err := d.ExecContext(ctx, ms[idx].sql); err != nil {
		t.Fatalf("migration %s: %v", ms[idx].name, err)
	}

	rows, err := d.QueryContext(ctx,
		"SELECT chain_id || ':' || from_node_id || '->' || to_node_id || ' port=' || hop_port FROM chain_edges ORDER BY chain_id, from_node_id, to_node_id")
	if err != nil {
		t.Fatal(err)
	}
	var edges []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatal(err)
		}
		edges = append(edges, e)
	}
	rows.Close()
	want := []string{
		"1:0->1 port=18001", "1:1->3 port=18002",
		"2:0->2 port=18003", "2:0->5 port=0", "2:4->2 port=0",
		"3:0->1 port=18004", "3:1->2 port=18005",
	}
	if len(edges) != len(want) {
		t.Fatalf("edges: got %v, want %v", edges, want)
	}
	for i := range want {
		if edges[i] != want[i] {
			t.Fatalf("edge %d: got %s, want %s", i, edges[i], want[i])
		}
	}

	var flags int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM chain_entry_flags WHERE chain_id = 2 AND node_id = 4").Scan(&flags); err != nil {
		t.Fatal(err)
	}
	if flags != 1 {
		t.Fatalf("backup entry flag: got %d, want 1", flags)
	}

	for _, q := range []string{
		"SELECT nodes FROM chains LIMIT 1",
		"SELECT * FROM chain_links",
		"SELECT * FROM client_routes",
	} {
		if err := d.QueryRowContext(ctx, q).Scan(new(any)); err == nil {
			t.Fatalf("expected %q to fail after the migration", q)
		} else if err != sql.ErrNoRows && !isNoSuchColumnOrTable(err) {
			t.Fatalf("%q: unexpected error %v", q, err)
		}
	}

	// The later migrations must still run on top of the converted schema.
	for _, m := range ms[idx+1:] {
		if _, err := d.ExecContext(ctx, m.sql); err != nil {
			t.Fatalf("migration %s: %v", m.name, err)
		}
	}
	var col string
	if err := d.QueryRowContext(ctx, "SELECT name FROM pragma_table_info('route_rules') WHERE name = 'chain_id'").Scan(&col); err != nil {
		t.Fatalf("route_rules.chain_id: %v", err)
	}
}

func isNoSuchColumnOrTable(err error) bool {
	return err != nil && (contains(err.Error(), "no such column") || contains(err.Error(), "no such table"))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
