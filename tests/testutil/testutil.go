// Package testutil provides database fixtures shared by module and integration tests.
package testutil

import (
	"database/sql"
	"testing"

	"github.com/RSTCK-Innovation/arveld/internal/database"
)

// OpenDatabase opens and migrates a test database and closes it during cleanup.
func OpenDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := database.OpenFile(t.Context(), path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := database.Migrate(t.Context(), db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	return db
}
