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

	"github.com/carlosalbertorg/groundtruth/internal/buildinfo"
)

// NewRouter builds the root HTTP handler. spa serves the embedded frontend
// build output (see internal/webassets); it is an http.FileSystem-style
// fs.FS rooted at the build's index.html.
func NewRouter(spa fs.FS, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	// Deliberately not using chi's middleware.RealIP: it trusts
	// X-Forwarded-For/X-Real-IP unconditionally, which lets a client spoof
	// its logged IP unless every deployment is known to sit behind a
	// trusted reverse proxy that strips those headers. r.RemoteAddr is used
	// as-is until a configurable trusted-proxy allowlist is worth adding.
	r.Use(slogRequestLogger(logger))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/healthz", handleHealthz)

	r.Route("/api", func(_ chi.Router) {
		// Authenticated API routes are added in later phases (sessions,
		// workspaces, checks, alerting). Nothing lives here yet.
	})

	r.NotFound(spaHandler(spa))

	return r
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
