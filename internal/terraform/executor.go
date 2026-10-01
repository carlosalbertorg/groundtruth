package terraform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tfjson "github.com/hashicorp/terraform-json"
)

// defaultTimeout applies when a workspace's configured timeout is zero
// or negative - callers are expected to pass a real value, this is only
// a last-resort guard against an unbounded check.
const defaultTimeout = 10 * time.Minute

// checkDirPrefix names every per-check scratch directory. SweepStale only
// ever removes entries with this prefix, so it can't touch anything else
// that happens to live in the same parent directory.
const checkDirPrefix = "check-"

// Executor runs isolated Terraform/OpenTofu drift checks: every call to
// RunCheck gets its own disposable copy of the module source in a fresh
// temp directory, which is removed once the check finishes - win or
// fail - so nothing about one check (including a plan file that may
// contain unredacted sensitive values) outlives it.
type Executor struct {
	baseTmpDir     string
	pluginCacheDir string

	// newClient is swapped out in tests to avoid depending on a real
	// terraform/tofu binary being on PATH.
	newClient func(binaryKind, workDir string, env map[string]string) (client, error)
}

// NewExecutor builds an Executor. baseTmpDir holds the per-check
// disposable directories; pluginCacheDir is shared and persistent across
// every check and workspace, so provider plugins are downloaded once
// rather than on every scheduled run.
func NewExecutor(baseTmpDir, pluginCacheDir string) (*Executor, error) {
	// Absolute, so the "does the source contain our scratch space" check in
	// RunCheck compares like with like whatever the working directory is.
	baseTmpDir, err := filepath.Abs(baseTmpDir)
	if err != nil {
		return nil, fmt.Errorf("resolve base temp dir: %w", err)
	}
	if err := os.MkdirAll(baseTmpDir, 0o700); err != nil {
		return nil, fmt.Errorf("create base temp dir: %w", err)
	}
	if err := os.MkdirAll(pluginCacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create plugin cache dir: %w", err)
	}

	return &Executor{
		baseTmpDir:     baseTmpDir,
		pluginCacheDir: pluginCacheDir,
		newClient:      newClient,
	}, nil
}

// SweepStale removes check directories left behind by an earlier process
// that was killed outright (SIGKILL, the OOM killer, a power cut) before
// its deferred cleanup could run. Such a directory can hold a plan file
// with unredacted sensitive values, so it must not be left to linger until
// someone happens to notice it.
//
// Call it once at startup, before any check runs. It assumes this is the
// only groundtruth process using baseTmpDir - the only supported
// deployment - because run while another process had a check in flight, it
// would delete that check's working directory out from under it.
func (e *Executor) SweepStale() (removed int, err error) {
	entries, err := os.ReadDir(e.baseTmpDir)
	if err != nil {
		return 0, fmt.Errorf("read check scratch dir: %w", err)
	}

	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), checkDirPrefix) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(e.baseTmpDir, entry.Name())); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// CheckInput is everything RunCheck needs about the workspace being
// checked. It's a plain struct (not a *sqlc.Workspace) so this package
// has no dependency on the storage layer.
type CheckInput struct {
	SourcePath          string
	WorkingSubdirectory string
	BinaryKind          string // "terraform" | "tofu"
	CredentialEnvFile   string // optional; read fresh from disk, never stored
	Timeout             time.Duration
}

// RunCheck runs `init` then `plan -refresh-only` against a fresh copy of
// in.SourcePath, returning the parsed plan. The plan this returns still
// contains unredacted sensitive values - callers must run it through
// internal/drift before persisting or returning it to a client.
//
// Any error it returns has had the credential file's values removed from
// its text (see scrubSecrets), since that text is stored and shown.
func (e *Executor) RunCheck(ctx context.Context, in CheckInput) (*tfjson.Plan, error) {
	if err := e.rejectSelfCopy(in.SourcePath); err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp(e.baseTmpDir, checkDirPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("create isolated workdir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := copyModuleSource(in.SourcePath, tmpDir); err != nil {
		return nil, fmt.Errorf("copy module source: %w", err)
	}

	workDir := tmpDir
	if in.WorkingSubdirectory != "" {
		workDir = filepath.Join(tmpDir, in.WorkingSubdirectory)
		// Defense in depth: the HTTP layer already rejects a
		// WorkingSubdirectory that escapes upward (see
		// isSafeRelativeSubdir in internal/httpapi), but this package
		// has no other caller yet to rely on that, so it checks again
		// rather than trusting workDir is still under tmpDir.
		if !isWithin(tmpDir, workDir) {
			return nil, fmt.Errorf("working subdirectory %q escapes the module source", in.WorkingSubdirectory)
		}
	}

	timeout := in.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var creds map[string]string
	if in.CredentialEnvFile != "" {
		creds, err = readCredentialEnvFile(in.CredentialEnvFile)
		if err != nil {
			return nil, fmt.Errorf("build environment: read credential env file: %w", err)
		}
	}

	tfClient, err := e.newClient(in.BinaryKind, workDir, e.buildEnv(creds))
	if err != nil {
		return nil, scrubError(err, creds)
	}

	if err := tfClient.Init(checkCtx); err != nil {
		return nil, scrubError(fmt.Errorf("terraform init: %w", err), creds)
	}

	planPath := filepath.Join(workDir, "plan.out")
	if err := tfClient.PlanRefreshOnly(checkCtx, planPath); err != nil {
		return nil, scrubError(fmt.Errorf("terraform plan: %w", err), creds)
	}

	plan, err := tfClient.ShowPlanFile(checkCtx, planPath)
	if err != nil {
		return nil, scrubError(fmt.Errorf("terraform show: %w", err), creds)
	}

	return plan, nil
}

// rejectSelfCopy refuses a source path that contains this executor's own
// scratch directory. Copying such a source would walk into the very
// directory being populated and never terminate, growing until the disk is
// full. It's what happens when a workspace is pointed at "/" or at the
// data directory itself instead of at a module.
func (e *Executor) rejectSelfCopy(sourcePath string) error {
	src, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}
	if isWithin(src, e.baseTmpDir) {
		return fmt.Errorf("source path %q contains groundtruth's own scratch directory (%s): point the workspace at the module itself, not at a parent of the data directory", sourcePath, e.baseTmpDir)
	}
	return nil
}

// buildEnv is the *complete* subprocess environment - see
// terraform_client.go/tofu_client.go, which call SetEnv with exactly
// this map, replacing (not merging with) groundtruth's own environment.
// Deliberately excludes TF_IN_AUTOMATION/TF_INPUT: both libraries already
// force non-interactive behavior themselves and reject attempts to set
// those two manually.
func (e *Executor) buildEnv(creds map[string]string) map[string]string {
	env := map[string]string{
		"PATH": os.Getenv("PATH"),
		"HOME": os.Getenv("HOME"),
		// Skip HashiCorp's version-check telemetry ping - no reason for
		// a drift check running every few minutes to make that call.
		"CHECKPOINT_DISABLE":  "1",
		"TF_PLUGIN_CACHE_DIR": e.pluginCacheDir,
	}
	for k, v := range creds {
		env[k] = v
	}
	return env
}

// isWithin reports whether target is base itself or a descendant of it.
func isWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
