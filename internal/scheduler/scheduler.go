// Package scheduler periodically runs drift checks for every enabled
// workspace that's due, bounding how many run at once.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/checks"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

// workspaceLister is the subset of *sqlc.Queries the scheduler needs, so
// tests can supply canned rows instead of a real database.
type workspaceLister interface {
	ListEnabledWorkspacesWithLastCheck(ctx context.Context) ([]sqlc.ListEnabledWorkspacesWithLastCheckRow, error)
}

// checkRunner is the subset of *checks.Service the scheduler needs, so
// tests can supply a fake instead of a real terraform binary.
type checkRunner interface {
	Run(ctx context.Context, ws sqlc.Workspace, triggeredBy string) (checks.Result, error)
}

// Scheduler periodically checks every enabled, due workspace. It is not
// itself responsible for deciding *what* a check does or how it's
// persisted - that's internal/checks.Service, shared with the
// synchronous "check now" HTTP endpoint - only for deciding *when* and
// bounding *how many at once*.
type Scheduler struct {
	queries       workspaceLister
	runner        checkRunner
	logger        *slog.Logger
	now           func() time.Time
	pollInterval  time.Duration
	maxConcurrent int
}

// New builds a Scheduler. pollInterval is how often it checks for due
// workspaces (not how often any single workspace is checked - that's
// the workspace's own check_interval_minutes); maxConcurrent bounds how
// many checks run at the same time across all workspaces.
func New(queries *sqlc.Queries, runner *checks.Service, logger *slog.Logger, pollInterval time.Duration, maxConcurrent int) *Scheduler {
	return &Scheduler{
		queries:       queries,
		runner:        runner,
		logger:        logger,
		now:           time.Now,
		pollInterval:  pollInterval,
		maxConcurrent: maxConcurrent,
	}
}

// Run blocks, checking for due workspaces every pollInterval, until ctx
// is cancelled. On cancellation, it waits for in-flight checks to
// unwind (they themselves observe ctx and are killed promptly - see
// internal/terraform.Executor) before returning.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	state := &runState{
		sem:     make(chan struct{}, s.maxConcurrent),
		running: make(map[string]bool),
	}

	for {
		select {
		case <-ctx.Done():
			state.wg.Wait()
			return
		case <-ticker.C:
			s.tick(ctx, state)
		}
	}
}

// runState is the mutable state one Scheduler's ticks share, factored
// out so tests can drive tick directly and deterministically (no real
// ticker involved) and still wait on exactly the goroutines that one
// tick spawned.
type runState struct {
	mu      sync.Mutex
	running map[string]bool
	sem     chan struct{}
	wg      sync.WaitGroup
}

func (s *Scheduler) tick(ctx context.Context, state *runState) {
	rows, err := s.queries.ListEnabledWorkspacesWithLastCheck(ctx)
	if err != nil {
		s.logger.Error("scheduler: list workspaces", "error", err)
		return
	}

	now := s.now()
	for _, row := range rows {
		if !isDue(row, now) {
			continue
		}

		state.mu.Lock()
		if state.running[row.ID] {
			state.mu.Unlock()
			continue
		}
		state.running[row.ID] = true
		state.mu.Unlock()

		state.wg.Add(1)
		go s.runOne(ctx, state, row)
	}
}

func (s *Scheduler) runOne(ctx context.Context, state *runState, row sqlc.ListEnabledWorkspacesWithLastCheckRow) {
	defer state.wg.Done()
	defer func() {
		state.mu.Lock()
		delete(state.running, row.ID)
		state.mu.Unlock()
	}()

	select {
	case state.sem <- struct{}{}:
		defer func() { <-state.sem }()
	case <-ctx.Done():
		return
	}

	ws := toWorkspace(row)
	if _, err := s.runner.Run(ctx, ws, checks.TriggeredBySchedule); err != nil {
		if errors.Is(err, checks.ErrCheckInProgress) {
			// A manual or API-triggered check for this workspace got there
			// first. Its result counts as this interval's check, so there's
			// nothing to do - and nothing to report as a failure.
			s.logger.Debug("scheduler: skipped, a check is already running", "workspace_id", ws.ID)
			return
		}
		s.logger.Error("scheduler: check failed to persist", "workspace_id", ws.ID, "error", err)
	}
}

// isDue reports whether a workspace should be checked now: never
// checked, or its last check started at least check_interval_minutes
// ago.
func isDue(row sqlc.ListEnabledWorkspacesWithLastCheckRow, now time.Time) bool {
	if !row.LastCheckStartedAt.Valid {
		return true
	}
	interval := time.Duration(row.CheckIntervalMinutes) * time.Minute
	return now.Sub(row.LastCheckStartedAt.Time) >= interval
}

func toWorkspace(row sqlc.ListEnabledWorkspacesWithLastCheckRow) sqlc.Workspace {
	return sqlc.Workspace{
		ID:                   row.ID,
		Name:                 row.Name,
		Description:          row.Description,
		SourcePath:           row.SourcePath,
		WorkingSubdirectory:  row.WorkingSubdirectory,
		BinaryKind:           row.BinaryKind,
		BinaryVersion:        row.BinaryVersion,
		CredentialEnvFile:    row.CredentialEnvFile,
		CheckIntervalMinutes: row.CheckIntervalMinutes,
		CheckTimeoutSeconds:  row.CheckTimeoutSeconds,
		IsEnabled:            row.IsEnabled,
		LastCheckID:          row.LastCheckID,
		LastCheckStatus:      row.LastCheckStatus,
		CreatedBy:            row.CreatedBy,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}
