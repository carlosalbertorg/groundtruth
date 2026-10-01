package store

import (
	"errors"
	"strings"

	"modernc.org/sqlite"
)

// sqliteConstraintPrimaryCode is SQLite's primary SQLITE_CONSTRAINT result
// code. Extended result codes (which this driver may or may not have
// enabled for a given error) pack the primary code into the low byte, so
// masking with 0xff recovers it either way.
const sqliteConstraintPrimaryCode = 19

// IsUniqueConstraintViolation reports whether err came from violating a
// UNIQUE constraint (e.g. workspaces.name), as opposed to some other
// failure. Callers use this to turn a known, expected conflict into a 409
// response instead of a generic 500.
func IsUniqueConstraintViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	if sqliteErr.Code()&0xff != sqliteConstraintPrimaryCode {
		return false
	}
	return strings.Contains(sqliteErr.Error(), "UNIQUE")
}
