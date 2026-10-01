package terraform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tfjson "github.com/hashicorp/terraform-json"
)

type fakeClient struct {
	initErr error
	planErr error
	showErr error
	plan    *tfjson.Plan

	initCalled bool
	planOut    string
}

func (f *fakeClient) Init(_ context.Context) error {
	f.initCalled = true
	return f.initErr
}

func (f *fakeClient) PlanRefreshOnly(_ context.Context, outPath string) error {
	f.planOut = outPath
	return f.planErr
}

func (f *fakeClient) ShowPlanFile(_ context.Context, _ string) (*tfjson.Plan, error) {
	return f.plan, f.showErr
}

// newTestExecutor builds an Executor whose newClient is faked, capturing
// the binaryKind/workDir/env it was called with, so tests can assert on
// them without needing a real terraform/tofu binary.
func newTestExecutor(t *testing.T, fake *fakeClient) (*Executor, *capturedClientArgs) {
	t.Helper()

	e, err := NewExecutor(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	captured := &capturedClientArgs{}
	e.newClient = func(binaryKind, workDir string, env map[string]string) (client, error) {
		captured.binaryKind = binaryKind
		captured.workDir = workDir
		captured.env = env
		return fake, nil
	}
	return e, captured
}

type capturedClientArgs struct {
	binaryKind string
	workDir    string
	env        map[string]string
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestRunCheckCopiesSourceIntoIsolatedDirAndCleansUp(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), `resource "null_resource" "x" {}`)
	writeFile(t, filepath.Join(source, ".git", "HEAD"), "ref: refs/heads/main")
	writeFile(t, filepath.Join(source, "terraform.tfstate"), `{"should":"never be copied"}`)
	writeFile(t, filepath.Join(source, "terraform.tfstate.backup"), `{"should":"never be copied"}`)
	writeFile(t, filepath.Join(source, ".terraform.lock.hcl"), "# a real, intentionally-committed file")

	fake := &fakeClient{plan: &tfjson.Plan{}}
	e, captured := newTestExecutor(t, fake)

	_, err := e.RunCheck(context.Background(), CheckInput{
		SourcePath: source,
		BinaryKind: "terraform",
	})
	if err != nil {
		t.Fatalf("RunCheck: %v", err)
	}

	if !fake.initCalled {
		t.Error("Init was never called")
	}
	if captured.binaryKind != "terraform" {
		t.Errorf("binaryKind = %q, want terraform", captured.binaryKind)
	}

	// The isolated workdir must have been removed by the time RunCheck
	// returns - this is the single most important property of the whole
	// executor (the plan file can contain unredacted secrets).
	if _, err := os.Stat(captured.workDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("isolated workdir %q still exists after RunCheck returned", captured.workDir)
	}
}

func TestRunCheckSkipsGitAndStateButKeepsLockFile(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")
	writeFile(t, filepath.Join(source, ".git", "HEAD"), "x")
	writeFile(t, filepath.Join(source, "terraform.tfstate"), "x")
	writeFile(t, filepath.Join(source, ".terraform.lock.hcl"), "x")
	writeFile(t, filepath.Join(source, ".terraform", "providers", "cached-plugin"), "x")

	var seenFiles []string
	fake := &fakeClient{plan: &tfjson.Plan{}}
	e, _ := newTestExecutor(t, fake)
	e.newClient = func(_, workDir string, _ map[string]string) (client, error) {
		entries, err := os.ReadDir(workDir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", workDir, err)
		}
		for _, entry := range entries {
			seenFiles = append(seenFiles, entry.Name())
		}
		return fake, nil
	}

	if _, err := e.RunCheck(context.Background(), CheckInput{SourcePath: source, BinaryKind: "terraform"}); err != nil {
		t.Fatalf("RunCheck: %v", err)
	}

	assertContains(t, seenFiles, "main.tf")
	assertContains(t, seenFiles, ".terraform.lock.hcl")
	assertNotContains(t, seenFiles, ".git")
	assertNotContains(t, seenFiles, ".terraform")
	assertNotContains(t, seenFiles, "terraform.tfstate")
}

func TestRunCheckUsesWorkingSubdirectory(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "envs", "prod", "main.tf"), "resource")

	var workDirAtCallTime string
	var statErr error
	fake := &fakeClient{plan: &tfjson.Plan{}}
	e, _ := newTestExecutor(t, fake)
	e.newClient = func(_, workDir string, _ map[string]string) (client, error) {
		// Must check from inside this callback: by the time RunCheck
		// returns, its deferred cleanup has already deleted workDir -
		// that auto-cleanup is itself the property under test elsewhere.
		workDirAtCallTime = workDir
		_, statErr = os.Stat(filepath.Join(workDir, "main.tf"))
		return fake, nil
	}

	if _, err := e.RunCheck(context.Background(), CheckInput{
		SourcePath:          source,
		WorkingSubdirectory: "envs/prod",
		BinaryKind:          "terraform",
	}); err != nil {
		t.Fatalf("RunCheck: %v", err)
	}

	if filepath.Base(workDirAtCallTime) != "prod" {
		t.Errorf("workDir = %q, want it to end in .../envs/prod", workDirAtCallTime)
	}
	if statErr != nil {
		t.Errorf("main.tf not found in working subdirectory: %v", statErr)
	}
}

func TestRunCheckMergesCredentialsIntoEnv(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")

	credFile := filepath.Join(t.TempDir(), "creds.env")
	writeFile(t, credFile, "AWS_ACCESS_KEY_ID=fake-key\nAWS_SECRET_ACCESS_KEY=fake-secret\n")

	fake := &fakeClient{plan: &tfjson.Plan{}}
	e, captured := newTestExecutor(t, fake)

	if _, err := e.RunCheck(context.Background(), CheckInput{
		SourcePath:        source,
		BinaryKind:        "terraform",
		CredentialEnvFile: credFile,
	}); err != nil {
		t.Fatalf("RunCheck: %v", err)
	}

	if captured.env["AWS_ACCESS_KEY_ID"] != "fake-key" {
		t.Errorf("AWS_ACCESS_KEY_ID = %q, want fake-key", captured.env["AWS_ACCESS_KEY_ID"])
	}
	if captured.env["AWS_SECRET_ACCESS_KEY"] != "fake-secret" {
		t.Errorf("AWS_SECRET_ACCESS_KEY = %q, want fake-secret", captured.env["AWS_SECRET_ACCESS_KEY"])
	}
	// The library-forced vars must never be in our map - both tfexec and
	// tofu-exec reject SetEnv calls that include them.
	for _, forbidden := range []string{"TF_IN_AUTOMATION", "TF_INPUT"} {
		if _, ok := captured.env[forbidden]; ok {
			t.Errorf("env contains %s, which SetEnv would reject", forbidden)
		}
	}
}

func TestRunCheckScrubsCredentialValuesFromErrors(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")

	const secret = "s3cr3t-api-key-value"
	credFile := filepath.Join(t.TempDir(), "creds.env")
	writeFile(t, credFile, "PROVIDER_API_KEY="+secret+"\n")

	// A provider echoing the key it was handed - exactly the case an error
	// stored in the database (and shown in the UI) must not carry.
	planErr := errors.New("request rejected: key " + secret + " is not authorized")
	e, _ := newTestExecutor(t, &fakeClient{planErr: planErr})

	_, err := e.RunCheck(context.Background(), CheckInput{
		SourcePath:        source,
		BinaryKind:        "terraform",
		CredentialEnvFile: credFile,
	})
	if err == nil {
		t.Fatal("RunCheck succeeded, want the plan error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaked the credential value: %v", err)
	}
	if !strings.Contains(err.Error(), scrubPlaceholder) {
		t.Errorf("error = %q, want it to show the value was redacted", err)
	}
	if !errors.Is(err, planErr) {
		t.Error("errors.Is no longer matches the original error")
	}
}

func TestSweepStaleRemovesOnlyCheckDirectories(t *testing.T) {
	base := t.TempDir()
	e, err := NewExecutor(base, t.TempDir())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	writeFile(t, filepath.Join(base, "check-aaa", "plan.out"), "unredacted plan from a killed check")
	writeFile(t, filepath.Join(base, "check-bbb", "nested", "main.tf"), "x")
	writeFile(t, filepath.Join(base, "unrelated-dir", "keep.txt"), "x")
	writeFile(t, filepath.Join(base, "check-looks-like-a-file"), "x")

	removed, err := e.SweepStale()
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}

	for _, gone := range []string{"check-aaa", "check-bbb"} {
		if _, err := os.Stat(filepath.Join(base, gone)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists after SweepStale", gone)
		}
	}
	for _, kept := range []string{"unrelated-dir", "check-looks-like-a-file"} {
		if _, err := os.Stat(filepath.Join(base, kept)); err != nil {
			t.Errorf("%s was removed, but SweepStale must only remove check directories: %v", kept, err)
		}
	}
}

func TestRunCheckRejectsSourceContainingScratchDirectory(t *testing.T) {
	// A workspace pointed at a parent of the data directory: copying it
	// would recurse into the scratch directory being filled.
	parent := t.TempDir()
	writeFile(t, filepath.Join(parent, "main.tf"), "resource")

	e, err := NewExecutor(filepath.Join(parent, "data", "tmp"), filepath.Join(parent, "data", "plugin-cache"))
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	clientBuilt := false
	e.newClient = func(_, _ string, _ map[string]string) (client, error) {
		clientBuilt = true
		return &fakeClient{plan: &tfjson.Plan{}}, nil
	}

	_, err = e.RunCheck(context.Background(), CheckInput{SourcePath: parent, BinaryKind: "terraform"})
	if err == nil {
		t.Fatal("RunCheck accepted a source path that contains its own scratch directory")
	}
	if clientBuilt {
		t.Error("a client was built despite the rejected source path")
	}
}

func TestRunCheckLetsInitReuseTheSharedPluginCache(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")

	e, captured := newTestExecutor(t, &fakeClient{plan: &tfjson.Plan{}})
	if _, err := e.RunCheck(context.Background(), CheckInput{SourcePath: source, BinaryKind: "terraform"}); err != nil {
		t.Fatalf("RunCheck: %v", err)
	}

	if captured.env["TF_PLUGIN_CACHE_DIR"] != e.pluginCacheDir {
		t.Errorf("TF_PLUGIN_CACHE_DIR = %q, want the executor's shared cache %q", captured.env["TF_PLUGIN_CACHE_DIR"], e.pluginCacheDir)
	}
	// Without this, init rewrites a cached provider binary another check may
	// be executing ("text file busy"), failing concurrent checks at random.
	if captured.env["TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE"] != "true" {
		t.Errorf("TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE = %q, want true", captured.env["TF_PLUGIN_CACHE_MAY_BREAK_DEPENDENCY_LOCK_FILE"])
	}
}

// initTrackingClient records how many Init calls overlap, and can hold one.
type initTrackingClient struct {
	running, peak atomic.Int32
	hold          chan struct{} // if non-nil, Init blocks until it is closed
	entered       chan struct{} // if non-nil, signalled when an Init starts
}

func (c *initTrackingClient) Init(ctx context.Context) error {
	now := c.running.Add(1)
	defer c.running.Add(-1)
	for {
		peak := c.peak.Load()
		if now <= peak || c.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	if c.entered != nil {
		c.entered <- struct{}{}
	}
	if c.hold != nil {
		select {
		case <-c.hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	} else {
		time.Sleep(30 * time.Millisecond) // long enough for concurrent callers to overlap if they could
	}
	return nil
}

func (c *initTrackingClient) PlanRefreshOnly(context.Context, string) error { return nil }

func (c *initTrackingClient) ShowPlanFile(context.Context, string) (*tfjson.Plan, error) {
	return &tfjson.Plan{}, nil
}

func newExecutorWithClient(t *testing.T, c client) *Executor {
	t.Helper()
	e, err := NewExecutor(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	e.newClient = func(string, string, map[string]string) (client, error) { return c, nil }
	return e
}

func TestInitRunsOneCheckAtATime(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")
	tracker := &initTrackingClient{}
	e := newExecutorWithClient(t, tracker)

	const checks = 6
	errs := make(chan error, checks)
	for range checks {
		go func() {
			_, err := e.RunCheck(context.Background(), CheckInput{SourcePath: source, BinaryKind: "terraform"})
			errs <- err
		}()
	}
	for range checks {
		if err := <-errs; err != nil {
			t.Errorf("RunCheck: %v", err)
		}
	}

	// Two inits installing the same new provider at once collide.
	if peak := tracker.peak.Load(); peak != 1 {
		t.Errorf("up to %d inits ran at the same time, want 1", peak)
	}
}

func TestACheckQueuedForInitGivesUpWhenCancelled(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")
	tracker := &initTrackingClient{hold: make(chan struct{}), entered: make(chan struct{}, 2)}
	e := newExecutorWithClient(t, tracker)

	// The first check takes the init slot and holds it.
	firstDone := make(chan error, 1)
	go func() {
		_, err := e.RunCheck(context.Background(), CheckInput{SourcePath: source, BinaryKind: "terraform"})
		firstDone <- err
	}()
	<-tracker.entered

	// A second one queues behind it, then is cancelled while still waiting.
	ctx, cancel := context.WithCancel(context.Background())
	secondDone := make(chan error, 1)
	go func() {
		_, err := e.RunCheck(ctx, CheckInput{SourcePath: source, BinaryKind: "terraform"})
		secondDone <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("queued check error = %v, want it to match context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cancelled check kept waiting for the init slot")
	}

	close(tracker.hold)
	if err := <-firstDone; err != nil {
		t.Errorf("first RunCheck: %v", err)
	}
	if tracker.peak.Load() != 1 {
		t.Errorf("peak concurrent inits = %d, want 1", tracker.peak.Load())
	}
}

func TestRunCheckPropagatesInitError(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")

	wantErr := errors.New("boom")
	fake := &fakeClient{initErr: wantErr}
	e, _ := newTestExecutor(t, fake)

	_, err := e.RunCheck(context.Background(), CheckInput{SourcePath: source, BinaryKind: "terraform"})
	if !errors.Is(err, wantErr) {
		t.Errorf("RunCheck error = %v, want it to wrap %v", err, wantErr)
	}
}

func TestRunCheckRejectsWorkingSubdirectoryEscapingSource(t *testing.T) {
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "resource")

	fake := &fakeClient{plan: &tfjson.Plan{}}
	e, _ := newTestExecutor(t, fake)

	_, err := e.RunCheck(context.Background(), CheckInput{
		SourcePath:          source,
		WorkingSubdirectory: "../../../../etc",
		BinaryKind:          "terraform",
	})
	if err == nil {
		t.Fatal("RunCheck did not reject a working subdirectory that escapes the isolated source copy")
	}
	if fake.initCalled {
		t.Error("Init was called despite the escaping working subdirectory - should have been rejected first")
	}
}

func TestNewClientRejectsUnsupportedBinaryKind(t *testing.T) {
	_, err := newClient("pulumi", t.TempDir(), nil)
	var target *UnsupportedBinaryKindError
	if !errors.As(err, &target) {
		t.Errorf("newClient error = %v, want *UnsupportedBinaryKindError", err)
	}
}

func assertContains(t *testing.T, haystack []string, want string) {
	t.Helper()
	for _, v := range haystack {
		if v == want {
			return
		}
	}
	t.Errorf("expected %v to contain %q", haystack, want)
}

func assertNotContains(t *testing.T, haystack []string, unwanted string) {
	t.Helper()
	for _, v := range haystack {
		if v == unwanted {
			t.Errorf("expected %v to not contain %q", haystack, unwanted)
		}
	}
}
