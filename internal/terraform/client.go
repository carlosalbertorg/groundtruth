// Package terraform runs terraform/tofu plans safely: each check gets an
// isolated, disposable working directory, credentials are read fresh
// from an operator-supplied file and never persisted, and no shell is
// ever invoked.
package terraform

import (
	"context"

	tfjson "github.com/hashicorp/terraform-json"
)

// client is the minimal surface this package needs from either
// hashicorp/terraform-exec or opentofu/tofu-exec.
//
// Those two libraries are structurally near-identical but each declares
// its own InitOption/PlanOption/ShowOption types inside a package that
// (confusingly) both name "tfexec" - so Terraform.Init(ctx, ...InitOption)
// and Tofu.Init(ctx, ...InitOption) take parameters of two different,
// incompatible named types and cannot satisfy one shared Go interface
// directly. The adapters in terraform_client.go and tofu_client.go
// normalize both libraries to this fixed, options-free interface, which
// is all the executor actually needs.
type client interface {
	Init(ctx context.Context) error
	PlanRefreshOnly(ctx context.Context, outPath string) error
	ShowPlanFile(ctx context.Context, path string) (*tfjson.Plan, error)
}

// newClient builds a client for the given binary kind, pointed at
// workDir, with env as its complete subprocess environment.
func newClient(binaryKind, workDir string, env map[string]string) (client, error) {
	switch binaryKind {
	case "terraform":
		return newTerraformClient(workDir, env)
	case "tofu":
		return newTofuClient(workDir, env)
	default:
		return nil, &UnsupportedBinaryKindError{Kind: binaryKind}
	}
}

// UnsupportedBinaryKindError is returned when a workspace's binary_kind
// isn't one newClient knows how to run - defensive, since the HTTP layer
// already validates this before a workspace can be saved.
type UnsupportedBinaryKindError struct {
	Kind string
}

func (e *UnsupportedBinaryKindError) Error() string {
	return "unsupported binary kind: " + e.Kind
}
