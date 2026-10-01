package terraform

import (
	"context"
	"fmt"
	"os/exec"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/opentofu/tofu-exec/tfexec"
)

type tofuClient struct {
	tf *tfexec.Tofu
}

func newTofuClient(workDir string, env map[string]string) (client, error) {
	execPath, err := exec.LookPath("tofu")
	if err != nil {
		return nil, fmt.Errorf("tofu binary not found on PATH: %w", err)
	}

	tf, err := tfexec.NewTofu(workDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("init tofu client: %w", err)
	}
	if err := tf.SetEnv(env); err != nil {
		return nil, fmt.Errorf("set tofu environment: %w", err)
	}

	return &tofuClient{tf: tf}, nil
}

func (c *tofuClient) Init(ctx context.Context) error {
	return c.tf.Init(ctx, tfexec.Upgrade(false))
}

func (c *tofuClient) PlanRefreshOnly(ctx context.Context, outPath string) error {
	_, err := c.tf.Plan(ctx, tfexec.Out(outPath), tfexec.RefreshOnly(true))
	return err
}

func (c *tofuClient) ShowPlanFile(ctx context.Context, path string) (*tfjson.Plan, error) {
	return c.tf.ShowPlanFile(ctx, path)
}
