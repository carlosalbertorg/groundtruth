// Package httpapi wires groundtruth's HTTP routes: the JSON API under
// /api, a health check for container orchestrators, and the embedded SPA
// for everything else.
package httpapi

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"

	"github.com/carlosalbertorg/groundtruth/internal/auth"
	"github.com/carlosalbertorg/groundtruth/internal/buildinfo"
	"github.com/carlosalbertorg/groundtruth/internal/checks"
	"github.com/carlosalbertorg/groundtruth/internal/store/sqlc"
)

// Deps are NewRouter's dependencies: everything handlers need, built once
// at startup and threaded through rather than reached via globals.
type Deps struct {
	SPA          fs.FS // embedded frontend build (internal/webassets)
	Logger       *slog.Logger
	Queries      *sqlc.Queries
	CheckService *checks.Service

	Sessions      *auth.SessionManager
	SetupGate     *auth.SetupGate
	APITokens     *auth.APITokenManager
	SecureCookies bool // mark the session cookie Secure; see config.Config.SecureCookies
}

// NewRouter builds the root HTTP handler: the JSON API under /api, a
// health check, and the embedded SPA for everything else.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	// First, so even a response written by Recoverer or the rate limiter
	// carries the hardening headers.
	r.Use(securityHeaders)
	r.Use(middleware.RequestID)
	// Deliberately not using chi's middleware.RealIP: it trusts
	// X-Forwarded-For/X-Real-IP unconditionally, which lets a client spoof
	// its logged IP unless every deployment is known to sit behind a
	// trusted reverse proxy that strips those headers. r.RemoteAddr is used
	// as-is until a configurable trusted-proxy allowlist is worth adding.
	r.Use(slogRequestLogger(d.Logger))
	r.Use(middleware.Recoverer)

	r.Get("/healthz", handleHealthz)

	setup := newSetupHandlers(d.Queries, d.SetupGate)
	authH := newAuthHandlers(d.Queries, d.Sessions, d.SecureCookies)
	requireSession := auth.RequireSession(d.Sessions, d.Queries)
	requireSetup := auth.RequireSetupComplete(d.SetupGate)
	// 10 attempts/minute/IP with a burst of 5: enough for a real user who
	// mistypes a password, not enough to brute-force one at any speed
	// that matters.
	loginLimiter := newPerIPLimiter(rate.Every(6*time.Second), 5)
	// Applied explicitly to the ordinary CRUD-ish routes below, not
	// globally: the check-now route can legitimately run for minutes
	// (bounded by that workspace's own, operator-configured
	// check_timeout_seconds, enforced inside the executor itself), so it
	// deliberately has no blanket request timeout layered on top.
	shortTimeout := middleware.Timeout(60 * time.Second)

	r.Route("/api", func(r chi.Router) {
		// Reachable before the first admin account exists; every other
		// route below requires setup to be complete.
		r.Get("/setup/status", setup.status)
		r.With(loginLimiter.middleware, shortTimeout).Post("/setup/admin", setup.createAdmin)

		r.Group(func(r chi.Router) {
			r.Use(requireSetup)

			r.With(loginLimiter.middleware, shortTimeout).Post("/auth/login", authH.login)

			checkH := newCheckHandlers(d.Queries, d.CheckService)
			// Session OR API token: the one route an unattended
			// caller (CI) needs to reach without a browser session.
			r.With(auth.RequireSessionOrAPIToken(d.Sessions, d.APITokens, d.Queries)).
				Post("/workspaces/{id}/check", checkH.runNow)

			r.Group(func(r chi.Router) {
				r.Use(requireSession)
				r.With(shortTimeout).Post("/auth/logout", authH.logout)
				r.With(shortTimeout).Get("/auth/me", authH.me)

				workspaces := newWorkspaceHandlers(d.Queries)
				r.Route("/workspaces", func(r chi.Router) {
					r.Use(shortTimeout)
					r.Get("/", workspaces.list)
					r.Post("/", workspaces.create)
					r.Get("/{id}", workspaces.get)
					r.Patch("/{id}", workspaces.update)
					r.Delete("/{id}", workspaces.delete)
				})

				r.With(shortTimeout).Get("/workspaces/{id}/checks", checkH.history)
				r.With(shortTimeout).Get("/checks/{checkID}", checkH.get)

				alertH := newAlertDestinationHandlers(d.Queries)
				r.Route("/alert-destinations", func(r chi.Router) {
					r.Use(shortTimeout)
					r.Get("/", alertH.list)
					r.Post("/", alertH.create)
					r.Patch("/{id}", alertH.update)
					r.Delete("/{id}", alertH.delete)
				})

				tokenH := newAPITokenHandlers(d.Queries, d.APITokens)
				r.Route("/api-tokens", func(r chi.Router) {
					r.Use(shortTimeout)
					r.Get("/", tokenH.list)
					r.Post("/", tokenH.create)
					r.Delete("/{id}", tokenH.revoke)
				})
			})
		})
	})

	r.NotFound(spaHandler(d.SPA))

	// Rejects cross-origin state-changing requests (CSRF) by checking
	// Sec-Fetch-Site/Origin against Host. This is origin-based, not
	// session-based, so it covers every mutating route uniformly,
	// including setup/login before any session cookie exists.
	csrf := http.NewCrossOriginProtection()
	return csrf.Handler(r)
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": buildinfo.Version,
	})
}

// spaHandler serves static assets from the embedded build, falling back to
// index.html for any path that isn't a real file (client-side routing).
func spaHandler(spa fs.FS) http.HandlerFunc {
	fileServer := http.FileServerFS(spa)
	return func(w http.ResponseWriter, r *http.Request) {
		// An unknown API route must be a JSON 404, never the SPA shell: the
		// shell answers 200, so a mistyped URL in a CI script (curl without
		// -f) would look like a check that was triggered.
		if isAPIPath(r.URL.Path) {
			writeJSONError(w, http.StatusNotFound, "not_found")
			return
		}
		// The shell is a page to read, so only GET/HEAD get it. A POST to a
		// non-route is a client error, not something to answer with HTML.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}

		path := r.URL.Path
		if path != "/" {
			if f, err := spa.Open(trimLeadingSlash(path)); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r2 := new(http.Request)
		*r2 = *r
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	}
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
