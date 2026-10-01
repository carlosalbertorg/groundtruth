//go:build integration

// Integration tier: exercises the real terraform binary end-to-end
// (init -> plan -refresh-only -> show -> drift.FromPlan), proving the
// whole pipeline works without any cloud provider or credentials. Needs
// a real `terraform` on PATH - see .github/workflows/ci.yml, which
// installs one via hashicorp/setup-terraform before running this tier.
package terraform_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"

	"github.com/carlosalbertorg/groundtruth/internal/drift"
	"github.com/carlosalbertorg/groundtruth/internal/terraform"
)

// setUpRealWorkspace writes a fixture module using the "local" backend
// (pointed at a path outside the module, standing in for the remote
// backend - S3/GCS/TFC - a real workspace would use) and a single
// local_file resource (standing in for "a real managed object"), then
// runs a genuine `terraform init && terraform apply` against it - the
// one-time setup step a real operator would do themselves, outside of
// groundtruth, before ever pointing a workspace at this module.
//
// It returns the module's source directory (what a Workspace.SourcePath
// would be) and the path to the file local_file manages (standing in for
// "the real world," which the test later mutates to simulate drift).
func setUpRealWorkspace(t *testing.T) (sourceDir, managedFilePath string) {
	t.Helper()

	execPath, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("terraform not found on PATH; skipping integration test")
	}

	sourceDir = t.TempDir()
	backendStatePath := filepath.Join(t.TempDir(), "terraform.tfstate")
	managedFilePath = filepath.Join(t.TempDir(), "managed-file.txt")

	config := fmt.Sprintf(`
terraform {
  backend "local" {
    path = %q
  }
}

resource "local_file" "example" {
  filename = %q
  content  = "hello"
}
`, filepath.ToSlash(backendStatePath), filepath.ToSlash(managedFilePath))

	if err := os.WriteFile(filepath.Join(sourceDir, "main.tf"), []byte(config), 0o600); err != nil {
		t.Fatalf("write fixture module: %v", err)
	}

	tf, err := tfexec.NewTerraform(sourceDir, execPath)
	if err != nil {
		t.Fatalf("tfexec.NewTerraform: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := tf.Init(ctx); err != nil {
		t.Fatalf("initial terraform init: %v", err)
	}
	if err := tf.Apply(ctx); err != nil {
		t.Fatalf("initial terraform apply: %v", err)
	}

	return sourceDir, managedFilePath
}

// An `apply` holds the backend's state lock for as long as it runs. A drift
// check that overlaps one - a scheduled check meeting the operator's own
// pipeline - must neither fail on that lock nor take a lock the apply would
// then collide with: groundtruth only ever reads.
func TestRealTerraformCheckIgnoresTheStateLockOfARunningApply(t *testing.T) {
	execPath, err := exec.LookPath("terraform")
	if err != nil {
		t.Skip("terraform not found on PATH; skipping integration test")
	}

	sourceDir := t.TempDir()
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "terraform.tfstate")

	// terraform_data is built in, so nothing is downloaded. Its provisioner
	// keeps the apply - and with it the state lock - going for ten seconds.
	config := fmt.Sprintf(`
terraform {
  backend "local" {
    path = %q
  }
}

resource "terraform_data" "hold" {
  provisioner "local-exec" {
    command = "sleep 10"
  }
}
`, filepath.ToSlash(statePath))
	if err := os.WriteFile(filepath.Join(sourceDir, "main.tf"), []byte(config), 0o600); err != nil {
		t.Fatalf("write fixture module: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	tf, err := tfexec.NewTerraform(sourceDir, execPath)
	if err != nil {
		t.Fatalf("tfexec.NewTerraform: %v", err)
	}
	if err := tf.Init(ctx); err != nil {
		t.Fatalf("terraform init: %v", err)
	}

	applyDone := make(chan error, 1)
	go func() { applyDone <- tf.Apply(ctx) }()

	// The local backend records a held lock in a *.lock.info file beside the
	// state. Wait for it, so the check below really does overlap the apply.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if held, _ := filepath.Glob(filepath.Join(stateDir, "*.lock.info")); len(held) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the apply never took the state lock")
		}
		time.Sleep(100 * time.Millisecond)
	}

	executor, err := terraform.NewExecutor(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	if _, err := executor.RunCheck(ctx, terraform.CheckInput{
		SourcePath: sourceDir,
		BinaryKind: "terraform",
		Timeout:    90 * time.Second,
	}); err != nil {
		t.Fatalf("a check failed while an apply held the state lock - does it still take the lock itself? %v", err)
	}

	if err := <-applyDone; err != nil {
		t.Fatalf("the apply failed: %v - did the check's lock collide with it?", err)
	}
}

func TestRealTerraformPlanDetectsExternalDeletion(t *testing.T) {
	sourceDir, managedFilePath := setUpRealWorkspace(t)

	executor, err := terraform.NewExecutor(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	runCheck := func() drift.Result {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		plan, err := executor.RunCheck(ctx, terraform.CheckInput{
			SourcePath: sourceDir,
			BinaryKind: "terraform",
			Timeout:    90 * time.Second,
		})
		if err != nil {
			t.Fatalf("RunCheck: %v", err)
		}
		return drift.FromPlan(plan)
	}

	// Before any external change: the real pipeline (real binary, real
	// isolated copy, real backend round-trip) must report no drift.
	before := runCheck()
	if before.Drifted() {
		t.Fatalf("expected no drift before any external change, got %+v", before)
	}

	// Simulate drift: delete the file groundtruth never touched or
	// knew about, entirely outside of Terraform.
	if err := os.Remove(managedFilePath); err != nil {
		t.Fatalf("remove managed file: %v", err)
	}

	after := runCheck()
	if !after.Drifted() {
		t.Fatalf("expected drift after external deletion, got %+v", after)
	}

	found := false
	for _, r := range after.Resources {
		if r.Address == "local_file.example" {
			found = true
			t.Logf("detected drift on %s: action=%s", r.Address, r.Action)
		}
	}
	if !found {
		t.Errorf("expected a drift entry for local_file.example, got %+v", after.Resources)
	}
}
