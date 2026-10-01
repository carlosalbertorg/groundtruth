package checks

import (
	"context"
	"database/sql"
	"errors"
	"sync"
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
	return newNamedTestWorkspace(t, queries, "test-workspace")
}

func newNamedTestWorkspace(t *testing.T, queries *sqlc.Queries, name string) sqlc.Workspace {
	t.Helper()
	ws, err := queries.CreateWorkspace(context.Background(), sqlc.CreateWorkspaceParams{
		ID:                   uuid.NewString(),
		Name:                 name,
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

// blockingExecutor holds every check "in flight" until release is closed,
// announcing each arrival on entered, so a test can run something else while
// a check is provably mid-run.
type blockingExecutor struct {
	entered chan struct{}
	release chan struct{}
}

func newBlockingExecutor() *blockingExecutor {
	return &blockingExecutor{entered: make(chan struct{}, 8), release: make(chan struct{})}
}

func (b *blockingExecutor) RunCheck(ctx context.Context, _ terraform.CheckInput) (*tfjson.Plan, error) {
	b.entered <- struct{}{}
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return &tfjson.Plan{}, nil
}

func waitForEntry(t *testing.T, b *blockingExecutor) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a check to reach the executor")
	}
}

type recordingNotifier struct {
	mu       sync.Mutex
	previous []string
}

func (n *recordingNotifier) Notify(_ context.Context, _ sqlc.Workspace, _ sqlc.DriftCheck, previousStatus string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.previous = append(n.previous, previousStatus)
}

func TestRunRejectsAnOverlappingCheckOfTheSameWorkspace(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws := newTestWorkspace(t, queries)
	exec := newBlockingExecutor()
	svc := &Service{queries: queries, executor: exec}

	firstDone := make(chan error, 1)
	go func() {
		_, err := svc.Run(context.Background(), ws, TriggeredBySchedule)
		firstDone <- err
	}()
	waitForEntry(t, exec) // the first check now holds the workspace

	if _, err := svc.Run(context.Background(), ws, TriggeredByManual); !errors.Is(err, ErrCheckInProgress) {
		t.Fatalf("overlapping Run error = %v, want ErrCheckInProgress", err)
	}

	close(exec.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Run: %v", err)
	}

	// Once the first check is done the workspace is free again.
	if _, err := svc.Run(context.Background(), ws, TriggeredByManual); err != nil {
		t.Fatalf("Run after the first finished: %v", err)
	}
}

func TestRunDoesNotBlockOtherWorkspaces(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	first := newNamedTestWorkspace(t, queries, "first")
	second := newNamedTestWorkspace(t, queries, "second")
	exec := newBlockingExecutor()
	svc := &Service{queries: queries, executor: exec}

	errs := make(chan error, 2)
	for _, ws := range []sqlc.Workspace{first, second} {
		go func() {
			_, err := svc.Run(context.Background(), ws, TriggeredBySchedule)
			errs <- err
		}()
	}
	// Both reach the executor at the same time: the guard is per workspace,
	// not global.
	waitForEntry(t, exec)
	waitForEntry(t, exec)

	close(exec.release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("Run: %v", err)
		}
	}
}

func TestRunReleasesTheWorkspaceAfterAFailedRun(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws := newTestWorkspace(t, queries)
	svc := &Service{queries: queries, executor: &fakeExecutor{err: errors.New("boom")}}

	for i := range 2 {
		if _, err := svc.Run(context.Background(), ws, TriggeredByManual); err != nil {
			t.Fatalf("Run #%d: %v (a failed terraform run must not leave the workspace marked busy)", i+1, err)
		}
	}
}

func TestRunJudgesTheTransitionAgainstTheLatestStoredStatus(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws := newTestWorkspace(t, queries) // this copy was taken before any check: status NULL

	// Meanwhile another check finished and left the workspace drifted - the
	// situation of a scheduled check that waited behind the concurrency limit
	// while a manual one ran.
	if err := queries.UpdateWorkspaceLastCheck(context.Background(), sqlc.UpdateWorkspaceLastCheckParams{
		LastCheckID:     sql.NullString{String: "an-earlier-check", Valid: true},
		LastCheckStatus: sql.NullString{String: StatusDrifted, Valid: true},
		ID:              ws.ID,
	}); err != nil {
		t.Fatalf("UpdateWorkspaceLastCheck: %v", err)
	}

	notifier := &recordingNotifier{}
	svc := &Service{queries: queries, executor: &fakeExecutor{plan: planWithOneCreate()}, notifier: notifier}
	if _, err := svc.Run(context.Background(), ws, TriggeredBySchedule); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(notifier.previous) != 1 {
		t.Fatalf("Notify called %d times, want 1", len(notifier.previous))
	}
	// "drifted", not the stale "": drifted -> drifted is silent, whereas
	// "" -> drifted would have announced the same change a second time.
	if notifier.previous[0] != StatusDrifted {
		t.Errorf("previous status passed to Notify = %q, want %q", notifier.previous[0], StatusDrifted)
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
