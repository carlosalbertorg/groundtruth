package terraform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
