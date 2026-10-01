package terraform

// These tests drive the real terraform-exec and tofu-exec libraries against a
// stand-in for the CLI: a shell script that records how it was invoked and
// answers just enough (version, init, plan, show) for RunCheck to complete.
// They need a POSIX shell and Linux process semantics, hence the _linux
// suffix; the cases that need a real Terraform live in integration_test.go.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// installFakeCLI puts an executable called name first on PATH and returns the
// file where it logs its arguments, one invocation per line. planBody is the
// shell run when the CLI is asked to plan.
func installFakeCLI(t *testing.T, name, planBody string) (logPath string) {
	t.Helper()

	dir := t.TempDir()
	logPath = filepath.Join(dir, "invocations.log")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  version) echo '{"terraform_version":"1.9.0","platform":"linux_amd64","provider_selections":{},"terraform_outdated":false}' ;;
  plan) %s ;;
  show) echo '{"format_version":"1.2","resource_drift":[]}' ;;
esac
`, logPath, planBody)

	// A test double has to be executable; 0o700 keeps it to this user.
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil { // #nosec G306
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// invocation returns the logged argument line of the first call to subcommand.
func invocation(t *testing.T, logPath, subcommand string) string {
	t.Helper()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, subcommand+" ") || line == subcommand {
			return line
		}
	}
	t.Fatalf("%q was never invoked; log:\n%s", subcommand, data)
	return ""
}

func moduleWithMainTF(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "main.tf"), "# the fake CLI ignores the module")
	return source
}

func TestRefreshOnlyPlanNeverTakesTheStateLock(t *testing.T) {
	for _, kind := range []string{"terraform", "tofu"} {
		t.Run(kind, func(t *testing.T) {
			logPath := installFakeCLI(t, kind, "exit 0")
			e, err := NewExecutor(t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatalf("NewExecutor: %v", err)
			}

			if _, err := e.RunCheck(context.Background(), CheckInput{SourcePath: moduleWithMainTF(t), BinaryKind: kind}); err != nil {
				t.Fatalf("RunCheck: %v", err)
			}

			plan := invocation(t, logPath, "plan")
			// -lock=true (the libraries' default) would write a lock to the backend
			// for the whole check: it blocks the pipeline's own apply, and a check
			// killed on a timeout leaves it behind.
			if !strings.Contains(plan, "-lock=false") {
				t.Errorf("plan ran as %q, want it to pass -lock=false", plan)
			}
			if strings.Contains(plan, "-lock=true") {
				t.Errorf("plan ran as %q, which takes the state lock", plan)
			}
			if !strings.Contains(plan, "-refresh-only") {
				t.Errorf("plan ran as %q, want -refresh-only", plan)
			}
		})
	}
}

// A Terraform that doesn't stop when asked to (here: ignores SIGINT, as one
// blocked on a slow provider call effectively does) must still be killed, and
// the check's scratch directory removed, a bounded time after cancellation.
func TestCancelledCheckEndsEvenIfTerraformIgnoresTheInterrupt(t *testing.T) {
	installFakeCLI(t, "terraform", "trap '' INT; exec sleep 30")
	defer func(previous time.Duration) { killGrace = previous }(killGrace)
	killGrace = 300 * time.Millisecond

	scratch := t.TempDir()
	e, err := NewExecutor(scratch, t.TempDir())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(700*time.Millisecond, cancel)

	start := time.Now()
	_, err = e.RunCheck(ctx, CheckInput{SourcePath: moduleWithMainTF(t), BinaryKind: "terraform"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("RunCheck succeeded although it was cancelled mid-plan")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to match context.Canceled", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("RunCheck took %v after cancellation: the stubborn Terraform was not killed after the grace period", elapsed)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("scratch directory still holds %d entries after the cancelled check returned", len(entries))
	}
}

// OpenTofu has no graceful stage: tofu-exec kills the process on cancellation.
func TestCancelledCheckEndsAtOnceForOpenTofu(t *testing.T) {
	installFakeCLI(t, "tofu", "exec sleep 30")

	e, err := NewExecutor(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(700*time.Millisecond, cancel)

	start := time.Now()
	_, err = e.RunCheck(ctx, CheckInput{SourcePath: moduleWithMainTF(t), BinaryKind: "tofu"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("RunCheck succeeded although it was cancelled mid-plan")
	}
	if elapsed > 10*time.Second {
		t.Errorf("RunCheck took %v after cancellation, want it to return promptly", elapsed)
	}
}
