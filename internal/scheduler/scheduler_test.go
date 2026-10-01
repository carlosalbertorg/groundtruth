package scheduler

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/checks"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

type fakeLister struct {
	rows []sqlc.ListEnabledWorkspacesWithLastCheckRow
}

func (f *fakeLister) ListEnabledWorkspacesWithLastCheck(context.Context) ([]sqlc.ListEnabledWorkspacesWithLastCheckRow, error) {
	return f.rows, nil
}

// fakeRunner records every workspace ID it was called for and, if
// gate is non-nil, blocks until the test sends on it - letting tests
// hold a check "in flight" to observe concurrency behavior.
type fakeRunner struct {
	gate chan struct{}

	mu        sync.Mutex
	calledFor []string

	concurrent     atomic.Int32
	peakConcurrent atomic.Int32
}

func (f *fakeRunner) Run(ctx context.Context, ws sqlc.Workspace, _ string) (checks.Result, error) {
	f.mu.Lock()
	f.calledFor = append(f.calledFor, ws.ID)
	f.mu.Unlock()

	cur := f.concurrent.Add(1)
	defer f.concurrent.Add(-1)
	for {
		peak := f.peakConcurrent.Load()
		if cur <= peak || f.peakConcurrent.CompareAndSwap(peak, cur) {
			break
		}
	}

	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
		}
	}
	return checks.Result{Check: sqlc.DriftCheck{ID: "x", WorkspaceID: ws.ID}}, nil
}

func (f *fakeRunner) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calledFor...)
}

func workspaceRow(id string, lastCheckStartedAt *time.Time, intervalMinutes int64) sqlc.ListEnabledWorkspacesWithLastCheckRow {
	row := sqlc.ListEnabledWorkspacesWithLastCheckRow{
		ID:                   id,
		Name:                 id,
		BinaryKind:           "terraform",
		SourcePath:           "/data/" + id,
		CheckIntervalMinutes: intervalMinutes,
		CheckTimeoutSeconds:  600,
		IsEnabled:            true,
	}
	if lastCheckStartedAt != nil {
		row.LastCheckStartedAt = sql.NullTime{Time: *lastCheckStartedAt, Valid: true}
	}
	return row
}

func newTestScheduler(lister workspaceLister, runner checkRunner, now time.Time, maxConcurrent int) *Scheduler {
	return &Scheduler{
		queries:       lister,
		runner:        runner,
		logger:        slog.New(slog.DiscardHandler),
		now:           func() time.Time { return now },
		pollInterval:  10 * time.Millisecond,
		maxConcurrent: maxConcurrent,
	}
}

func newRunState(maxConcurrent int) *runState {
	return &runState{
		sem:     make(chan struct{}, maxConcurrent),
		running: make(map[string]bool),
	}
}

func TestTickTriggersOnlyDueWorkspaces(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	never := workspaceRow("never-checked", nil, 60)
	recentlyChecked := workspaceRow("recently-checked", ptr(now.Add(-5*time.Minute)), 60)
	overdue := workspaceRow("overdue", ptr(now.Add(-61*time.Minute)), 60)

	runner := &fakeRunner{}
	s := newTestScheduler(&fakeLister{rows: []sqlc.ListEnabledWorkspacesWithLastCheckRow{
		never, recentlyChecked, overdue,
	}}, runner, now, 10)

	state := newRunState(10)
	s.tick(context.Background(), state)
	state.wg.Wait()

	calls := runner.calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want exactly the 2 due workspaces", calls)
	}
	for _, want := range []string{"never-checked", "overdue"} {
		if !contains(calls, want) {
			t.Errorf("expected a call for %q, got %v", want, calls)
		}
	}
	if contains(calls, "recently-checked") {
		t.Errorf("recently-checked workspace should not have been triggered: %v", calls)
	}
}

func TestTickSkipsAWorkspaceAlreadyRunning(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	due := workspaceRow("due", nil, 60)

	runner := &fakeRunner{}
	s := newTestScheduler(&fakeLister{rows: []sqlc.ListEnabledWorkspacesWithLastCheckRow{due}}, runner, now, 10)

	state := newRunState(10)
	state.running["due"] = true // simulate: already in flight from a previous tick

	s.tick(context.Background(), state)
	state.wg.Wait()

	if len(runner.calls()) != 0 {
		t.Errorf("calls = %v, want none (workspace was already running)", runner.calls())
	}
}

func TestTickNeverExceedsMaxConcurrent(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var rows []sqlc.ListEnabledWorkspacesWithLastCheckRow
	for i := 0; i < 6; i++ {
		rows = append(rows, workspaceRow(string(rune('a'+i)), nil, 60))
	}

	runner := &fakeRunner{gate: make(chan struct{})}
	s := newTestScheduler(&fakeLister{rows: rows}, runner, now, 2)

	state := newRunState(2)
	s.tick(context.Background(), state)

	// Let every goroutine reach the gate (or decide not to run at all).
	deadline := time.After(2 * time.Second)
	for runner.concurrent.Load() != 2 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for 2 concurrent checks to start")
		case <-time.After(time.Millisecond):
		}
	}

	close(runner.gate)
	state.wg.Wait()

	if peak := runner.peakConcurrent.Load(); peak > 2 {
		t.Errorf("peak concurrent checks = %d, want <= 2", peak)
	}
	if len(runner.calls()) != 6 {
		t.Errorf("calls = %d, want all 6 eventually run", len(runner.calls()))
	}
}

func TestRunStopsOnCancellationAndWaitsForInFlightChecks(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	due := workspaceRow("due", nil, 60)

	released := make(chan struct{})
	runner := &fakeRunner{gate: released}
	s := newTestScheduler(&fakeLister{rows: []sqlc.ListEnabledWorkspacesWithLastCheckRow{due}}, runner, now, 10)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(runDone)
	}()

	// Wait for the scheduled check to actually start (it's holding the
	// gate), then cancel - RunCheck being "in flight" when shutdown
	// happens is exactly the scenario this test cares about.
	deadline := time.After(2 * time.Second)
	for runner.concurrent.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the scheduled check to start")
		case <-time.After(time.Millisecond):
		}
	}

	cancel()
	close(released) // let the in-flight fake check observe ctx.Done() or just finish

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation and the in-flight check finishing")
	}

	if len(runner.calls()) != 1 {
		t.Errorf("calls = %v, want exactly 1", runner.calls())
	}
}

func ptr(t time.Time) *time.Time { return &t }

func contains(haystack []string, want string) bool {
	for _, v := range haystack {
		if v == want {
			return true
		}
	}
	return false
}
