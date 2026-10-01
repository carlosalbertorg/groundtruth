package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

type apiTokenHandlers struct {
	queries *sqlc.Queries
	tokens  *auth.APITokenManager
}

func newAPITokenHandlers(queries *sqlc.Queries, tokens *auth.APITokenManager) *apiTokenHandlers {
	return &apiTokenHandlers{queries: queries, tokens: tokens}
}

// apiTokenResponse never includes the token value itself - see create,
// which is the one and only response that does.
type apiTokenResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func toAPITokenResponse(t sqlc.ApiToken) apiTokenResponse {
	resp := apiTokenResponse{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt}
	if t.LastUsedAt.Valid {
		resp.LastUsedAt = &t.LastUsedAt.Time
	}
	return resp
}

func (h *apiTokenHandlers) list(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	rows, err := h.queries.ListAPITokensForUser(r.Context(), user.ID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	resp := make([]apiTokenResponse, len(rows))
	for i, t := range rows {
		resp[i] = toAPITokenResponse(t)
	}
	writeJSON(w, http.StatusOK, resp)
}

type createAPITokenRequest struct {
	Name string `json:"name"`
}

func (h *apiTokenHandlers) create(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createAPITokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name_required")
		return
	}

	id := uuid.NewString()
	token, err := h.tokens.Create(r.Context(), user.ID, id, name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	// The only time this value is ever shown - the client must save it
	// now. Everywhere else, an API token is identified by id/name only.
	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    id,
		"name":  name,
		"token": token,
	})
}

func (h *apiTokenHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.tokens.Revoke(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
