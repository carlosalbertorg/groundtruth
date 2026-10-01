package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// skipIfNoUnixPermissions skips tests that assert on permission bits:
// Windows reports a fixed mode for every file, so there's nothing to check.
func skipIfNoUnixPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits aren't meaningful on Windows")
	}
}

func TestOpenCreatesDatabaseOwnerOnly(t *testing.T) {
	skipIfNoUnixPermissions(t)

	path := filepath.Join(t.TempDir(), "groundtruth.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(p)
		if err != nil {
			if p == path {
				t.Fatalf("stat %s: %v", p, err)
			}
			continue // a sidecar that isn't there can't be too permissive
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s has mode %o, want no access for group or others", filepath.Base(p), perm)
		}
	}
}

func TestOpenKeepsAnExistingDatabaseIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groundtruth.db")

	first, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := Migrate(first); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := first.ExecContext(t.Context(), `INSERT INTO users (id, email, password_hash) VALUES ('u1', 'a@example.com', 'x')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Opening an existing file must not truncate or recreate it.
	second, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	var count int
	if err := second.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Errorf("users after reopening = %d, want 1: Open must not clobber an existing database", count)
	}
}

func TestRestrictAccessTightensExistingModes(t *testing.T) {
	skipIfNoUnixPermissions(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "groundtruth.db")
	if err := os.WriteFile(dbPath, nil, 0o644); err != nil { // #nosec G306 -- deliberately too open: the test tightens it
		t.Fatalf("write db: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { // #nosec G302 -- deliberately too open: the test tightens it
		t.Fatalf("chmod dir: %v", err)
	}

	// No -wal/-shm files exist: that must not be an error.
	if err := RestrictAccess(dir, dbPath); err != nil {
		t.Fatalf("RestrictAccess: %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("data dir mode = %o, want 700", perm)
	}
	dbInfo, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if perm := dbInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("database mode = %o, want 600", perm)
	}
}
