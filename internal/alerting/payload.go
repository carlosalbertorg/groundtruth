package alerting

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

// buildPayload returns the JSON body and content type to send for
// dest.Kind. Both payload shapes carry the same underlying facts; they
// differ in how a human is meant to read them (Slack renders Block Kit,
// a generic webhook receiver parses plain JSON fields).
func buildPayload(kind string, ws sqlc.Workspace, check sqlc.DriftCheck, event, baseURL string) ([]byte, string, error) {
	switch kind {
	case "slack":
		body, err := json.Marshal(slackPayload(ws, check, event, baseURL))
		return body, "application/json", err
	default:
		body, err := json.Marshal(webhookPayload(ws, check, event, baseURL))
		return body, "application/json", err
	}
}

type genericWebhookPayload struct {
	Event        string       `json:"event"`
	Workspace    workspaceRef `json:"workspace"`
	Check        checkRef     `json:"check"`
	DashboardURL string       `json:"dashboard_url,omitempty"`
	Timestamp    time.Time    `json:"timestamp"`
}

type workspaceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type checkRef struct {
	ID        string       `json:"id"`
	Status    string       `json:"status"`
	StartedAt time.Time    `json:"started_at"`
	Summary   checkSummary `json:"summary"`
}

type checkSummary struct {
	Added     int64 `json:"added"`
	Changed   int64 `json:"changed"`
	Destroyed int64 `json:"destroyed"`
}

func webhookPayload(ws sqlc.Workspace, check sqlc.DriftCheck, event, baseURL string) genericWebhookPayload {
	return genericWebhookPayload{
		Event:        event,
		Workspace:    workspaceRef{ID: ws.ID, Name: ws.Name},
		Check:        toCheckRef(check),
		DashboardURL: dashboardURL(baseURL, ws.ID),
		Timestamp:    time.Now(),
	}
}

func toCheckRef(check sqlc.DriftCheck) checkRef {
	return checkRef{
		ID:        check.ID,
		Status:    check.Status,
		StartedAt: check.StartedAt,
		Summary: checkSummary{
			Added:     check.ResourcesAdded,
			Changed:   check.ResourcesChanged,
			Destroyed: check.ResourcesDestroyed,
		},
	}
}

func dashboardURL(baseURL, workspaceID string) string {
	if baseURL == "" {
		return ""
	}
	// GROUNDTRUTH_BASE_URL is operator-typed, and a trailing slash is an
	// easy thing to include; without trimming it the link has a "//".
	return fmt.Sprintf("%s/workspaces/%s", strings.TrimRight(baseURL, "/"), workspaceID)
}

// --- Slack Block Kit ---

type slackMessage struct {
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string         `json:"type"`
	Text     *slackText     `json:"text,omitempty"`
	Fields   []slackText    `json:"fields,omitempty"`
	Elements []slackElement `json:"elements,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackElement struct {
	Type string `json:"type"`
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

func slackPayload(ws sqlc.Workspace, check sqlc.DriftCheck, event, baseURL string) slackMessage {
	header, emoji := slackHeaderFor(event)

	blocks := []slackBlock{
		{
			Type: "header",
			Text: &slackText{Type: "plain_text", Text: fmt.Sprintf("%s %s", emoji, header)},
		},
		{
			Type: "section",
			Fields: []slackText{
				{Type: "mrkdwn", Text: "*Workspace:*\n" + ws.Name},
				{Type: "mrkdwn", Text: "*Status:*\n" + check.Status},
				{Type: "mrkdwn", Text: fmt.Sprintf("*Added / Changed / Destroyed:*\n%d / %d / %d",
					check.ResourcesAdded, check.ResourcesChanged, check.ResourcesDestroyed)},
			},
		},
	}

	if url := dashboardURL(baseURL, ws.ID); url != "" {
		blocks = append(blocks, slackBlock{
			Type: "actions",
			Elements: []slackElement{
				{Type: "button", Text: "View in groundtruth", URL: url},
			},
		})
	}

	return slackMessage{Blocks: blocks}
}

func slackHeaderFor(event string) (headline, emoji string) {
	switch event {
	case EventDriftDetected:
		return "Drift detected", "⚠️"
	case EventResolved:
		return "Drift resolved", "✅"
	case EventCheckFailed:
		return "Check failed", "🛑"
	default:
		return "groundtruth notification", "ℹ️"
	}
}
