package handlers

import (
	"database/sql"
	"errors"
	"forum/internal/models"
	"forum/internal/repository"
	"html/template"
	"net/http"
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
}

func NewHandler(db *sql.DB, tmpl *template.Template) *Handler {
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
	}
}

func (h *Handler) render(w http.ResponseWriter, name string, data any, status int) {
	w.WriteHeader(status)
	if err := h.Tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) methodRequired(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	return user, true
}

func parseID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
