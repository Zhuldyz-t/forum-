package logger

import (
	"bufio"
	"errors"
	"log"
	"net"
	"net/http"
	"time"
)

// responseWriter оборачивает http.ResponseWriter для захвата статуса
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack passes the connection through for WebSocket upgrades: without it
// the wrapper hides the underlying http.Hijacker and upgrades fail with 500.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacking not supported")
	}
	rw.status = http.StatusSwitchingProtocols
	return h.Hijack()
}

// Unwrap lets http.ResponseController reach the original writer.
func (rw *responseWriter) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

// LoggingMiddleware логирует запросы
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		log.Printf("[HTTP] %s %s %d %v", r.Method, r.URL.Path, wrapped.status, time.Since(start))
	})
}

// Error логирует ошибку с контекстом
func Error(prefix string, err error) {
	if err != nil {
		log.Printf("[ERROR] %s: %v", prefix, err)
	}
}
