// Package buildinfo exposes version metadata set at build time via -ldflags.
package buildinfo

var (
	// Version is the released version (a git tag), or "dev" otherwise.
	Version = "dev"
	// Commit is the git commit SHA this binary was built from.
	Commit = "none"
	// Date is the build timestamp, in RFC3339.
	Date = "unknown"
)
