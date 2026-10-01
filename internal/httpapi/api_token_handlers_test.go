package httpapi_test

import (
	"net/http"
	"testing"
)

func TestAPITokenCreateListRevoke(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodPost, "/api/api-tokens", map[string]any{"name": "ci"}, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body)
	}
	var created map[string]any
	decodeBody(t, rec, &created)
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatal("create did not return a token value")
	}
	id := created["id"].(string)

	listRec := doJSON(t, r, http.MethodGet, "/api/api-tokens", nil, cookie)
	var list []map[string]any
	decodeBody(t, listRec, &list)
	if len(list) != 1 {
		t.Fatalf("list length = %d, want 1", len(list))
	}
	if _, leaked := list[0]["token"]; leaked {
		t.Error("list response leaked the raw token value")
	}

	revokeRec := doJSON(t, r, http.MethodDelete, "/api/api-tokens/"+id, nil, cookie)
	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d", revokeRec.Code)
	}

	// The revoked token must no longer work against the bearer-auth route.
	checkRec := doBearer(t, r, http.MethodPost, "/api/workspaces/does-not-exist/check", token)
	if checkRec.Code != http.StatusUnauthorized {
		t.Fatalf("status with revoked token = %d, want %d", checkRec.Code, http.StatusUnauthorized)
	}
}

func TestCheckNowAcceptsAPIToken(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, map[string]any{
		"source_path": "/this/path/does/not/exist/anywhere",
	})

	rec := doJSON(t, r, http.MethodPost, "/api/api-tokens", map[string]any{"name": "ci"}, cookie)
	var created map[string]any
	decodeBody(t, rec, &created)
	token := created["token"].(string)

	checkRec := doBearer(t, r, http.MethodPost, "/api/workspaces/"+ws["id"].(string)+"/check", token)
	if checkRec.Code != http.StatusOK {
		t.Fatalf("check via bearer token: status = %d, body = %s", checkRec.Code, checkRec.Body)
	}
}

func TestCheckNowRejectsInvalidBearerToken(t *testing.T) {
	r := newTestRouter(t)
	loginAsNewAdmin(t, r)

	rec := doBearer(t, r, http.MethodPost, "/api/workspaces/some-id/check", "gt_not-a-real-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
