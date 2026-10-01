package httpapi_test

import (
	"net/http"
	"testing"
)

func TestAlertDestinationCRUD(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodPost, "/api/alert-destinations", map[string]any{
		"name":          "ops-webhook",
		"kind":          "generic_webhook",
		"url":           "https://example.com/hook",
		"shared_secret": "top-secret",
	}, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body)
	}
	var created map[string]any
	decodeBody(t, rec, &created)
	if created["has_secret"] != true {
		t.Errorf("has_secret = %v, want true", created["has_secret"])
	}
	if _, leaked := created["shared_secret"]; leaked {
		t.Error("response leaked the shared_secret field")
	}
	id := created["id"].(string)

	listRec := doJSON(t, r, http.MethodGet, "/api/alert-destinations", nil, cookie)
	var list []map[string]any
	decodeBody(t, listRec, &list)
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}

	// Update without shared_secret should keep the existing one.
	updateRec := doJSON(t, r, http.MethodPatch, "/api/alert-destinations/"+id, map[string]any{
		"name": "ops-webhook-renamed",
		"kind": "generic_webhook",
		"url":  "https://example.com/hook",
	}, cookie)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", updateRec.Code, updateRec.Body)
	}
	var updated map[string]any
	decodeBody(t, updateRec, &updated)
	if updated["has_secret"] != true {
		t.Error("expected shared_secret to be preserved when omitted from an update")
	}
	if updated["name"] != "ops-webhook-renamed" {
		t.Errorf("name = %v, want ops-webhook-renamed", updated["name"])
	}

	// Explicitly clearing the secret.
	clearRec := doJSON(t, r, http.MethodPatch, "/api/alert-destinations/"+id, map[string]any{
		"name": "ops-webhook-renamed", "kind": "generic_webhook", "url": "https://example.com/hook",
		"shared_secret": "",
	}, cookie)
	var cleared map[string]any
	decodeBody(t, clearRec, &cleared)
	if cleared["has_secret"] != false {
		t.Error("expected shared_secret to be cleared by an explicit empty string")
	}

	deleteRec := doJSON(t, r, http.MethodDelete, "/api/alert-destinations/"+id, nil, cookie)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", deleteRec.Code)
	}
}

func TestAlertDestinationValidation(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodPost, "/api/alert-destinations", map[string]any{
		"name": "x", "kind": "pagerduty", "url": "https://example.com",
	}, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body)
	}
}

func TestAlertDestinationRejectsURLsTheDispatcherCannotPost(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	for _, badURL := range []string{
		"not a url",
		"/relative/path",
		"ftp://example.com/hook",
		"javascript:alert(1)",
		"https://", // a scheme with no host
		"example.com/hook",
	} {
		t.Run(badURL, func(t *testing.T) {
			rec := doJSON(t, r, http.MethodPost, "/api/alert-destinations", map[string]any{
				"name": "x", "kind": "generic_webhook", "url": badURL,
			}, cookie)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body)
			}
			var errResp struct {
				Error string `json:"error"`
			}
			decodeBody(t, rec, &errResp)
			if errResp.Error != "invalid_url" {
				t.Errorf("error = %q, want invalid_url", errResp.Error)
			}
		})
	}

	// Updating an existing destination to a bad URL is rejected too.
	rec := doJSON(t, r, http.MethodPost, "/api/alert-destinations", map[string]any{
		"name": "ok", "kind": "slack", "url": "https://hooks.example.com/abc",
	}, cookie)
	var created map[string]any
	decodeBody(t, rec, &created)

	rec = doJSON(t, r, http.MethodPatch, "/api/alert-destinations/"+created["id"].(string), map[string]any{
		"name": "ok", "kind": "slack", "url": "ftp://example.com/abc",
	}, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update with a bad URL: status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body)
	}
}

func TestAlertDestinationsRequireSession(t *testing.T) {
	r := newTestRouter(t)
	loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodGet, "/api/alert-destinations", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
