package handlers

import (
	"database/sql"
	"forum/internal/models"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		user, _ := h.getCurrentUser(r)
		h.render(w, "register.html", models.AuthPageData{CurrentUser: user}, http.StatusOK)
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	data := models.AuthPageData{Error: "All fields are required"}

	if email == "" || username == "" || password == "" {
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}

	exists, err := h.Users.EmailExists(email)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if exists {
		data.Error = "Email is already taken"
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}

	exists, err = h.Users.UsernameExists(username)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if exists {
		data.Error = "Username is already taken"
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := h.Users.Create(email, username, string(hashed)); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		user, _ := h.getCurrentUser(r)
		h.render(w, "login.html", models.AuthPageData{CurrentUser: user}, http.StatusOK)
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := strings.TrimSpace(r.FormValue("password"))
	data := models.AuthPageData{Error: "Invalid credentials"}

	if email == "" || password == "" {
		data.Error = "Email and password are required"
		h.render(w, "login.html", data, http.StatusBadRequest)
		return
	}

	user, err := h.Users.GetByEmail(email)
	if err != nil {
		if err == sql.ErrNoRows {
			h.render(w, "login.html", data, http.StatusUnauthorized)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		h.render(w, "login.html", data, http.StatusUnauthorized)
		return
	}

	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(h.SessionMaxAge)
	if err := h.Sessions.Replace(sessionID, user.ID, expiresAt); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Expires:  expiresAt,
		MaxAge:   int(h.SessionMaxAge.Seconds()),
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}
	cookie, err := r.Cookie("session_id")
	if err == nil {
		_ = h.Sessions.DeleteByID(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
