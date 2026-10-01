package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestClientKey(t *testing.T) {
	cases := []struct {
		remoteAddr string
		want       string
	}{
		{"192.0.2.10:5555", "192.0.2.10"},
		{"192.0.2.10", "192.0.2.10"}, // no port
		{"[2001:db8:abcd:12::1]:443", "2001:db8:abcd:12::/64"},
		{"[2001:db8:abcd:12:ffff:ffff:ffff:ffff]:80", "2001:db8:abcd:12::/64"}, // same /64, different host bits
		{"[2001:db8:abcd:13::1]:443", "2001:db8:abcd:13::/64"},                 // a different /64
		{"[::ffff:192.0.2.10]:80", "192.0.2.10"},                               // IPv4 client on a dual-stack socket
		{"not-an-address", "not-an-address"},
	}
	for _, tc := range cases {
		if got := clientKey(tc.remoteAddr); got != tc.want {
			t.Errorf("clientKey(%q) = %q, want %q", tc.remoteAddr, got, tc.want)
		}
	}
}

func TestLimiterTreatsAnIPv6PrefixAsOneClient(t *testing.T) {
	limited := newPerIPLimiter(rate.Every(time.Hour), 2).middleware(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	status := func(remoteAddr string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, req)
		return rec.Code
	}

	// Three different addresses in one /64 share a burst of two: rotating the
	// host bits must not buy a fresh budget.
	if got := status("[2001:db8:abcd:12::1]:1"); got != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", got)
	}
	if got := status("[2001:db8:abcd:12::2]:1"); got != http.StatusOK {
		t.Fatalf("second request: status = %d, want 200", got)
	}
	if got := status("[2001:db8:abcd:12::3]:1"); got != http.StatusTooManyRequests {
		t.Fatalf("third request from the same /64: status = %d, want 429", got)
	}

	// A different /64 has its own budget.
	if got := status("[2001:db8:abcd:99::1]:1"); got != http.StatusOK {
		t.Fatalf("request from a different /64: status = %d, want 200", got)
	}
}

func TestLimiterEvictsIdleBuckets(t *testing.T) {
	// The production login limiter's parameters: it refills completely in
	// 30s, well inside limiterIdleTTL, which is what makes eviction safe.
	l := newPerIPLimiter(rate.Every(6*time.Second), 5)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }

	for range 5 {
		if !l.allow("noisy-client") {
			t.Fatal("a request within the burst was refused")
		}
	}
	if l.allow("noisy-client") {
		t.Fatal("a request beyond the burst was allowed")
	}
	if len(l.limiters) != 1 {
		t.Fatalf("buckets = %d, want 1", len(l.limiters))
	}

	// Long after the TTL, a request from somebody else triggers a sweep that
	// drops the idle bucket.
	clock = clock.Add(limiterIdleTTL + limiterSweepInterval)
	if !l.allow("other-client") {
		t.Fatal("a fresh client's first request was refused")
	}
	if len(l.limiters) != 1 {
		t.Errorf("buckets after the sweep = %d, want 1 (only the active client)", len(l.limiters))
	}
	if _, stillThere := l.limiters["noisy-client"]; stillThere {
		t.Error("the idle bucket was not evicted")
	}

	// Having been evicted, the client starts over with a full burst - the same
	// state it would be in had its bucket stayed and refilled.
	if !l.allow("noisy-client") {
		t.Error("a returning client was refused after its idle bucket was evicted")
	}
}

func TestLimiterKeepsActiveBucketsAcrossASweep(t *testing.T) {
	l := newPerIPLimiter(rate.Every(6*time.Second), 5)
	clock := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return clock }

	l.allow("steady-client")
	// Requests keep arriving more often than the TTL, across several sweeps.
	for range 40 {
		clock = clock.Add(limiterSweepInterval)
		l.allow("steady-client")
	}
	if _, ok := l.limiters["steady-client"]; !ok {
		t.Error("an actively used bucket was evicted")
	}
}
