// Package store owns groundtruth's SQLite database: connecting to it,
// running its versioned migrations, and (via the sqlc subpackage) running
// type-safe queries against it.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

const (
	// dataDirMode and dbFileMode keep everything groundtruth persists
	// readable by its own user only. The database holds password and token
	// hashes and alert-destination secrets, so it shouldn't be readable by
	// every other local account.
	dataDirMode os.FileMode = 0o700
	dbFileMode  os.FileMode = 0o600
)

// Open opens the SQLite database at path, applying the pragmas groundtruth
// relies on: enforced foreign keys (SQLite ignores them by default), WAL
// journaling (readers don't block the single writer), and a busy timeout
// so concurrent access waits briefly instead of failing immediately.
//
// These are set via the DSN's _pragma parameters (rather than with
// db.Exec after opening) because database/sql maintains a pool of
// connections — an Exec only reaches whichever single connection services
// it, not every connection new or old in the pool. DSN pragmas, by
// contrast, are applied by the driver to every connection it opens.
//
// A missing database file is created owner-only (0600) rather than with
// SQLite's default of 0644; SQLite gives its -wal and -shm sidecar files
// the permissions of the main file, so they inherit this.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	// An empty file is a valid, empty SQLite database, so creating it here
	// first costs nothing and lets us pick its mode. path is the
	// operator-configured data directory, not request input.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, dbFileMode) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("create sqlite database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("create sqlite database file: %w", err)
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to sqlite database: %w", err)
	}
	return db, nil
}

// RestrictAccess tightens a data directory and database that may predate
// the owner-only modes Open and the server now create them with: the
// directory to 0700, the database file and its -wal/-shm sidecars (when
// present) to 0600.
//
// It's best effort, because it can legitimately fail - a directory the
// process doesn't own, such as a bind mount, can't be chmod'ed - so it
// reports what it couldn't change rather than refusing to start.
func RestrictAccess(dataDir, dbPath string) error {
	var errs []error
	if err := os.Chmod(dataDir, dataDirMode); err != nil {
		errs = append(errs, err)
	}
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Chmod(p, dbFileMode); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
