package checks

import (
	"context"
	"errors"
	"testing"
	"time"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/store/storetest"
	"github.com/carlosalbertorg/groundtruth/internal/terraform"
	"github.com/google/uuid"
)

type fakeExecutor struct {
	plan *tfjson.Plan
	err  error
}

func (f *fakeExecutor) RunCheck(_ context.Context, _ terraform.CheckInput) (*tfjson.Plan, error) {
	return f.plan, f.err
}

func newTestWorkspace(t *testing.T, queries *sqlc.Queries) sqlc.Workspace {
	t.Helper()
	ws, err := queries.CreateWorkspace(context.Background(), sqlc.CreateWorkspaceParams{
		ID:                   uuid.NewString(),
		Name:                 "test-workspace",
		SourcePath:           "/data/modules/test",
		BinaryKind:           "terraform",
		CheckIntervalMinutes: 60,
		CheckTimeoutSeconds:  600,
		IsEnabled:            true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	})
	if err != nil {
		t.Fatalf("create test workspace: %v", err)
	}
	return ws
}

func planWithOneCreate() *tfjson.Plan {
	return &tfjson.Plan{
		ResourceDrift: []*tfjson.ResourceChange{
			{
				Address: "null_resource.example",
				Type:    "null_resource",
				Change: &tfjson.Change{
					Actions: tfjson.Actions{tfjson.ActionCreate},
					After:   map[string]any{"id": "123"},
				},
			},
		},
	}
}

func TestRunPersistsCleanCheck(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws := newTestWorkspace(t, queries)
	svc := &Service{queries: queries, executor: &fakeExecutor{plan: &tfjson.Plan{}}}

	result, err := svc.Run(context.Background(), ws, TriggeredByManual)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Check.Status != StatusClean {
		t.Errorf("status = %q, want %q", result.Check.Status, StatusClean)
	}
	if len(result.Resources) != 0 {
		t.Errorf("resources = %v, want none", result.Resources)
	}

	updated, err := queries.GetWorkspace(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if !updated.LastCheckID.Valid || updated.LastCheckID.String != result.Check.ID {
		t.Errorf("workspace.last_check_id = %v, want %q", updated.LastCheckID, result.Check.ID)
	}
	if updated.LastCheckStatus.String != StatusClean {
		t.Errorf("workspace.last_check_status = %v, want %q", updated.LastCheckStatus, StatusClean)
	}
}

func TestRunPersistsDriftedCheckWithResources(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws := newTestWorkspace(t, queries)
	svc := &Service{queries: queries, executor: &fakeExecutor{plan: planWithOneCreate()}}

	result, err := svc.Run(context.Background(), ws, TriggeredByManual)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Check.Status != StatusDrifted {
		t.Errorf("status = %q, want %q", result.Check.Status, StatusDrifted)
	}
	if result.Check.ResourcesAdded != 1 {
		t.Errorf("resources_added = %d, want 1", result.Check.ResourcesAdded)
	}
	if len(result.Resources) != 1 || result.Resources[0].Address != "null_resource.example" {
		t.Fatalf("resources = %+v", result.Resources)
	}

	persisted, err := queries.ListDriftResourcesForCheck(context.Background(), result.Check.ID)
	if err != nil {
		t.Fatalf("ListDriftResourcesForCheck: %v", err)
	}
	if len(persisted) != 1 {
		t.Fatalf("persisted resources = %d, want 1", len(persisted))
	}
	if persisted[0].AfterJson.String != `{"id":"123"}` {
		t.Errorf("after_json = %q, want {\"id\":\"123\"}", persisted[0].AfterJson.String)
	}
}

func TestRunPersistsFailedCheck(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws := newTestWorkspace(t, queries)
	wantErr := errors.New("boom")
	svc := &Service{queries: queries, executor: &fakeExecutor{err: wantErr}}

	result, err := svc.Run(context.Background(), ws, TriggeredByManual)
	if err != nil {
		t.Fatalf("Run returned an error for a failed *terraform* run: %v (should be recorded as a failed check, not a Go error)", err)
	}
	if result.Check.Status != StatusFailed {
		t.Errorf("status = %q, want %q", result.Check.Status, StatusFailed)
	}
	if !result.Check.ErrorMessage.Valid || result.Check.ErrorMessage.String != "boom" {
		t.Errorf("error_message = %v, want %q", result.Check.ErrorMessage, "boom")
	}

	updated, err := queries.GetWorkspace(context.Background(), ws.ID)
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if updated.LastCheckStatus.String != StatusFailed {
		t.Errorf("workspace.last_check_status = %v, want %q", updated.LastCheckStatus, StatusFailed)
	}
}

func TestRunHistoryIsOrderedNewestFirst(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws := newTestWorkspace(t, queries)
	svc := &Service{queries: queries, executor: &fakeExecutor{plan: &tfjson.Plan{}}}

	var ids []string
	for range 3 {
		result, err := svc.Run(context.Background(), ws, TriggeredByManual)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		ids = append(ids, result.Check.ID)
		// Keep started_at strictly increasing: Windows' wall clock ticks at
		// ~15ms, so anything shorter can still produce a tie.
		time.Sleep(20 * time.Millisecond)
	}

	history, err := queries.ListDriftChecksForWorkspace(context.Background(), sqlc.ListDriftChecksForWorkspaceParams{
		WorkspaceID: ws.ID,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListDriftChecksForWorkspace: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3", len(history))
	}
	if history[0].ID != ids[2] || history[2].ID != ids[0] {
		t.Errorf("history not newest-first: got IDs %v, want reverse of %v", []string{history[0].ID, history[1].ID, history[2].ID}, ids)
	}
}
