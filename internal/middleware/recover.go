package middleware

import (
	"forum/internal/errorpage"
	"html/template"
	"log"
	"net/http"
	"runtime/debug"
)

// RecoveryMiddleware восстанавливает сервер после паник
func RecoveryMiddleware(next http.Handler, tmpl *template.Template) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[PANIC] %s %s: %v\n%s", r.Method, r.URL.Path, err, debug.Stack())
				errorpage.Render(w, tmpl, http.StatusInternalServerError,
					"Ошибка сервера", "Что-то пошло не так. Попробуй позже.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
