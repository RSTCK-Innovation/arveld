package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"modernc.org/sqlite"

	sqlite3 "modernc.org/sqlite/lib"
)

func TestOpenFileCreatesDirectoryAndConnectsToSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(
		t.TempDir(),
		"database",
		"test.db",
	)

	db, err := OpenFile(ctx, path)
	if err != nil {
		t.Fatalf("OpenFile() error = %v, want nil", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	var journalMode string
	if err := db.QueryRowContext(
		ctx,
		"PRAGMA journal_mode",
	).Scan(&journalMode); err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal mode = %q, want %q", journalMode, "wal")
	}

	var busyTimeout int
	if err := db.QueryRowContext(
		ctx,
		"PRAGMA busy_timeout",
	).Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy timeout = %d, want %d", busyTimeout, 5000)
	}
}

func TestOpenFileConnectsWithRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx := context.Background()

	db, err := OpenFile(ctx, filepath.Join("data", "arveld.db"))
	if err != nil {
		t.Fatalf("OpenFile() error = %v, want nil", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
}

func TestOpenFileRejectsOrphanConfigurationOnNewConnections(t *testing.T) {
	ctx := t.Context()
	db, err := OpenFile(ctx, filepath.Join(t.TempDir(), "arveld.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	// Discard idle connections so each insert opens a fresh connection.
	db.SetMaxIdleConns(0)
	for attempt := range 2 {
		_, err := db.ExecContext(ctx, `
			INSERT INTO agent_config_revisions (
				instance_uid, revision, config_hash, content
			) VALUES (?, 1, zeroblob(32), X'01')
		`, []byte{byte(attempt)})
		sqliteErr, ok := errors.AsType[*sqlite.Error](err)
		if !ok || sqliteErr.Code() != sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY {
			t.Errorf("insert %d error = %v, want foreign key violation", attempt, err)
		}
	}
}
