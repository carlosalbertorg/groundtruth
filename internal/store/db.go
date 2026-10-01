// Package store owns groundtruth's SQLite database: connecting to it,
// running its versioned migrations, and (via the sqlc subpackage) running
// type-safe queries against it.
package store

import (
	"context"
	"database/sql"
	"fmt"
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
func Open(ctx context.Context, path string) (*sql.DB, error) {
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
