// Package alerting notifies configured destinations (a generic webhook
// or Slack) when a workspace's drift status changes, and logs every
// delivery attempt.
package alerting

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

// Event types stored in alert_log.event_type.
const (
	EventDriftDetected = "drift_detected"
	EventResolved      = "resolved"
	EventCheckFailed   = "check_failed"
)

const (
	maxAttempts        = 3
	attemptTimeout     = 10 * time.Second
	responseSnippetCap = 1024
)

// httpDoer is the subset of *http.Client Dispatcher needs, so tests can
// supply a fake instead of making real network calls.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Dispatcher sends drift-status-change notifications and records every
// attempt. Delivery is synchronous (called inline from
// internal/checks.Service, right after a check is persisted): simpler to
// reason about and test than fire-and-forget, and checks already run
// minutes apart, so a few extra seconds for webhook delivery is an
// acceptable trade-off for v1.
type Dispatcher struct {
	queries *sqlc.Queries
	client  httpDoer
	logger  *slog.Logger
	baseURL string

	// retryBackoff scales the delay between attempts (attempt *
	// retryBackoff); overridden by tests so a 3-attempt failure doesn't
	// take 6 real seconds.
	retryBackoff time.Duration
}

// NewDispatcher builds a Dispatcher. baseURL (optional) is groundtruth's
// externally-visible URL, used only to include a dashboard link in
// outgoing payloads; leave it empty if unset.
func NewDispatcher(queries *sqlc.Queries, logger *slog.Logger, baseURL string) *Dispatcher {
	return &Dispatcher{
		queries:      queries,
		client:       &http.Client{Timeout: attemptTimeout},
		logger:       logger,
		baseURL:      baseURL,
		retryBackoff: 2 * time.Second,
	}
}

// Notify fires the appropriate event (if any) for a workspace
// transitioning from previousStatus to check.Status, to every enabled
// destination that applies to ws. A same-to-same transition (still
// drifted, still failed) is deliberately silent - see determineEvent.
func (d *Dispatcher) Notify(ctx context.Context, ws sqlc.Workspace, check sqlc.DriftCheck, previousStatus string) {
	event, shouldAlert := determineEvent(previousStatus, check.Status)
	if !shouldAlert {
		return
	}

	destinations, err := d.queries.ListAlertDestinationsForWorkspace(ctx, sql.NullString{String: ws.ID, Valid: true})
	if err != nil {
		d.logger.Error("alerting: list destinations", "workspace_id", ws.ID, "error", err)
		return
	}

	for _, dest := range destinations {
		d.send(ctx, dest, ws, check, event)
	}
}

func (d *Dispatcher) send(ctx context.Context, dest sqlc.AlertDestination, ws sqlc.Workspace, check sqlc.DriftCheck, event string) {
	body, contentType, err := buildPayload(dest.Kind, ws, check, event, d.baseURL)
	if err != nil {
		d.logger.Error("alerting: build payload", "destination_id", dest.ID, "error", err)
		return
	}

	var (
		lastStatus  int
		lastSnippet string
		attempt     int
	)
	for attempt = 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * d.retryBackoff)
		}

		status, snippet, err := d.attempt(ctx, dest, body, contentType)
		lastStatus, lastSnippet = status, snippet
		if err == nil {
			d.logAttempt(ctx, dest, ws, check, event, true, lastStatus, lastSnippet, attempt)
			return
		}
		if lastSnippet == "" {
			lastSnippet = err.Error()
		}
	}
	d.logAttempt(ctx, dest, ws, check, event, false, lastStatus, lastSnippet, attempt-1)
}

func (d *Dispatcher) attempt(ctx context.Context, dest sqlc.AlertDestination, body []byte, contentType string) (status int, snippet string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest.Url, bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", contentType)
	if dest.SharedSecret.Valid && dest.SharedSecret.String != "" {
		req.Header.Set("X-Groundtruth-Signature", "sha256="+signBody(dest.SharedSecret.String, body))
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, responseSnippetCap))
	snippet = string(raw)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, snippet, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return resp.StatusCode, snippet, nil
}

func (d *Dispatcher) logAttempt(ctx context.Context, dest sqlc.AlertDestination, ws sqlc.Workspace, check sqlc.DriftCheck, event string, success bool, status int, snippet string, retryCount int) {
	err := d.queries.CreateAlertLogEntry(ctx, sqlc.CreateAlertLogEntryParams{
		ID:              uuid.NewString(),
		DestinationID:   dest.ID,
		WorkspaceID:     ws.ID,
		DriftCheckID:    sql.NullString{String: check.ID, Valid: true},
		EventType:       event,
		Success:         success,
		HttpStatus:      sql.NullInt64{Int64: int64(status), Valid: status != 0},
		ResponseSnippet: sql.NullString{String: snippet, Valid: snippet != ""},
		AttemptedAt:     time.Now(),
		RetryCount:      int64(retryCount),
	})
	if err != nil {
		d.logger.Error("alerting: persist alert log entry", "destination_id", dest.ID, "error", err)
	}
	if !success {
		d.logger.Warn("alerting: delivery failed", "destination_id", dest.ID, "workspace_id", ws.ID, "event", event, "status", status)
	}
}

// signBody returns the hex-encoded HMAC-SHA256 of body using secret,
// mirroring GitHub/Stripe's webhook-signing convention so receivers can
// verify authenticity.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// determineEvent decides whether current (the just-finished check's
// status) warrants an alert given previous (the workspace's status
// before this check), and which event type. A transition to the same
// status never alerts - including drifted-to-drifted or failed-to-
// failed, so an unresolved problem doesn't re-notify on every check.
// Revisit with a time-based re-notify if that turns out to matter in
// practice; v1 intentionally keeps this simple.
func determineEvent(previous, current string) (event string, shouldAlert bool) {
	if previous == current {
		return "", false
	}
	switch current {
	case "drifted":
		return EventDriftDetected, true
	case "clean":
		if previous == "drifted" {
			return EventResolved, true
		}
		return "", false
	case "failed":
		return EventCheckFailed, true
	default:
		return "", false
	}
}
