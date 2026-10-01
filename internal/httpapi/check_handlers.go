package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/checks"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

const (
	defaultCheckHistoryLimit = 50

	// maxCheckHistoryLimit caps ?limit=: an unbounded value would let one
	// request read every check a workspace has ever run into memory.
	maxCheckHistoryLimit = 500
)

// checkRunner is the part of *checks.Service the handlers use, so tests can
// supply a fake.
type checkRunner interface {
	Run(ctx context.Context, ws sqlc.Workspace, triggeredBy string) (checks.Result, error)
}

type checkHandlers struct {
	queries *sqlc.Queries
	service checkRunner
}

func newCheckHandlers(queries *sqlc.Queries, service checkRunner) *checkHandlers {
	return &checkHandlers{queries: queries, service: service}
}

// driftCheckResponse is a drift_checks row, shaped for the API. Resources
// is only populated by the single-check detail endpoint - the per-
// workspace history list omits it, since a history page doesn't need
// every past check's full resource diff loaded at once.
type driftCheckResponse struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	DurationMs  *int64     `json:"duration_ms"`
	Summary     struct {
		Added     int64 `json:"added"`
		Changed   int64 `json:"changed"`
		Destroyed int64 `json:"destroyed"`
		Unchanged int64 `json:"unchanged"`
	} `json:"summary"`
	ErrorMessage *string                 `json:"error_message"`
	TriggeredBy  string                  `json:"triggered_by"`
	Resources    []driftResourceResponse `json:"resources,omitempty"`
}

type driftResourceResponse struct {
	Address       string          `json:"address"`
	Type          string          `json:"type"`
	ModuleAddress *string         `json:"module_address,omitempty"`
	Action        string          `json:"action"`
	Before        json.RawMessage `json:"before,omitempty"`
	After         json.RawMessage `json:"after,omitempty"`
	HasSensitive  bool            `json:"has_sensitive"`
	HasUnknown    bool            `json:"has_unknown"`
}

func toDriftCheckResponse(c sqlc.DriftCheck) driftCheckResponse {
	resp := driftCheckResponse{
		ID:          c.ID,
		WorkspaceID: c.WorkspaceID,
		Status:      c.Status,
		StartedAt:   c.StartedAt,
		TriggeredBy: c.TriggeredBy,
	}
	resp.Summary.Added = c.ResourcesAdded
	resp.Summary.Changed = c.ResourcesChanged
	resp.Summary.Destroyed = c.ResourcesDestroyed
	resp.Summary.Unchanged = c.ResourcesUnchanged

	if c.FinishedAt.Valid {
		resp.FinishedAt = &c.FinishedAt.Time
	}
	if c.DurationMs.Valid {
		resp.DurationMs = &c.DurationMs.Int64
	}
	if c.ErrorMessage.Valid {
		resp.ErrorMessage = &c.ErrorMessage.String
	}
	return resp
}

func toDriftResourceResponse(r sqlc.DriftResource) driftResourceResponse {
	resp := driftResourceResponse{
		Address:      r.ResourceAddress,
		Type:         r.ResourceType,
		Action:       r.Action,
		HasSensitive: r.HasSensitive,
		HasUnknown:   r.HasUnknown,
	}
	if r.ModuleAddress.Valid {
		resp.ModuleAddress = &r.ModuleAddress.String
	}
	if r.BeforeJson.Valid {
		resp.Before = json.RawMessage(r.BeforeJson.String)
	}
	if r.AfterJson.Valid {
		resp.After = json.RawMessage(r.AfterJson.String)
	}
	return resp
}

// runNow executes a synchronous drift check against one workspace,
// persists it, and returns the persisted result (now including an ID
// and history, unlike phase 3's version of this endpoint).
func (h *checkHandlers) runNow(w http.ResponseWriter, r *http.Request) {
	ws, err := h.queries.GetWorkspace(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// A check started with an API token (CI) is recorded as such, so the
	// history can tell "someone clicked Check now" from "the pipeline did".
	triggeredBy := checks.TriggeredByManual
	if _, viaToken := auth.APITokenFromContext(r.Context()); viaToken {
		triggeredBy = checks.TriggeredByAPI
	}

	result, err := h.service.Run(r.Context(), ws, triggeredBy)
	if err != nil {
		if errors.Is(err, checks.ErrCheckInProgress) {
			writeJSONError(w, http.StatusConflict, "check_in_progress")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	resp := toDriftCheckResponse(result.Check)
	resp.Resources = make([]driftResourceResponse, len(result.Resources))
	for i, res := range result.Resources {
		resp.Resources[i] = driftResourceResponse{
			Address:       res.Address,
			Type:          res.Type,
			ModuleAddress: stringPtrOrNil(res.ModuleAddress),
			Action:        string(res.Action),
			Before:        marshalRaw(res.Before),
			After:         marshalRaw(res.After),
			HasSensitive:  res.HasSensitive,
			HasUnknown:    res.HasUnknown,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// marshalRaw returns v re-encoded as json.RawMessage, or nil if v is
// nil. v is already redacted (see internal/drift), so this never embeds
// an unredacted value - it only re-serializes what's already safe.
func marshalRaw(v map[string]any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(b)
}

func (h *checkHandlers) history(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")

	if _, err := h.queries.GetWorkspace(r.Context(), workspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	rows, err := h.queries.ListDriftChecksForWorkspace(r.Context(), sqlc.ListDriftChecksForWorkspaceParams{
		WorkspaceID: workspaceID,
		Limit:       parseHistoryLimit(r.URL.Query().Get("limit")),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	resp := make([]driftCheckResponse, len(rows))
	for i, c := range rows {
		resp[i] = toDriftCheckResponse(c)
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseHistoryLimit turns the ?limit= query value into a row count: the
// default when it's absent or unusable, and never more than
// maxCheckHistoryLimit.
func parseHistoryLimit(raw string) int64 {
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		return defaultCheckHistoryLimit
	}
	return min(parsed, maxCheckHistoryLimit)
}

func (h *checkHandlers) get(w http.ResponseWriter, r *http.Request) {
	check, err := h.queries.GetDriftCheck(r.Context(), chi.URLParam(r, "checkID"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "check_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	resources, err := h.queries.ListDriftResourcesForCheck(r.Context(), check.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	resp := toDriftCheckResponse(check)
	resp.Resources = make([]driftResourceResponse, len(resources))
	for i, res := range resources {
		resp.Resources[i] = toDriftResourceResponse(res)
	}
	writeJSON(w, http.StatusOK, resp)
}
