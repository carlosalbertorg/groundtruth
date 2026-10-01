package alerting

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestNotifyTreatsARedirectAsAFailedDelivery(t *testing.T) {
	var landed atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		landed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	// A webhook URL that redirects (say, http:// to https://). Following it
	// would re-send the POST as a GET, get a 200 from wherever it lands, and
	// log a delivery that never reached the receiver.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusMovedPermanently)
	}))
	defer redirector.Close()

	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries)
	createDestination(t, queries, redirector.URL, "generic_webhook")

	d := NewDispatcher(queries, slog.New(slog.DiscardHandler), "")
	d.retryBackoff = time.Millisecond
	d.Notify(context.Background(), ws, check, "")

	if landed.Load() != 0 {
		t.Errorf("the redirect target was reached %d times, want 0: redirects must not be followed", landed.Load())
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
	if logs[0].Success {
		t.Error("a redirect was logged as a successful delivery")
	}
	if !logs[0].HttpStatus.Valid || logs[0].HttpStatus.Int64 != http.StatusMovedPermanently {
		t.Errorf("http_status = %v, want %d, so the operator can see what went wrong", logs[0].HttpStatus, http.StatusMovedPermanently)
	}
}

func TestNotifyDeliversToDestinationsInParallel(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = true
		mu.Unlock()

		// Hold the response until a request for the *other* destination is
		// also in flight. Delivered one after the other, the first would sit
		// here waiting for a request that can't start until it returns.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			both := len(seen) == 2
			mu.Unlock()
			if both {
				w.WriteHeader(http.StatusOK)
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries)
	createDestination(t, queries, server.URL+"/a", "generic_webhook")
	createDestination(t, queries, server.URL+"/b", "generic_webhook")

	d := NewDispatcher(queries, slog.New(slog.DiscardHandler), "")
	d.retryBackoff = time.Millisecond
	d.Notify(context.Background(), ws, check, "")

	logs, err := queries.ListAlertLogForWorkspace(context.Background(), sqlc.ListAlertLogForWorkspaceParams{
		WorkspaceID: ws.ID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAlertLogForWorkspace: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("len(logs) = %d, want one entry per destination", len(logs))
	}
	for _, l := range logs {
		if !l.Success {
			t.Errorf("a delivery failed (status %v): destinations are not being attempted concurrently", l.HttpStatus)
		}
	}
}

// panickingDoer fails every request by panicking, standing in for a bug
// somewhere in the delivery path.
type panickingDoer struct{}

func (panickingDoer) Do(*http.Request) (*http.Response, error) { panic("boom") }

func TestNotifyContainsAPanicInOneDelivery(t *testing.T) {
	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries)
	createDestination(t, queries, "https://example.com/hook", "generic_webhook")

	var logs strings.Builder
	d := NewDispatcher(queries, slog.New(slog.NewTextHandler(&logs, nil)), "")
	d.client = panickingDoer{}

	// Reaching the next line at all is the assertion: delivery runs in its
	// own goroutine, so an unrecovered panic would end the test binary.
	d.Notify(context.Background(), ws, check, "")

	if !strings.Contains(logs.String(), "panic while delivering") {
		t.Errorf("the panic was swallowed without being logged:\n%s", logs.String())
	}
}

func TestNotifyStopsRetryingOnceTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		cancel() // shutdown begins while the first attempt is failing
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	queries := sqlc.New(storetest.OpenDB(t))
	ws, check := setupWorkspaceAndCheck(t, queries)
	createDestination(t, queries, server.URL, "generic_webhook")

	d := NewDispatcher(queries, slog.New(slog.DiscardHandler), "")
	d.retryBackoff = time.Hour // the test hangs if the wait between attempts ignores ctx

	done := make(chan struct{})
	go func() {
		d.Notify(ctx, ws, check, "")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Notify kept waiting out its retry backoff after the context was cancelled")
	}

	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (no retries once cancelled)", calls.Load())
	}

	// The abandoned delivery must still be on record, even though the
	// context it ran under is gone.
	logs, err := queries.ListAlertLogForWorkspace(context.Background(), sqlc.ListAlertLogForWorkspaceParams{
		WorkspaceID: ws.ID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAlertLogForWorkspace: %v", err)
	}
	if len(logs) != 1 || logs[0].Success {
		t.Fatalf("logs = %+v, want exactly one failed entry", logs)
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
