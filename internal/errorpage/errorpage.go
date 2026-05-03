package errorpage

import (
	"html/template"
	"log"
	"net/http"
)

// Data is passed to the error.html template.
type Data struct {
	Code    int
	Title   string
	Message string
}

// Render writes a styled error page using the shared template set.
// Falls back to plain text if the template fails.
func Render(w http.ResponseWriter, tmpl *template.Template, status int, title, message string) {
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, "error.html", Data{Code: status, Title: title, Message: message}); err != nil {
		log.Printf("[TEMPLATE ERROR] error.html: %v", err)
		http.Error(w, message, status)
	}
}
