package db

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// An update changes the database, the new version does not come up, and the
// updater starts the previous one again. That one must find the database as
// it left it, not refuse to start for good.
func TestThePreviousVersionGetsItsDatabaseBack(t *testing.T) {
	ctx, log := context.Background(), slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	path, before := filepath.Join(dir, "portunus.db"), filepath.Join(dir, "before-update.db")

	d, err := Open(ctx, path, log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `CREATE TABLE mark (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	d.ExecContext(ctx, `INSERT INTO mark VALUES ('as the old version left it')`)
	if _, err := d.ExecContext(ctx, `VACUUM INTO ?`, before); err != nil {
		t.Fatal(err)
	}
	// The newer version: a migration this binary does not know, and a change.
	d.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (9999, 'from the future', 0)`)
	d.ExecContext(ctx, `UPDATE mark SET v = 'changed by the new version'`)
	d.Close()

	if _, err := Open(ctx, path, log); !errors.Is(err, ErrNewer) {
		t.Fatalf("a database from a newer version: %v", err)
	}
	// A copy from long ago is not this update's: it stays where it is.
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(before, old, old)
	if put, err := PutBack(path, before, time.Now()); put || err != nil {
		t.Fatalf("an old copy was put back: %v %v", put, err)
	}
	os.Chtimes(before, time.Now(), time.Now())
	if put, err := PutBack(path, before, time.Now()); !put || err != nil {
		t.Fatalf("put back: %v %v", put, err)
	}
	d, err = Open(ctx, path, log)
	if err != nil {
		t.Fatalf("the previous version still cannot start: %v", err)
	}
	defer d.Close()
	var v string
	if err := d.QueryRowContext(ctx, `SELECT v FROM mark`).Scan(&v); err != nil || v != "as the old version left it" {
		t.Fatalf("the database is not as the old version left it: %q %v", v, err)
	}
	// What the new version wrote is kept aside, not thrown away.
	if _, err := os.Stat(path + ".newer"); err != nil {
		t.Fatal("the newer database was not kept")
	}
}
