package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/carlosalbertorg/groundtruth/internal/drift"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/terraform"
)

type checkHandlers struct {
	queries  *sqlc.Queries
	executor *terraform.Executor
}

func newCheckHandlers(queries *sqlc.Queries, executor *terraform.Executor) *checkHandlers {
	return &checkHandlers{queries: queries, executor: executor}
}

// runNow executes a synchronous drift check against one workspace and
// returns the redacted result directly in the response. Nothing is
// persisted yet - there's no drift_checks table until workspace history
// is added, and no scheduler runs this on its own yet either.
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

	plan, err := h.executor.RunCheck(r.Context(), terraform.CheckInput{
		SourcePath:          ws.SourcePath,
		WorkingSubdirectory: ws.WorkingSubdirectory.String,
		BinaryKind:          ws.BinaryKind,
		CredentialEnvFile:   ws.CredentialEnvFile.String,
		Timeout:             time.Duration(ws.CheckTimeoutSeconds) * time.Second,
	})
	if err != nil {
		// A self-hosted, single-operator admin tool: showing the real
		// error (init failed, binary missing, plan timed out, ...) is
		// more useful here than hiding it. It's never a credential
		// value - nothing in this package formats one into an error.
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":   "check_failed",
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, drift.FromPlan(plan))
}
