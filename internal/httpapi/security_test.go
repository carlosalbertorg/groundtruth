package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestResponsesCarryHardeningHeaders(t *testing.T) {
	r := newTestRouter(t)

	// One of each kind of response: a plain route, an API route, and the
	// embedded SPA shell.
	for _, path := range []string{"/healthz", "/api/setup/status", "/"} {
		t.Run(path, func(t *testing.T) {
			rec := doJSON(t, r, http.MethodGet, path, nil)

			csp := rec.Header().Get("Content-Security-Policy")
			for _, directive := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
				if !strings.Contains(csp, directive) {
					t.Errorf("Content-Security-Policy = %q, want it to contain %q", csp, directive)
				}
			}
			if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") || strings.Contains(csp, "'unsafe-eval'") {
				t.Errorf("Content-Security-Policy = %q: script execution must not be loosened", csp)
			}
			for header, want := range map[string]string{
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
				"Referrer-Policy":        "no-referrer",
			} {
				if got := rec.Header().Get(header); got != want {
					t.Errorf("%s = %q, want %q", header, got, want)
				}
			}
		})
	}
}

func TestAPIResponsesAreNotCacheable(t *testing.T) {
	r := newTestRouter(t)

	rec := doJSON(t, r, http.MethodGet, "/api/setup/status", nil)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control on an API response = %q, want no-store", got)
	}

	// Errors are API responses too - and an unknown route must not escape.
	rec = doJSON(t, r, http.MethodGet, "/api/no-such-route", nil)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control on an API 404 = %q, want no-store", got)
	}
}

func TestUnknownAPIRouteIsAJSON404NotTheSPAShell(t *testing.T) {
	r := newTestRouter(t)
	cookie := loginAsNewAdmin(t, r)

	// The README's CI example is a POST; a typo in the path must fail loudly
	// instead of returning the SPA's HTML with a 200.
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/workspace/abc/check"}, // typo in the first segment
		{http.MethodGet, "/api/no-such-route"},
		{http.MethodGet, "/api"},
		{http.MethodPost, "/api/workspaces/abc/chek"}, // typo inside a real, mounted prefix
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := doJSON(t, r, tc.method, tc.path, nil, cookie)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want JSON", ct)
			}
			var body struct {
				Error string `json:"error"`
			}
			decodeBody(t, rec, &body)
			if body.Error != "not_found" {
				t.Errorf("error = %q, want not_found", body.Error)
			}
		})
	}
}

func TestMistypedAPIPathNeverLooksLikeSuccessToACICaller(t *testing.T) {
	r := newTestRouter(t)
	loginAsNewAdmin(t, r) // setup complete; the CI caller below has only a token

	// A CI job using `curl` without -f exits 0 on any HTTP response, so the
	// status code is all that stands between a typo and a pipeline that
	// believes a drift check ran. It must never be 2xx.
	for _, path := range []string{"/api/workspace/abc/check", "/api/workspaces/abc/chek"} {
		rec := doBearer(t, r, http.MethodPost, path, "gt_not-a-real-token")
		if rec.Code >= 200 && rec.Code < 300 {
			t.Errorf("POST %s: status = %d, want an error status", path, rec.Code)
		}
	}
}

func TestSPAShellIsOnlyServedForReads(t *testing.T) {
	r := newTestRouter(t)

	rec := doJSON(t, r, http.MethodGet, "/workspaces/some-id", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET of a client-side route: status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = doJSON(t, r, http.MethodPost, "/workspaces/some-id", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST to a client-side route: status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want %q", got, "GET, HEAD")
	}
}
