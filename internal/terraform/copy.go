package terraform

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// copyModuleSource copies src into dst, skipping:
//   - .git and .terraform directories (VCS metadata and cached provider
//     plugins/state respectively - the latter would also be pointless to
//     copy, since `init` takes providers from the plugin cache shared by
//     every check, via TF_PLUGIN_CACHE_DIR)
//   - any terraform.tfstate* file - a real state file must never be
//     dragged into an isolated check's throwaway directory, where
//     "refresh-only" behavior and provider credential scoping assumptions
//     no longer hold
//
// .terraform.lock.hcl (the dependency lock file) is a normal file and is
// copied like any other - only the .terraform directory itself is
// skipped.
func copyModuleSource(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			if rel != "." && (d.Name() == ".git" || d.Name() == ".terraform") {
				return filepath.SkipDir
			}
			target := dst
			if rel != "." {
				target = filepath.Join(dst, rel)
			}
			return os.MkdirAll(target, 0o700)
		}

		if strings.HasPrefix(d.Name(), "terraform.tfstate") {
			return nil
		}

		return copyFile(path, filepath.Join(dst, rel))
	})
}

func copyFile(src, dst string) error {
	// gosec G304 flags both opens below as "file inclusion via variable."
	// src/dst are built from a workspace's source_path (validated
	// absolute) and working_subdirectory (validated to never escape
	// upward - see isSafeRelativeSubdir in internal/httpapi, and isWithin
	// in executor.go for this package's own defensive re-check), both
	// set only by an authenticated admin through the workspace CRUD API.
	// There's no untrusted caller of this function.
	in, err := os.Open(src) // #nosec G304
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm()) // #nosec G304
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
