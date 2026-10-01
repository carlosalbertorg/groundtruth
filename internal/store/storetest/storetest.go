// Package storetest gives tests a real, migrated, temporary SQLite
// database — matching this project's testing philosophy of exercising a
// real database rather than mocking the DB layer.
package storetest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/carlosalbertorg/groundtruth/internal/store"
)

// OpenDB opens a fresh, migrated SQLite database in a directory that's
// removed when t's test (and its subtests) finish.
func OpenDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	db, err := store.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	if err := store.Migrate(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	return db
}
