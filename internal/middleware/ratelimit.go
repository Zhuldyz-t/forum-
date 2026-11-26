package middleware

import (
	"forum/internal/errorpage"
	"html/template"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limits are requests per minute for each class of request.
type Limits struct {
	Auth    int // POST /login, POST /register, OAuth sign-in
	Content int // creating posts and comments
	Default int // everything else (static files and uploads are not limited)
}

var DefaultLimits = Limits{Auth: 5, Content: 10, Default: 100}

// RateLimiter is a token bucket per (class, IP) and per (class, user):
// a request must fit into both. Buckets refill continuously.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limits  Limits
	userID  func(*http.Request) int // 0 for guests
	tmpl    *template.Template
	quit    chan struct{}
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter creates a limiter. userID resolves the logged-in user so
// limits also apply per account, not only per IP.
func NewRateLimiter(limits Limits, userID func(*http.Request) int, tmpl *template.Template) *RateLimiter {
	rl := &RateLimiter{
		buckets: map[string]*bucket{},
		limits:  limits,
		userID:  userID,
		tmpl:    tmpl,
		quit:    make(chan struct{}),
		now:     time.Now,
	}
	go rl.cleanup()
	return rl
}

// Stop shuts down the background cleanup goroutine.
func (rl *RateLimiter) Stop() { close(rl.quit) }

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			for k, b := range rl.buckets {
				if rl.now().Sub(b.last) > 10*time.Minute {
					delete(rl.buckets, k)
				}
			}
			rl.mu.Unlock()
		case <-rl.quit:
			return
		}
	}
}

// classify returns the limit class of a request, or "" when it isn't limited.
func (rl *RateLimiter) classify(r *http.Request) (string, int) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/static/"), strings.HasPrefix(p, "/uploads/"):
		return "", 0
	case r.Method == http.MethodPost && (p == "/login" || p == "/register"),
		strings.HasPrefix(p, "/auth/"):
		return "auth", rl.limits.Auth
	case r.Method == http.MethodPost && (p == "/post/create" || p == "/comment/create"):
		return "content", rl.limits.Content
	}
	return "default", rl.limits.Default
}

// take consumes one token from every key, or none if any bucket is empty;
// it returns how long until a token is available.
func (rl *RateLimiter) take(perMinute int, keys ...string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := rl.now()
	rate := float64(perMinute) / 60 // tokens per second
	var wait time.Duration
	bs := make([]*bucket, 0, len(keys))
	for _, k := range keys {
		b, ok := rl.buckets[k]
		if !ok {
			b = &bucket{tokens: float64(perMinute), last: now}
			rl.buckets[k] = b
		}
		b.tokens = math.Min(float64(perMinute), b.tokens+now.Sub(b.last).Seconds()*rate)
		b.last = now
		if b.tokens < 1 {
			if d := time.Duration((1 - b.tokens) / rate * float64(time.Second)); d > wait {
				wait = d
			}
		}
		bs = append(bs, b)
	}
	if wait > 0 {
		return false, wait
	}
	for _, b := range bs {
		b.tokens--
	}
	return true, 0
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip, _, _ := strings.Cut(fwd, ",")
		return strings.TrimSpace(ip)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		class, limit := rl.classify(r)
		if class == "" || limit <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		keys := []string{class + "|ip|" + clientIP(r)}
		if rl.userID != nil {
			if id := rl.userID(r); id > 0 {
				keys = append(keys, class+"|user|"+strconv.Itoa(id))
			}
		}
		if ok, wait := rl.take(limit, keys...); !ok {
			secs := int(math.Ceil(wait.Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			msg := "Слишком много запросов. Подождите " + strconv.Itoa(secs) + " сек. и попробуйте снова."
			switch class {
			case "auth":
				msg = "Слишком много попыток входа. Подождите " + strconv.Itoa(secs) + " сек."
			case "content":
				msg = "Вы публикуете слишком часто. Подождите " + strconv.Itoa(secs) + " сек."
			}
			if strings.Contains(r.Header.Get("Accept"), "application/json") {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"` + msg + `"}`)) //nolint:errcheck
				return
			}
			errorpage.Render(w, rl.tmpl, http.StatusTooManyRequests, "Слишком много запросов", msg)
			return
		}
		next.ServeHTTP(w, r)
	})
}
