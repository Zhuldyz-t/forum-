package handlers

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"forum/internal/errorpage"
	"forum/internal/models"
	"forum/internal/repository"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	DB            *sql.DB
	Tmpl          *template.Template
	Users         *repository.UserRepository
	Sessions      *repository.SessionRepository
	Posts         *repository.PostRepository
	Comments      *repository.CommentRepository
	Reactions     *repository.ReactionRepository
	Categories    *repository.CategoryRepository
	SessionMaxAge time.Duration
	CSRFSecret    []byte
}

func NewHandler(db *sql.DB, tmpl *template.Template) *Handler {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(err)
	}
	return &Handler{
		DB:            db,
		Tmpl:          tmpl,
		Users:         &repository.UserRepository{DB: db},
		Sessions:      &repository.SessionRepository{DB: db},
		Posts:         &repository.PostRepository{DB: db},
		Comments:      &repository.CommentRepository{DB: db},
		Reactions:     &repository.ReactionRepository{DB: db},
		Categories:    &repository.CategoryRepository{DB: db},
		SessionMaxAge: 24 * time.Hour,
		CSRFSecret:    secret,
	}
}

func (h *Handler) render(w http.ResponseWriter, name string, data any, status int) {
	var buf bytes.Buffer
	if err := h.Tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("[TEMPLATE ERROR] %s: %v", name, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	buf.WriteTo(w) //nolint:errcheck
}

func (h *Handler) renderError(w http.ResponseWriter, status int, title, message string) {
	errorpage.Render(w, h.Tmpl, status, title, message)
}

func (h *Handler) NotFound(w http.ResponseWriter, r *http.Request) {
	h.renderError(w, http.StatusNotFound, "Страница не найдена", "Запрошенная страница не существует.")
}

func (h *Handler) methodRequired(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		h.renderError(w, http.StatusMethodNotAllowed, "Метод не поддерживается", "Этот метод HTTP не поддерживается.")
		return false
	}
	return true
}

func (h *Handler) getCurrentUser(r *http.Request) (*models.User, error) {
	_ = h.Sessions.DeleteExpired()
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return nil, err
	}
	session, err := h.Sessions.GetByID(cookie.Value)
	if err != nil {
		return nil, err
	}
	if session.ExpiresAt.Before(time.Now()) {
		_ = h.Sessions.DeleteByID(session.ID)
		return nil, errors.New("session expired")
	}
	return h.Users.GetByID(session.UserID)
}

func (h *Handler) mustAuth(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil, false
	}
	return user, true
}

// safeRedirect returns the Referer URL only when it points to the same host,
// preventing open-redirect attacks via a forged Referer header.
func safeRedirect(r *http.Request, fallback string) string {
	back := r.Referer()
	if back == "" {
		return fallback
	}
	u, err := url.Parse(back)
	if err != nil || (u.Host != "" && u.Host != r.Host) {
		return fallback
	}
	return back
}

func parseID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func (h *Handler) generateCSRFToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}

func (h *Handler) validateCSRFToken(r *http.Request) bool {
	token := r.FormValue("csrf_token")
	cookie, err := r.Cookie("csrf_token")
	if err != nil || token == "" || cookie.Value != token {
		return false
	}
	return true
}

func (h *Handler) setCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.SessionMaxAge.Seconds()),
	})
}
