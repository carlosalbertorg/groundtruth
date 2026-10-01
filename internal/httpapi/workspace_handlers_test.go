package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
)

// loginAsNewAdmin creates the first admin and logs in, returning the
// session cookie every authenticated test needs.
func loginAsNewAdmin(t *testing.T, r http.Handler) *http.Cookie {
	t.Helper()

	rec := doJSON(t, r, http.MethodPost, "/api/setup/admin", map[string]string{
		"email":    "admin@example.com",
		"password": "correct-horse-battery-staple",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create admin: status = %d, body = %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, r, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "admin@example.com",
		"password": "correct-horse-battery-staple",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			return c
		}
	}
	t.Fatal("login did not set a session cookie")
	return nil
}

func createTestWorkspace(t *testing.T, r http.Handler, cookie *http.Cookie, overrides map[string]any) map[string]any {
	t.Helper()

	body := map[string]any{
		"name":        "prod-network",
		"source_path": "/data/modules/prod-network",
		"binary_kind": "terraform",
	}
	for k, v := range overrides {
		body[k] = v
	}

	rec := doJSON(t, r, http.MethodPost, "/api/workspaces", body, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace: status = %d, body = %s", rec.Code, rec.Body)
	}
	var ws map[string]any
	decodeBody(t, rec, &ws)
	return ws
}

func TestWorkspaceCreateAppliesDefaults(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, nil)

	if ws["check_interval_minutes"] != float64(60) {
		t.Errorf("check_interval_minutes = %v, want 60", ws["check_interval_minutes"])
	}
	if ws["check_timeout_seconds"] != float64(600) {
		t.Errorf("check_timeout_seconds = %v, want 600", ws["check_timeout_seconds"])
	}
	if ws["is_enabled"] != true {
		t.Errorf("is_enabled = %v, want true", ws["is_enabled"])
	}
	if ws["description"] != nil {
		t.Errorf("description = %v, want nil", ws["description"])
	}
}

func TestWorkspaceCreateValidation(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	cases := []struct {
		name     string
		body     map[string]any
		wantCode string
	}{
		{
			name:     "missing name",
			body:     map[string]any{"name": "", "source_path": "/x", "binary_kind": "terraform"},
			wantCode: "name_required",
		},
		{
			name:     "missing source path",
			body:     map[string]any{"name": "a", "source_path": "  ", "binary_kind": "terraform"},
			wantCode: "source_path_required",
		},
		{
			name:     "invalid binary kind",
			body:     map[string]any{"name": "a", "source_path": "/x", "binary_kind": "pulumi"},
			wantCode: "invalid_binary_kind",
		},
		{
			name: "non-positive check interval",
			body: map[string]any{
				"name": "a", "source_path": "/x", "binary_kind": "terraform",
				"check_interval_minutes": 0,
			},
			wantCode: "invalid_check_interval_minutes",
		},
		{
			name: "non-positive check timeout",
			body: map[string]any{
				"name": "a", "source_path": "/x", "binary_kind": "terraform",
				"check_timeout_seconds": -5,
			},
			wantCode: "invalid_check_timeout_seconds",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, r, http.MethodPost, "/api/workspaces", tc.body, cookie)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			var errResp struct {
				Error string `json:"error"`
			}
			decodeBody(t, rec, &errResp)
			if errResp.Error != tc.wantCode {
				t.Errorf("error = %q, want %q", errResp.Error, tc.wantCode)
			}
		})
	}
}

func TestWorkspaceCreateDuplicateNameConflicts(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	createTestWorkspace(t, r, cookie, nil)

	rec := doJSON(t, r, http.MethodPost, "/api/workspaces", map[string]any{
		"name":        "prod-network",
		"source_path": "/data/modules/other",
		"binary_kind": "tofu",
	}, cookie)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusConflict, rec.Body)
	}
}

func TestWorkspaceGetAndList(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, map[string]any{"name": "b-workspace"})
	createTestWorkspace(t, r, cookie, map[string]any{"name": "a-workspace"})

	rec := doJSON(t, r, http.MethodGet, "/api/workspaces/"+ws["id"].(string), nil, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d", rec.Code)
	}

	rec = doJSON(t, r, http.MethodGet, "/api/workspaces/does-not-exist", nil, cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec = doJSON(t, r, http.MethodGet, "/api/workspaces", nil, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d", rec.Code)
	}
	var list []map[string]any
	decodeBody(t, rec, &list)
	if len(list) != 2 {
		t.Fatalf("list length = %d, want 2", len(list))
	}
	if list[0]["name"] != "a-workspace" || list[1]["name"] != "b-workspace" {
		t.Errorf("list order = [%v, %v], want [a-workspace, b-workspace]", list[0]["name"], list[1]["name"])
	}
}

func TestWorkspaceUpdate(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, nil)

	rec := doJSON(t, r, http.MethodPatch, "/api/workspaces/"+ws["id"].(string), map[string]any{
		"name":                   "prod-network-renamed",
		"source_path":            "/data/modules/prod-network",
		"binary_kind":            "tofu",
		"is_enabled":             false,
		"check_interval_minutes": 15,
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", rec.Code, rec.Body)
	}
	var updated map[string]any
	decodeBody(t, rec, &updated)
	if updated["name"] != "prod-network-renamed" {
		t.Errorf("name = %v, want prod-network-renamed", updated["name"])
	}
	if updated["binary_kind"] != "tofu" {
		t.Errorf("binary_kind = %v, want tofu", updated["binary_kind"])
	}
	if updated["is_enabled"] != false {
		t.Errorf("is_enabled = %v, want false", updated["is_enabled"])
	}

	rec = doJSON(t, r, http.MethodPatch, "/api/workspaces/does-not-exist", map[string]any{
		"name": "x", "source_path": "/x", "binary_kind": "terraform",
	}, cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update missing: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestWorkspaceDelete(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	ws := createTestWorkspace(t, r, cookie, nil)
	id := ws["id"].(string)

	rec := doJSON(t, r, http.MethodDelete, "/api/workspaces/"+id, nil, cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", rec.Code)
	}

	rec = doJSON(t, r, http.MethodGet, "/api/workspaces/"+id, nil, cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: status = %d, want %d", rec.Code, http.StatusNotFound)
	}

	rec = doJSON(t, r, http.MethodDelete, "/api/workspaces/"+id, nil, cookie)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete again: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestWorkspaceRoutesRequireSession(t *testing.T) {
	r := newTestRouter(t)
	// Complete setup first, with no cookie attached, so this request is
	// rejected for lacking a session specifically - not just because
	// setup isn't done yet (that's a separate 503, tested elsewhere).
	loginAsNewAdmin(t, r)

	rec := doJSON(t, r, http.MethodGet, "/api/workspaces", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
