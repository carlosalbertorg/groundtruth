// Package webassets embeds the built frontend (web/dist, copied here by
// `make build-frontend`) into the Go binary.
package webassets

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// Dist returns the embedded frontend build output rooted at "dist", so
// callers see e.g. "index.html" rather than "dist/index.html".
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
