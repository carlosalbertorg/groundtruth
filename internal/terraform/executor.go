package terraform

import (
	"context"
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
func (e *Executor) RunCheck(ctx context.Context, in CheckInput) (*tfjson.Plan, error) {
	tmpDir, err := os.MkdirTemp(e.baseTmpDir, "check-*")
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

	env, err := e.buildEnv(in.CredentialEnvFile)
	if err != nil {
		return nil, fmt.Errorf("build environment: %w", err)
	}

	tfClient, err := e.newClient(in.BinaryKind, workDir, env)
	if err != nil {
		return nil, err
	}

	if err := tfClient.Init(checkCtx); err != nil {
		return nil, fmt.Errorf("terraform init: %w", err)
	}

	planPath := filepath.Join(workDir, "plan.out")
	if err := tfClient.PlanRefreshOnly(checkCtx, planPath); err != nil {
		return nil, fmt.Errorf("terraform plan: %w", err)
	}

	plan, err := tfClient.ShowPlanFile(checkCtx, planPath)
	if err != nil {
		return nil, fmt.Errorf("terraform show: %w", err)
	}

	return plan, nil
}

// buildEnv is the *complete* subprocess environment - see
// terraform_client.go/tofu_client.go, which call SetEnv with exactly
// this map, replacing (not merging with) groundtruth's own environment.
// Deliberately excludes TF_IN_AUTOMATION/TF_INPUT: both libraries already
// force non-interactive behavior themselves and reject attempts to set
// those two manually.
func (e *Executor) buildEnv(credentialEnvFile string) (map[string]string, error) {
	env := map[string]string{
		"PATH": os.Getenv("PATH"),
		"HOME": os.Getenv("HOME"),
		// Skip HashiCorp's version-check telemetry ping - no reason for
		// a drift check running every few minutes to make that call.
		"CHECKPOINT_DISABLE":  "1",
		"TF_PLUGIN_CACHE_DIR": e.pluginCacheDir,
	}

	if credentialEnvFile != "" {
		creds, err := readCredentialEnvFile(credentialEnvFile)
		if err != nil {
			return nil, fmt.Errorf("read credential env file: %w", err)
		}
		for k, v := range creds {
			env[k] = v
		}
	}

	return env, nil
}

// isWithin reports whether target is base itself or a descendant of it.
func isWithin(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
