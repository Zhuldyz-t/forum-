package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestLimiter(userID int) (*RateLimiter, *time.Time) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rl := NewRateLimiter(DefaultLimits, func(*http.Request) int { return userID }, nil)
	rl.now = func() time.Time { return now }
	return rl, &now
}

func do(rl *RateLimiter, method, path, ip string) int {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = ip + ":1234"
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	rl.Middleware(ok).ServeHTTP(rec, req)
	return rec.Code
}

func TestLoginLimitedToFivePerMinute(t *testing.T) {
	rl, now := newTestLimiter(0)
	defer rl.Stop()
	for i := 0; i < 5; i++ {
		if code := do(rl, http.MethodPost, "/login", "10.0.0.1"); code != 200 {
			t.Fatalf("attempt %d: got %d", i+1, code)
		}
	}
	if code := do(rl, http.MethodPost, "/login", "10.0.0.1"); code != 429 {
		t.Fatalf("6th attempt: got %d, want 429", code)
	}
	// Another IP is not affected; the GET form page is not an attempt.
	if code := do(rl, http.MethodPost, "/login", "10.0.0.2"); code != 200 {
		t.Fatalf("other IP: got %d", code)
	}
	if code := do(rl, http.MethodGet, "/login", "10.0.0.1"); code != 200 {
		t.Fatalf("GET /login: got %d", code)
	}
	// After 12 s one more attempt fits (5 per minute).
	*now = now.Add(12 * time.Second)
	if code := do(rl, http.MethodPost, "/login", "10.0.0.1"); code != 200 {
		t.Fatalf("after refill: got %d", code)
	}
}

func TestContentLimitPerUserAcrossIPs(t *testing.T) {
	rl, _ := newTestLimiter(42)
	defer rl.Stop()
	for i := 0; i < 10; i++ {
		// A new IP each time: only the per-user bucket can stop this.
		if code := do(rl, http.MethodPost, "/comment/create", "10.1.0."+string(rune('0'+i))); code != 200 {
			t.Fatalf("comment %d: got %d", i+1, code)
		}
	}
	if code := do(rl, http.MethodPost, "/comment/create", "10.1.1.1"); code != 429 {
		t.Fatalf("11th comment from a new IP: got %d, want 429", code)
	}
}

func TestDefaultLimitAndStaticExempt(t *testing.T) {
	rl, _ := newTestLimiter(0)
	defer rl.Stop()
	for i := 0; i < 100; i++ {
		if code := do(rl, http.MethodGet, "/", "10.2.0.1"); code != 200 {
			t.Fatalf("request %d: got %d", i+1, code)
		}
	}
	if code := do(rl, http.MethodGet, "/", "10.2.0.1"); code != 429 {
		t.Fatalf("101st request: got %d, want 429", code)
	}
	if code := do(rl, http.MethodGet, "/static/style.css", "10.2.0.1"); code != 200 {
		t.Fatalf("static file: got %d", code)
	}
}
