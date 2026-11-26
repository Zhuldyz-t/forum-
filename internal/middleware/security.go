package middleware

import "net/http"

// SecurityHeaders sets protective headers on every response. The CSP allows
// scripts only from this site (no inline scripts), styles from this site and
// Google Fonts (inline style attributes are used for avatar colors and
// progress bars), and WebSocket connections back to the same host.
func SecurityHeaders(tls bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", "default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self' ws://"+r.Host+" wss://"+r.Host+"; "+
				"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if tls {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}
