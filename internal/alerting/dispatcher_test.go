package alerting

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/store/storetest"
)

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

func TestDetermineEvent(t *testing.T) {
	cases := []struct {
		previous, current string
		wantEvent         string
		wantAlert         bool
	}{
		{"", "clean", "", false},
		{"", "drifted", EventDriftDetected, true},
		{"", "failed", EventCheckFailed, true},
		{"clean", "drifted", EventDriftDetected, true},
		{"drifted", "drifted", "", false},
		{"drifted", "clean", EventResolved, true},
		{"drifted", "failed", EventCheckFailed, true},
		{"failed", "failed", "", false},
		{"failed", "clean", "", false},
		{"failed", "drifted", EventDriftDetected, true},
	}
	for _, tc := range cases {
		event, alert := determineEvent(tc.previous, tc.current)
		if event != tc.wantEvent || alert != tc.wantAlert {
			t.Errorf("determineEvent(%q, %q) = (%q, %v), want (%q, %v)",
				tc.previous, tc.current, event, alert, tc.wantEvent, tc.wantAlert)
		}
	}
}

func TestSignBodyIsDeterministicAndKeyed(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	a := signBody("secret-one", body)
	b := signBody("secret-one", body)
	c := signBody("secret-two", body)

	if a != b {
		t.Error("signBody is not deterministic for the same secret and body")
	}
	if a == c {
		t.Error("signBody produced the same signature for two different secrets")
	}
}

func setupWorkspaceAndCheck(t *testing.T, queries *sqlc.Queries) (sqlc.Workspace, sqlc.DriftCheck) {
	t.Helper()
	ctx := context.Background()

	name := "ws-" + uuid.NewString()
	ws, err := queries.CreateWorkspace(ctx, sqlc.CreateWorkspaceParams{
		ID: uuid.NewString(), Name: name, SourcePath: "/data/" + name, BinaryKind: "terraform",
		CheckIntervalMinutes: 60, CheckTimeoutSeconds: 600, IsEnabled: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	check, err := queries.CreateDriftCheck(ctx, sqlc.CreateDriftCheckParams{
		ID: uuid.NewString(), WorkspaceID: ws.ID, Status: "drifted",
		StartedAt: time.Now(), TriggeredBy: "manual",
	})
	if err != nil {
		t.Fatalf("CreateDriftCheck: %v", err)
	}
	return ws, check
}

func createDestination(t *testing.T, queries *sqlc.Queries, url, kind string) sqlc.AlertDestination {
	t.Helper()
	dest, err := queries.CreateAlertDestination(context.Background(), sqlc.CreateAlertDestinationParams{
		ID: uuid.NewString(), Name: "test-dest", Kind: kind, Url: url, IsEnabled: true,
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateAlertDestination: %v", err)
	}
	return dest
}

func TestNotifySendsAndLogsSuccess(t *testing.T) {
	var receivedSig string
	var bodyBytes []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Groundtruth-Signature")
		bodyBytes, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries)

	dest, err := queries.CreateAlertDestination(context.Background(), sqlc.CreateAlertDestinationParams{
		ID: uuid.NewString(), Name: "test", Kind: "generic_webhook", Url: server.URL,
		SharedSecret: sqlNullString("super-secret"), IsEnabled: true, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateAlertDestination: %v", err)
	}

	d := NewDispatcher(queries, slog.New(slog.DiscardHandler), "")
	d.Notify(context.Background(), ws, check, "" /* never checked before -> drift_detected */)

	if receivedSig == "" {
		t.Error("expected an X-Groundtruth-Signature header, got none")
	}
	if !strings.Contains(string(bodyBytes), `"drift_detected"`) {
		t.Errorf("payload body = %s, want it to mention drift_detected", bodyBytes)
	}

	logs, err := queries.ListAlertLogForWorkspace(context.Background(), sqlc.ListAlertLogForWorkspaceParams{
		WorkspaceID: ws.ID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAlertLogForWorkspace: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if !logs[0].Success {
		t.Error("expected the logged attempt to be marked successful")
	}
	if logs[0].DestinationID != dest.ID {
		t.Errorf("destination_id = %q, want %q", logs[0].DestinationID, dest.ID)
	}
	if logs[0].EventType != EventDriftDetected {
		t.Errorf("event_type = %q, want %q", logs[0].EventType, EventDriftDetected)
	}
}

func TestNotifySkipsWhenStatusUnchanged(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries) // check.Status == "drifted"
	createDestination(t, queries, server.URL, "generic_webhook")

	d := NewDispatcher(queries, slog.New(slog.DiscardHandler), "")
	d.Notify(context.Background(), ws, check, "drifted") // same as current -> no alert

	if calls.Load() != 0 {
		t.Errorf("calls = %d, want 0 (drifted -> drifted should not alert)", calls.Load())
	}
}

func TestNotifyRetriesAndLogsFailureAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries)
	createDestination(t, queries, server.URL, "generic_webhook")

	d := NewDispatcher(queries, slog.New(slog.DiscardHandler), "")
	d.retryBackoff = time.Millisecond // keep the test fast

	d.Notify(context.Background(), ws, check, "")

	if calls.Load() != maxAttempts {
		t.Errorf("calls = %d, want %d", calls.Load(), maxAttempts)
	}

	logs, err := queries.ListAlertLogForWorkspace(context.Background(), sqlc.ListAlertLogForWorkspaceParams{
		WorkspaceID: ws.ID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAlertLogForWorkspace: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want exactly 1 summary row for the whole attempt sequence", len(logs))
	}
	if logs[0].Success {
		t.Error("expected the logged outcome to be marked unsuccessful")
	}
	if logs[0].RetryCount != maxAttempts-1 {
		t.Errorf("retry_count = %d, want %d", logs[0].RetryCount, maxAttempts-1)
	}
}

func TestNotifyAppliesOnlyToMatchingWorkspace(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries)
	otherWs, _ := setupWorkspaceAndCheck(t, queries)

	_, err := queries.CreateAlertDestination(context.Background(), sqlc.CreateAlertDestinationParams{
		ID: uuid.NewString(), Name: "scoped-to-other", Kind: "generic_webhook", Url: server.URL,
		WorkspaceID: sqlNullString(otherWs.ID), IsEnabled: true, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("CreateAlertDestination: %v", err)
	}

	d := NewDispatcher(queries, slog.New(slog.DiscardHandler), "")
	d.Notify(context.Background(), ws, check, "")

	if calls.Load() != 0 {
		t.Errorf("calls = %d, want 0 (destination is scoped to a different workspace)", calls.Load())
	}
}
