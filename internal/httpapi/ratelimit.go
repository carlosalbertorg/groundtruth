package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// limiterIdleTTL is how long a client's bucket is kept after its last
	// request. It must exceed the time a bucket takes to refill completely
	// (burst divided by rate - 30 seconds for the login limiter): a bucket
	// that has sat idle that long is full again, so dropping it and creating
	// a fresh one on the next request is indistinguishable from keeping it.
	limiterIdleTTL = 15 * time.Minute

	// limiterSweepInterval is how often idle buckets are looked for.
	limiterSweepInterval = time.Minute
)

// perIPLimiter rate-limits an endpoint per client address, so brute-forcing
// a password can't be done at network speed. It isn't a replacement for
// strong passwords — just friction.
//
// Idle buckets are evicted (see limiterIdleTTL), so memory is bounded by the
// number of distinct clients seen in the last few minutes rather than by
// every address the process has ever served. That matters on an
// unauthenticated endpoint: an attacker rotating through source addresses
// could otherwise grow the map without limit.
type perIPLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	// r and b configure every per-client limiter created: r is the sustained
	// rate (tokens/sec) and b is the burst size.
	r rate.Limit
	b int

	now       func() time.Time // injectable so tests can age buckets without sleeping
	lastSweep time.Time
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newPerIPLimiter(r rate.Limit, b int) *perIPLimiter {
	return &perIPLimiter{limiters: make(map[string]*limiterEntry), r: r, b: b, now: time.Now}
}

func (l *perIPLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if now.Sub(l.lastSweep) >= limiterSweepInterval {
		for k, e := range l.limiters {
			if now.Sub(e.lastSeen) >= limiterIdleTTL {
				delete(l.limiters, k)
			}
		}
		l.lastSweep = now
	}

	e, ok := l.limiters[key]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(l.r, l.b)}
		l.limiters[key] = e
	}
	e.lastSeen = now
	return e.limiter.AllowN(now, 1)
}

// middleware rejects requests over the limit with 429, keyed by the
// request's remote address (not X-Forwarded-For — see the RealIP note in
// router.go for why that header isn't trusted here).
func (l *perIPLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientKey(r.RemoteAddr)) {
			writeJSONError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientKey is the rate-limit bucket for a remote address: the address
// itself for IPv4, and the enclosing /64 for IPv6. One IPv6 subscriber
// normally controls a whole /64, so keying on the full address would let a
// single client rotate through 2^64 buckets and never be limited.
func clientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	addr = addr.Unmap() // an IPv4 client on a dual-stack listener arrives as ::ffff:a.b.c.d
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return addr.String()
}
