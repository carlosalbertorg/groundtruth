package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

// minPasswordLength follows NIST SP 800-63B: a minimum length requirement
// with no composition rules (no forced symbols/digits), since groundtruth
// is reached by one operator setting their own password, not the general
// public.
const minPasswordLength = 12

type setupHandlers struct {
	queries *sqlc.Queries
	gate    *auth.SetupGate
}

func newSetupHandlers(queries *sqlc.Queries, gate *auth.SetupGate) *setupHandlers {
	return &setupHandlers{queries: queries, gate: gate}
}

func (h *setupHandlers) status(w http.ResponseWriter, r *http.Request) {
	done, err := h.gate.Complete(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_complete": done})
}

type createAdminRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *setupHandlers) createAdmin(w http.ResponseWriter, r *http.Request) {
	var req createAdminRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	addr, err := mail.ParseAddress(strings.TrimSpace(req.Email))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_email")
		return
	}
	email := strings.ToLower(addr.Address)

	if len(req.Password) < minPasswordLength {
		writeJSONError(w, http.StatusBadRequest, "password_too_short")
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	user, err := h.gate.CreateFirstAdmin(r.Context(), func(ctx context.Context) (sqlc.User, error) {
		return h.queries.CreateUser(ctx, sqlc.CreateUserParams{
			ID:           uuid.NewString(),
			Email:        email,
			PasswordHash: passwordHash,
			CreatedAt:    time.Now(),
		})
	})
	if err != nil {
		if errors.Is(err, auth.ErrSetupAlreadyComplete) {
			writeJSONError(w, http.StatusConflict, "setup_already_complete")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":    user.ID,
		"email": user.Email,
	})
}
