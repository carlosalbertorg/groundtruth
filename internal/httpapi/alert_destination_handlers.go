package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

type alertDestinationHandlers struct {
	queries *sqlc.Queries
}

func newAlertDestinationHandlers(queries *sqlc.Queries) *alertDestinationHandlers {
	return &alertDestinationHandlers{queries: queries}
}

// alertDestinationResponse never includes the shared secret - it's
// write-only, same rationale as a password field. has_secret tells the
// UI whether one is set, without revealing it.
type alertDestinationResponse struct {
	ID          string    `json:"id"`
	WorkspaceID *string   `json:"workspace_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	URL         string    `json:"url"`
	HasSecret   bool      `json:"has_secret"`
	IsEnabled   bool      `json:"is_enabled"`
	CreatedAt   time.Time `json:"created_at"`
}

func toAlertDestinationResponse(d sqlc.AlertDestination) alertDestinationResponse {
	resp := alertDestinationResponse{
		ID:        d.ID,
		Name:      d.Name,
		Kind:      d.Kind,
		URL:       d.Url,
		HasSecret: d.SharedSecret.Valid && d.SharedSecret.String != "",
		IsEnabled: d.IsEnabled,
		CreatedAt: d.CreatedAt,
	}
	if d.WorkspaceID.Valid {
		resp.WorkspaceID = &d.WorkspaceID.String
	}
	return resp
}

type alertDestinationRequest struct {
	WorkspaceID *string `json:"workspace_id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	URL         string  `json:"url"`
	// SharedSecret: omitted/null on update keeps whatever secret is
	// already set; an explicit "" clears it; anything else replaces it.
	// On create, omitted/null/"" all just mean "no secret."
	SharedSecret *string `json:"shared_secret"`
	IsEnabled    *bool   `json:"is_enabled"`
}

func (h *alertDestinationHandlers) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.queries.ListAlertDestinations(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	resp := make([]alertDestinationResponse, len(rows))
	for i, d := range rows {
		resp[i] = toAlertDestinationResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *alertDestinationHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req alertDestinationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	name := strings.TrimSpace(req.Name)
	url := strings.TrimSpace(req.URL)
	kind := strings.TrimSpace(req.Kind)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name_required")
		return
	}
	if url == "" {
		writeJSONError(w, http.StatusBadRequest, "url_required")
		return
	}
	if kind != "generic_webhook" && kind != "slack" {
		writeJSONError(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	if !isValidWebhookURL(url) {
		writeJSONError(w, http.StatusBadRequest, "invalid_url")
		return
	}

	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	dest, err := h.queries.CreateAlertDestination(r.Context(), sqlc.CreateAlertDestinationParams{
		ID:           uuid.NewString(),
		WorkspaceID:  blankOrNilToNullString(req.WorkspaceID),
		Name:         name,
		Kind:         kind,
		Url:          url,
		SharedSecret: blankOrNilToNullString(req.SharedSecret),
		IsEnabled:    isEnabled,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusCreated, toAlertDestinationResponse(dest))
}

func (h *alertDestinationHandlers) update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.queries.GetAlertDestination(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "alert_destination_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	var req alertDestinationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	name := strings.TrimSpace(req.Name)
	url := strings.TrimSpace(req.URL)
	kind := strings.TrimSpace(req.Kind)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name_required")
		return
	}
	if url == "" {
		writeJSONError(w, http.StatusBadRequest, "url_required")
		return
	}
	if kind != "generic_webhook" && kind != "slack" {
		writeJSONError(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	if !isValidWebhookURL(url) {
		writeJSONError(w, http.StatusBadRequest, "invalid_url")
		return
	}

	isEnabled := existing.IsEnabled
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	secret := existing.SharedSecret
	if req.SharedSecret != nil {
		secret = blankOrNilToNullString(req.SharedSecret)
	}

	dest, err := h.queries.UpdateAlertDestination(r.Context(), sqlc.UpdateAlertDestinationParams{
		ID:           id,
		WorkspaceID:  blankOrNilToNullString(req.WorkspaceID),
		Name:         name,
		Kind:         kind,
		Url:          url,
		SharedSecret: secret,
		IsEnabled:    isEnabled,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, toAlertDestinationResponse(dest))
}

func (h *alertDestinationHandlers) delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.queries.GetAlertDestination(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "alert_destination_not_found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if err := h.queries.DeleteAlertDestination(r.Context(), id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// isValidWebhookURL reports whether raw is an absolute http(s) URL with a
// host - the only kind of destination the dispatcher can POST to. Anything
// else would be accepted at save time and then fail silently, at the moment
// an alert actually needs to go out.
func isValidWebhookURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// blankOrNilToNullString treats both a nil pointer and an explicit ""
// as "no value" - unlike workspace_handlers.go's ptrToNullString, which
// only nil-checks. Alert destinations use that distinction
// deliberately: an explicitly empty shared_secret in an update request
// means "clear it," not "store an empty string."
func blankOrNilToNullString(s *string) sql.NullString {
	if s == nil || *s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}
