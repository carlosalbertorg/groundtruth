// Package checks runs a drift check against a workspace and persists
// the result, shared by both the synchronous "check now" HTTP endpoint
// and (from phase 5 on) the periodic scheduler - so there's exactly one
// place that decides how a check's outcome becomes database rows.
package checks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	tfjson "github.com/hashicorp/terraform-json"

	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/drift"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/terraform"
)

// Status values stored in drift_checks.status.
const (
	StatusClean   = "clean"
	StatusDrifted = "drifted"
	StatusFailed  = "failed"
)

// TriggeredBy values stored in drift_checks.triggered_by.
const (
	TriggeredByManual   = "manual"
	TriggeredBySchedule = "schedule"
	TriggeredByAPI      = "api"
)

// executor is the subset of *terraform.Executor this package depends
// on, so tests can inject a fake instead of needing a real
// terraform/tofu binary on PATH.
type executor interface {
	RunCheck(ctx context.Context, in terraform.CheckInput) (*tfjson.Plan, error)
}

// Notifier is told about every check's outcome, so it can alert on a
// status change. It's optional - a Service with none configured simply
// skips this step.
type Notifier interface {
	Notify(ctx context.Context, ws sqlc.Workspace, check sqlc.DriftCheck, previousStatus string)
}

// Service runs drift checks and persists their outcome.
type Service struct {
	queries  *sqlc.Queries
	executor executor
	notifier Notifier
}

// NewService builds a Service backed by queries and executor.
func NewService(queries *sqlc.Queries, executor *terraform.Executor) *Service {
	return &Service{queries: queries, executor: executor}
}

// SetNotifier attaches a Notifier, told about every check's outcome
// from this point on. Optional; call it once during startup wiring.
func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

// Result is a persisted check together with its (already-redacted)
// per-resource detail, which isn't stored on DriftCheck itself.
type Result struct {
	Check     sqlc.DriftCheck
	Resources []drift.ResourceDrift
}

// Run executes one drift check against ws, persists it (a drift_checks
// row, a drift_resources row per affected resource, and the workspace's
// last_check_id/status), and returns the persisted result.
//
// A failed *terraform run* (bad source path, init error, timeout, ...)
// is not a Go error from Run's perspective - it's recorded as a
// "failed" check, same as any other outcome. Run only returns an error
// when persisting that outcome itself fails.
func (s *Service) Run(ctx context.Context, ws sqlc.Workspace, triggeredBy string) (Result, error) {
	startedAt := time.Now()
	plan, runErr := s.executor.RunCheck(ctx, terraform.CheckInput{
		SourcePath:          ws.SourcePath,
		WorkingSubdirectory: ws.WorkingSubdirectory.String,
		BinaryKind:          ws.BinaryKind,
		CredentialEnvFile:   ws.CredentialEnvFile.String,
		Timeout:             time.Duration(ws.CheckTimeoutSeconds) * time.Second,
	})
	finishedAt := time.Now()

	var result drift.Result
	status := StatusClean
	var errMessage sql.NullString

	if runErr != nil {
		status = StatusFailed
		errMessage = sql.NullString{String: runErr.Error(), Valid: true}
	} else {
		result = drift.FromPlan(plan)
		if result.Drifted() {
			status = StatusDrifted
		}
	}

	checkID := uuid.NewString()
	check, err := s.queries.CreateDriftCheck(ctx, sqlc.CreateDriftCheckParams{
		ID:                 checkID,
		WorkspaceID:        ws.ID,
		Status:             status,
		StartedAt:          startedAt,
		FinishedAt:         sql.NullTime{Time: finishedAt, Valid: true},
		DurationMs:         sql.NullInt64{Int64: finishedAt.Sub(startedAt).Milliseconds(), Valid: true},
		ResourcesAdded:     int64(result.Summary.Added),
		ResourcesChanged:   int64(result.Summary.Changed),
		ResourcesDestroyed: int64(result.Summary.Destroyed),
		ResourcesUnchanged: int64(result.Summary.Unchanged),
		ErrorMessage:       errMessage,
		TriggeredBy:        triggeredBy,
	})
	if err != nil {
		return Result{}, fmt.Errorf("persist drift check: %w", err)
	}

	for _, res := range result.Resources {
		if err := s.persistResource(ctx, checkID, res); err != nil {
			return Result{}, err
		}
	}

	if err := s.queries.UpdateWorkspaceLastCheck(ctx, sqlc.UpdateWorkspaceLastCheckParams{
		LastCheckID:     sql.NullString{String: checkID, Valid: true},
		LastCheckStatus: sql.NullString{String: status, Valid: true},
		ID:              ws.ID,
	}); err != nil {
		return Result{}, fmt.Errorf("update workspace last check: %w", err)
	}

	if s.notifier != nil {
		s.notifier.Notify(ctx, ws, check, ws.LastCheckStatus.String)
	}

	return Result{Check: check, Resources: result.Resources}, nil
}

func (s *Service) persistResource(ctx context.Context, checkID string, res drift.ResourceDrift) error {
	before, err := marshalOrNil(res.Before)
	if err != nil {
		return fmt.Errorf("marshal before value for %s: %w", res.Address, err)
	}
	after, err := marshalOrNil(res.After)
	if err != nil {
		return fmt.Errorf("marshal after value for %s: %w", res.Address, err)
	}

	err = s.queries.CreateDriftResource(ctx, sqlc.CreateDriftResourceParams{
		ID:              uuid.NewString(),
		DriftCheckID:    checkID,
		ResourceAddress: res.Address,
		ResourceType:    res.Type,
		ModuleAddress:   nullStringIfEmpty(res.ModuleAddress),
		Action:          string(res.Action),
		BeforeJson:      before,
		AfterJson:       after,
		HasSensitive:    res.HasSensitive,
		HasUnknown:      res.HasUnknown,
	})
	if err != nil {
		return fmt.Errorf("persist drift resource %s: %w", res.Address, err)
	}
	return nil
}

func marshalOrNil(v map[string]any) (sql.NullString, error) {
	if v == nil {
		return sql.NullString{}, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(b), Valid: true}, nil
}

func nullStringIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
