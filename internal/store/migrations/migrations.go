// Package migrations embeds groundtruth's versioned SQL schema migrations
// so they ship inside the binary instead of as loose files on disk.
package migrations

import "embed"

// FS holds every migration's .up.sql/.down.sql pair, embedded so they ship
// inside the binary.
//
//go:embed *.sql
var FS embed.FS
