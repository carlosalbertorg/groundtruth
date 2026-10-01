package httpapi

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// perIPLimiter rate-limits an endpoint per client IP, so brute-forcing a
// password can't be done at network speed. It isn't a replacement for
// strong passwords — just friction.
//
// The per-IP map grows for the life of the process with no eviction.
// Acceptable for v1's target deployment (a single admin-facing instance,
// not an internet-scale service) — revisit if that stops being true.
type perIPLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	// r and b configure every per-IP limiter created: r is the sustained
	// rate (tokens/sec) and b is the burst size.
	r rate.Limit
	b int
}

func newPerIPLimiter(r rate.Limit, b int) *perIPLimiter {
	return &perIPLimiter{limiters: make(map[string]*rate.Limiter), r: r, b: b}
}

func (l *perIPLimiter) allow(ip string) bool {
	l.mu.Lock()
	lim, ok := l.limiters[ip]
	if !ok {
		lim = rate.NewLimiter(l.r, l.b)
		l.limiters[ip] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// middleware rejects requests over the limit with 429, keyed by the
// request's remote IP (not X-Forwarded-For — see the RealIP note in
// router.go for why that header isn't trusted here).
func (l *perIPLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if !l.allow(host) {
			writeJSONError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}
