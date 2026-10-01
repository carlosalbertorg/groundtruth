package httpapi

import (
	"net/http"
	"strings"
)

// contentSecurityPolicy locks the dashboard down to what the embedded SPA
// actually loads: one module script and one stylesheet, both served from
// this origin, plus API calls back to it. Nothing is inline except styles
// ('unsafe-inline' is for style-src only: Radix UI, used by the dialogs,
// injects <style> elements at runtime), so a script injected into a page,
// or a third-party include, is refused by the browser.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// securityHeaders sets the response headers that harden a browser's
// handling of the dashboard: no framing (clickjacking), no MIME sniffing,
// no Referer leakage, a strict CSP, and no caching of API responses.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// Everything under /api is authenticated, per-user data (check
		// results, alert destinations). None of it belongs in a browser
		// cache or an intermediary's.
		if isAPIPath(r.URL.Path) {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// isAPIPath reports whether p is the JSON API's namespace.
func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}
