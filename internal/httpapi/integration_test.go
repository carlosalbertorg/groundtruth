package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/checks"
	"github.com/carlosalbertorg/groundtruth/internal/httpapi"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
	"github.com/carlosalbertorg/groundtruth/internal/store/storetest"
	"github.com/carlosalbertorg/groundtruth/internal/terraform"
	"github.com/carlosalbertorg/groundtruth/internal/webassets"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()

	queries := sqlc.New(storetest.OpenDB(t))
	spa, err := webassets.Dist()
	if err != nil {
		t.Fatalf("webassets.Dist: %v", err)
	}

	executor, err := terraform.NewExecutor(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("terraform.NewExecutor: %v", err)
	}

	return httpapi.NewRouter(httpapi.Deps{
		SPA:           spa,
		Logger:        slog.New(slog.DiscardHandler),
		Queries:       queries,
		CheckService:  checks.NewService(queries, executor),
		Sessions:      auth.NewSessionManager(queries),
		SetupGate:     auth.NewSetupGate(queries),
		APITokens:     auth.NewAPITokenManager(queries),
		SecureCookies: false,
	})
}

func doJSON(t *testing.T, r http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// doBearer is doJSON's counterpart for routes authenticated via
// "Authorization: Bearer <token>" instead of a session cookie.
func doBearer(t *testing.T, r http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(v); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
}

// TestFirstRunSetupAndLoginFlow exercises phase 1 end-to-end through the
// real HTTP router: no admin exists yet, every protected route is gated,
// the first admin is created, and that admin can log in, check their
// session, and log out again.
func TestFirstRunSetupAndLoginFlow(t *testing.T) {
	r := newTestRouter(t)

	// Before setup: status says incomplete, and login is gated.
	rec := doJSON(t, r, http.MethodGet, "/api/setup/status", nil)
	var status struct {
		SetupComplete bool `json:"setup_complete"`
	}
	decodeBody(t, rec, &status)
	if status.SetupComplete {
		t.Fatal("setup reported complete before any admin was created")
	}

	rec = doJSON(t, r, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "admin@example.com",
		"password": "whatever-password",
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("login before setup: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	// Create the first admin.
	rec = doJSON(t, r, http.MethodPost, "/api/setup/admin", map[string]string{
		"email":    "admin@example.com",
		"password": "correct-horse-battery-staple",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create admin: status = %d, body = %s", rec.Code, rec.Body)
	}

	// Setup is now complete, and a second attempt is rejected.
	rec = doJSON(t, r, http.MethodGet, "/api/setup/status", nil)
	decodeBody(t, rec, &status)
	if !status.SetupComplete {
		t.Fatal("setup reported incomplete after creating the first admin")
	}

	rec = doJSON(t, r, http.MethodPost, "/api/setup/admin", map[string]string{
		"email":    "someone-else@example.com",
		"password": "another-long-password",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("second setup attempt: status = %d, want %d", rec.Code, http.StatusConflict)
	}

	// Wrong password is rejected.
	rec = doJSON(t, r, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "admin@example.com",
		"password": "wrong-password",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with wrong password: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// Correct password succeeds and sets a session cookie.
	rec = doJSON(t, r, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "admin@example.com",
		"password": "correct-horse-battery-staple",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body = %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set a session cookie")
	}
	if !sessionCookie.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}

	// /api/auth/me fails without the cookie, succeeds with it.
	rec = doJSON(t, r, http.MethodGet, "/api/auth/me", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without cookie: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	rec = doJSON(t, r, http.MethodGet, "/api/auth/me", nil, sessionCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("me with cookie: status = %d, body = %s", rec.Code, rec.Body)
	}
	var me struct {
		Email string `json:"email"`
	}
	decodeBody(t, rec, &me)
	if me.Email != "admin@example.com" {
		t.Errorf("me returned email %q, want %q", me.Email, "admin@example.com")
	}

	// Logout revokes the session; the same cookie no longer works.
	rec = doJSON(t, r, http.MethodPost, "/api/auth/logout", nil, sessionCookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	rec = doJSON(t, r, http.MethodGet, "/api/auth/me", nil, sessionCookie)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHealthz(t *testing.T) {
	r := newTestRouter(t)
	rec := doJSON(t, r, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: status = %d", rec.Code)
	}
}
