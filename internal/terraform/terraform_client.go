package terraform

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

type terraformClient struct {
	tf *tfexec.Terraform
}

func newTerraformClient(workDir string, env map[string]string) (client, error) {
	execPath, err := exec.LookPath("terraform")
	if err != nil {
		return nil, fmt.Errorf("terraform binary not found on PATH: %w", err)
	}

	tf, err := tfexec.NewTerraform(workDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("init terraform client: %w", err)
	}
	if err := tf.SetEnv(env); err != nil {
		return nil, fmt.Errorf("set terraform environment: %w", err)
	}
	// See killGrace. Not supported on Windows, where terraform-exec has no
	// graceful cancellation to bound and kills the process at once anyway.
	if runtime.GOOS != "windows" {
		if err := tf.SetWaitDelay(killGrace); err != nil {
			return nil, fmt.Errorf("set terraform wait delay: %w", err)
		}
	}

	return &terraformClient{tf: tf}, nil
}

func (c *terraformClient) Init(ctx context.Context) error {
	// Upgrade(false): never let a scheduled drift check silently bump
	// provider/module versions. That's a deliberate operator action, not
	// something that should happen as a side effect of checking drift.
	//
	// It only holds the versions a committed .terraform.lock.hcl pins
	// (copyModuleSource copies that file in). Every check runs in a fresh
	// directory, so a module without a lock file has nothing to hold it back
	// and init resolves the newest versions its constraints allow - see
	// "Detection model and its limits" in docs/ARCHITECTURE.md.
	return c.tf.Init(ctx, tfexec.Upgrade(false))
}

func (c *terraformClient) PlanRefreshOnly(ctx context.Context, outPath string) error {
	// Lock(false): a check only reads. terraform-exec otherwise passes
	// -lock=true, which writes a lock record to the backend for the whole
	// check - so an `apply` from the operator's pipeline that overlaps one
	// fails to get the lock, and a check killed on a timeout leaves a stale
	// lock that blocks the pipeline until someone force-unlocks it. Reading
	// without the lock can at worst see state halfway through an apply and
	// report drift that the next check no longer sees.
	_, err := c.tf.Plan(ctx, tfexec.Out(outPath), tfexec.RefreshOnly(true), tfexec.Lock(false))
	return err
}

func (c *terraformClient) ShowPlanFile(ctx context.Context, path string) (*tfjson.Plan, error) {
	return c.tf.ShowPlanFile(ctx, path)
}
