package middleware

import (
	"forum/internal/errorpage"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter ограничивает количество запросов
type RateLimiter struct {
	visitors map[string]*visitor
	mu       sync.RWMutex
	rate     int
	burst    int
	quit     chan struct{}
	tmpl     *template.Template
}

type visitor struct {
	last   time.Time
	tokens int
}

// NewRateLimiter создаёт лимитер
func NewRateLimiter(rate, burst int, tmpl *template.Template) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		burst:    burst,
		quit:     make(chan struct{}),
		tmpl:     tmpl,
	}
	go rl.cleanup()
	return rl
}

// Stop shuts down the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.quit)
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			for ip, v := range rl.visitors {
				if time.Since(v.last) > 10*time.Minute {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.quit:
			return
		}
	}
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{tokens: rl.burst, last: time.Now()}
		return true
	}

	elapsed := time.Since(v.last).Seconds()
	v.tokens = min(rl.burst, v.tokens+int(elapsed*float64(rl.rate)))
	v.last = time.Now()

	if v.tokens < 1 {
		return false
	}
	v.tokens--
	return true
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		} else if idx := strings.Index(ip, ","); idx != -1 {
			ip = strings.TrimSpace(ip[:idx])
		}
		if !rl.allow(ip) {
			errorpage.Render(w, rl.tmpl, http.StatusTooManyRequests,
				"Слишком много запросов", "Ты отправляешь запросы слишком часто. Подожди немного.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
