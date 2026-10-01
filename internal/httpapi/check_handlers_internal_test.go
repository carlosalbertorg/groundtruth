package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/checks"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/store/storetest"
)

type stubRunner struct{ err error }

func (s stubRunner) Run(context.Context, sqlc.Workspace, string) (checks.Result, error) {
	return checks.Result{}, s.err
}

func TestRunNowReportsAnOverlappingCheckAsAConflict(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws, err := queries.CreateWorkspace(context.Background(), sqlc.CreateWorkspaceParams{
		ID: uuid.NewString(), Name: "busy", SourcePath: "/modules/busy", BinaryKind: "terraform",
		CheckIntervalMinutes: 60, CheckTimeoutSeconds: 600, IsEnabled: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	h := newCheckHandlers(queries, stubRunner{err: checks.ErrCheckInProgress})

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("id", ws.ID)
	req := httptest.NewRequestWithContext(
		context.WithValue(t.Context(), chi.RouteCtxKey, routeCtx), http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.runNow(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "check_in_progress" {
		t.Errorf("error = %q, want check_in_progress", body.Error)
	}
}

func TestParseHistoryLimit(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"", defaultCheckHistoryLimit},
		{"not-a-number", defaultCheckHistoryLimit},
		{"0", defaultCheckHistoryLimit},
		{"-5", defaultCheckHistoryLimit},
		{"10", 10},
		{"500", 500},
		{"501", maxCheckHistoryLimit},
		{"1000000000", maxCheckHistoryLimit},
	}
	for _, tc := range cases {
		if got := parseHistoryLimit(tc.raw); got != tc.want {
			t.Errorf("parseHistoryLimit(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}
