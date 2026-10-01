package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/store"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

const (
	defaultCheckIntervalMinutes = 60
	defaultCheckTimeoutSeconds  = 600
)

type workspaceHandlers struct {
	queries *sqlc.Queries
}

func newWorkspaceHandlers(queries *sqlc.Queries) *workspaceHandlers {
	return &workspaceHandlers{queries: queries}
}

// workspaceResponse is the JSON shape returned to clients. It exists
// separately from sqlc.Workspace so nullable columns serialize as null
// rather than database/sql's {"String":"","Valid":false} shape, and so
// adding a DB column doesn't silently change the public API.
type workspaceResponse struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Description          *string   `json:"description"`
	SourcePath           string    `json:"source_path"`
	WorkingSubdirectory  *string   `json:"working_subdirectory"`
	BinaryKind           string    `json:"binary_kind"`
	BinaryVersion        *string   `json:"binary_version"`
	CredentialEnvFile    *string   `json:"credential_env_file"`
	CheckIntervalMinutes int64     `json:"check_interval_minutes"`
	CheckTimeoutSeconds  int64     `json:"check_timeout_seconds"`
	IsEnabled            bool      `json:"is_enabled"`
	LastCheckID          *string   `json:"last_check_id"`
	LastCheckStatus      *string   `json:"last_check_status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func toWorkspaceResponse(w sqlc.Workspace) workspaceResponse {
	return workspaceResponse{
		ID:                   w.ID,
		Name:                 w.Name,
		Description:          nullStringToPtr(w.Description),
		SourcePath:           w.SourcePath,
		WorkingSubdirectory:  nullStringToPtr(w.WorkingSubdirectory),
		BinaryKind:           w.BinaryKind,
		BinaryVersion:        nullStringToPtr(w.BinaryVersion),
		CredentialEnvFile:    nullStringToPtr(w.CredentialEnvFile),
		CheckIntervalMinutes: w.CheckIntervalMinutes,
		CheckTimeoutSeconds:  w.CheckTimeoutSeconds,
		IsEnabled:            w.IsEnabled,
		LastCheckID:          nullStringToPtr(w.LastCheckID),
		LastCheckStatus:      nullStringToPtr(w.LastCheckStatus),
		CreatedAt:            w.CreatedAt,
		UpdatedAt:            w.UpdatedAt,
	}
}

func nullStringToPtr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func ptrToNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// workspaceRequest is the shape accepted from clients for both create and
// update. Update is a full replace (not a JSON-merge-patch), matching
// AddApplicationDialog's "DefaultPriority" pattern in the prior art this
// design followed — every editable field is sent every time.
type workspaceRequest struct {
	Name                 string  `json:"name"`
	Description          *string `json:"description"`
	SourcePath           string  `json:"source_path"`
	WorkingSubdirectory  *string `json:"working_subdirectory"`
	BinaryKind           string  `json:"binary_kind"`
	BinaryVersion        *string `json:"binary_version"`
	CredentialEnvFile    *string `json:"credential_env_file"`
	CheckIntervalMinutes *int64  `json:"check_interval_minutes"`
	CheckTimeoutSeconds  *int64  `json:"check_timeout_seconds"`
	IsEnabled            *bool   `json:"is_enabled"`
}

// normalizedWorkspace is workspaceRequest after trimming, defaulting, and
// validating — what's actually written to the database.
type normalizedWorkspace struct {
	name                 string
	description          *string
	sourcePath           string
	workingSubdirectory  *string
	binaryKind           string
	binaryVersion        *string
	credentialEnvFile    *string
	checkIntervalMinutes int64
	checkTimeoutSeconds  int64
	isEnabled            bool
}

func normalizeWorkspaceRequest(req workspaceRequest) (normalizedWorkspace, string) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return normalizedWorkspace{}, "name_required"
	}

	sourcePath := strings.TrimSpace(req.SourcePath)
	if sourcePath == "" {
		return normalizedWorkspace{}, "source_path_required"
	}

	binaryKind := strings.TrimSpace(req.BinaryKind)
	if binaryKind != "terraform" && binaryKind != "tofu" {
		return normalizedWorkspace{}, "invalid_binary_kind"
	}

	interval := int64(defaultCheckIntervalMinutes)
	if req.CheckIntervalMinutes != nil {
		interval = *req.CheckIntervalMinutes
	}
	if interval <= 0 {
		return normalizedWorkspace{}, "invalid_check_interval_minutes"
	}

	timeout := int64(defaultCheckTimeoutSeconds)
	if req.CheckTimeoutSeconds != nil {
		timeout = *req.CheckTimeoutSeconds
	}
	if timeout <= 0 {
		return normalizedWorkspace{}, "invalid_check_timeout_seconds"
	}

	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	return normalizedWorkspace{
		name:                 name,
		description:          trimmedOrNil(req.Description),
		sourcePath:           sourcePath,
		workingSubdirectory:  trimmedOrNil(req.WorkingSubdirectory),
		binaryKind:           binaryKind,
		binaryVersion:        trimmedOrNil(req.BinaryVersion),
		credentialEnvFile:    trimmedOrNil(req.CredentialEnvFile),
		checkIntervalMinutes: interval,
		checkTimeoutSeconds:  timeout,
		isEnabled:            isEnabled,
	}, ""
}

// trimmedOrNil trims s and returns nil for both a nil input and a
// now-empty string, so "" and " " are treated the same as omitted.
func trimmedOrNil(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (h *workspaceHandlers) list(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.queries.ListWorkspaces(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	resp := make([]workspaceResponse, len(workspaces))
	for i, ws := range workspaces {
		resp[i] = toWorkspaceResponse(ws)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *workspaceHandlers) get(w http.ResponseWriter, r *http.Request) {
	ws, err := h.queries.GetWorkspace(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, toWorkspaceResponse(ws))
}

func (h *workspaceHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req workspaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	normalized, errCode := normalizeWorkspaceRequest(req)
	if errCode != "" {
		writeJSONError(w, http.StatusBadRequest, errCode)
		return
	}

	user, _ := auth.UserFromContext(r.Context())
	now := time.Now()

	ws, err := h.queries.CreateWorkspace(r.Context(), sqlc.CreateWorkspaceParams{
		ID:                   uuid.NewString(),
		Name:                 normalized.name,
		Description:          ptrToNullString(normalized.description),
		SourcePath:           normalized.sourcePath,
		WorkingSubdirectory:  ptrToNullString(normalized.workingSubdirectory),
		BinaryKind:           normalized.binaryKind,
		BinaryVersion:        ptrToNullString(normalized.binaryVersion),
		CredentialEnvFile:    ptrToNullString(normalized.credentialEnvFile),
		CheckIntervalMinutes: normalized.checkIntervalMinutes,
		CheckTimeoutSeconds:  normalized.checkTimeoutSeconds,
		IsEnabled:            normalized.isEnabled,
		CreatedBy:            sql.NullString{String: user.ID, Valid: user.ID != ""},
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		if store.IsUniqueConstraintViolation(err) {
			writeJSONError(w, http.StatusConflict, "name_taken")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusCreated, toWorkspaceResponse(ws))
}

func (h *workspaceHandlers) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req workspaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	normalized, errCode := normalizeWorkspaceRequest(req)
	if errCode != "" {
		writeJSONError(w, http.StatusBadRequest, errCode)
		return
	}

	ws, err := h.queries.UpdateWorkspace(r.Context(), sqlc.UpdateWorkspaceParams{
		ID:                   id,
		Name:                 normalized.name,
		Description:          ptrToNullString(normalized.description),
		SourcePath:           normalized.sourcePath,
		WorkingSubdirectory:  ptrToNullString(normalized.workingSubdirectory),
		BinaryKind:           normalized.binaryKind,
		BinaryVersion:        ptrToNullString(normalized.binaryVersion),
		CredentialEnvFile:    ptrToNullString(normalized.credentialEnvFile),
		CheckIntervalMinutes: normalized.checkIntervalMinutes,
		CheckTimeoutSeconds:  normalized.checkTimeoutSeconds,
		IsEnabled:            normalized.isEnabled,
		UpdatedAt:            time.Now(),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		if store.IsUniqueConstraintViolation(err) {
			writeJSONError(w, http.StatusConflict, "name_taken")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusOK, toWorkspaceResponse(ws))
}

func (h *workspaceHandlers) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if _, err := h.queries.GetWorkspace(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	if err := h.queries.DeleteWorkspace(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
