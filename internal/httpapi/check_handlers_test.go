package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

func TestCheckNowReturns404ForMissingWorkspace(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodPost, "/api/workspaces/does-not-exist/check", nil, cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body)
	}
}

func TestCheckNowPersistsAFailedCheckWhenSourcePathDoesNotExist(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, map[string]any{
		"source_path": "/this/path/does/not/exist/anywhere",
	})

	rec := doJSON(t, r, http.MethodPost, "/api/workspaces/"+ws["id"].(string)+"/check", nil, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body)
	}

	var body map[string]any
	decodeBody(t, rec, &body)
	if body["status"] != "failed" {
		t.Errorf("status = %v, want failed", body["status"])
	}
	if body["error_message"] == nil {
		t.Error("error_message = nil, want a description of the failure")
	}
	if body["id"] == nil || body["id"] == "" {
		t.Error("expected a persisted check id")
	}
}

func TestCheckNowRecordsWhoTriggeredIt(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)
	ws := createTestWorkspace(t, r, cookie, map[string]any{
		"source_path": "/this/path/does/not/exist/anywhere",
	})
	checkURL := "/api/workspaces/" + ws["id"].(string) + "/check"

	// From the dashboard: a session cookie.
	rec := doJSON(t, r, http.MethodPost, checkURL, nil, cookie)
	var viaSession map[string]any
	decodeBody(t, rec, &viaSession)
	if viaSession["triggered_by"] != "manual" {
		t.Errorf("triggered_by via the dashboard = %v, want manual", viaSession["triggered_by"])
	}

	// From CI: an API token, as in the README's example.
	rec = doJSON(t, r, http.MethodPost, "/api/api-tokens", map[string]string{"name": "ci"}, cookie)
	var token map[string]string
	decodeBody(t, rec, &token)

	rec = doBearer(t, r, http.MethodPost, checkURL, token["token"])
	if rec.Code != http.StatusOK {
		t.Fatalf("check via API token: status = %d, body = %s", rec.Code, rec.Body)
	}
	var viaToken map[string]any
	decodeBody(t, rec, &viaToken)
	if viaToken["triggered_by"] != "api" {
		t.Errorf("triggered_by via an API token = %v, want api", viaToken["triggered_by"])
	}
}

func TestCheckNowRequiresSession(t *testing.T) {
	r := newTestRouter(t)
	loginAsNewAdmin(t, r) // complete setup, but don't attach the cookie below

	rec := doJSON(t, r, http.MethodPost, "/api/workspaces/some-id/check", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCheckHistoryAndDetail(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, map[string]any{
		"source_path": "/this/path/does/not/exist/anywhere",
	})
	wsID := ws["id"].(string)

	// Run two checks so there's real history to list. The pause keeps their
	// started_at values distinct: both checks fail instantly (the source
	// path doesn't exist), and on a platform with a coarse wall clock
	// (Windows ticks at ~15ms) they'd otherwise tie and sort arbitrarily.
	doJSON(t, r, http.MethodPost, "/api/workspaces/"+wsID+"/check", nil, cookie)
	time.Sleep(20 * time.Millisecond)
	rec := doJSON(t, r, http.MethodPost, "/api/workspaces/"+wsID+"/check", nil, cookie)
	var second map[string]any
	decodeBody(t, rec, &second)
	secondID := second["id"].(string)

	histRec := doJSON(t, r, http.MethodGet, "/api/workspaces/"+wsID+"/checks", nil, cookie)
	if histRec.Code != http.StatusOK {
		t.Fatalf("history: status = %d, body = %s", histRec.Code, histRec.Body)
	}
	var history []map[string]any
	decodeBody(t, histRec, &history)
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	// Newest first.
	if history[0]["id"] != secondID {
		t.Errorf("history[0].id = %v, want %v (newest first)", history[0]["id"], secondID)
	}
	if _, hasResources := history[0]["resources"]; hasResources {
		t.Error("history list should not embed full resource detail")
	}

	detailRec := doJSON(t, r, http.MethodGet, "/api/checks/"+secondID, nil, cookie)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail: status = %d, body = %s", detailRec.Code, detailRec.Body)
	}
	var detail map[string]any
	decodeBody(t, detailRec, &detail)
	if detail["id"] != secondID {
		t.Errorf("detail.id = %v, want %v", detail["id"], secondID)
	}

	missingRec := doJSON(t, r, http.MethodGet, "/api/checks/does-not-exist", nil, cookie)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("missing check: status = %d, want %d", missingRec.Code, http.StatusNotFound)
	}
}

func TestCheckHistoryReturns404ForMissingWorkspace(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodGet, "/api/workspaces/does-not-exist/checks", nil, cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
