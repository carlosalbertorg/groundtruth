package httpapi_test

import (
	"net/http"
	"testing"
)

func TestCheckNowReturns404ForMissingWorkspace(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodPost, "/api/workspaces/does-not-exist/check", nil, cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body)
	}
}

func TestCheckNowReturns502WhenSourcePathDoesNotExist(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, map[string]any{
		"source_path": "/this/path/does/not/exist/anywhere",
	})

	rec := doJSON(t, r, http.MethodPost, "/api/workspaces/"+ws["id"].(string)+"/check", nil, cookie)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadGateway, rec.Body)
	}

	var body map[string]string
	decodeBody(t, rec, &body)
	if body["error"] != "check_failed" {
		t.Errorf("error = %q, want check_failed", body["error"])
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
