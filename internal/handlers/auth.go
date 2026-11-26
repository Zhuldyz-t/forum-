package handlers

import (
	"database/sql"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/validator"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		user, _ := h.getCurrentUser(r)
		token := h.generateCSRFToken()
		h.setCSRFCookie(w, token)
		h.render(w, "register.html", models.AuthPageData{CurrentUser: user, CSRFToken: token}, http.StatusOK)
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	// CSRF failure: re-render form with fresh token instead of blank error page
	if !h.validateCSRFToken(r) {
		token := h.generateCSRFToken()
		h.setCSRFCookie(w, token)
		h.render(w, "register.html", models.AuthPageData{
			Error:     "Сессия устарела, попробуй снова",
			CSRFToken: token,
		}, http.StatusOK)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))
	data := models.AuthPageData{Error: "Все поля обязательны для заполнения"}

	if email == "" || username == "" || password == "" {
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}

	if !validator.ValidateEmail(email) {
		data.Error = "Неверный формат email"
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}
	if !validator.ValidateUsername(username) {
		data.Error = "Имя пользователя: 3–20 символов, только буквы, цифры и _"
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}
	if !validator.ValidatePassword(password) {
		data.Error = "Пароль должен быть не менее 6 символов"
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}

	exists, err := h.Users.EmailExists(email)
	if err != nil {
		logger.Error("EmailExists", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if exists {
		data.Error = "Этот email уже занят"
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}

	exists, err = h.Users.UsernameExists(username)
	if err != nil {
		logger.Error("UsernameExists", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if exists {
		data.Error = "Это имя пользователя уже занято"
		h.render(w, "register.html", data, http.StatusBadRequest)
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("bcrypt.GenerateFromPassword", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	if err := h.Users.Create(email, username, string(hashed)); err != nil {
		logger.Error("User.Create", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		user, _ := h.getCurrentUser(r)
		token := h.generateCSRFToken()
		h.setCSRFCookie(w, token)
		h.render(w, "login.html", models.AuthPageData{CurrentUser: user, CSRFToken: token}, http.StatusOK)
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	// CSRF failure: re-render form with fresh token instead of blank error page
	if !h.validateCSRFToken(r) {
		token := h.generateCSRFToken()
		h.setCSRFCookie(w, token)
		h.render(w, "login.html", models.AuthPageData{
			Error:     "Сессия устарела, попробуй снова",
			CSRFToken: token,
		}, http.StatusOK)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := strings.TrimSpace(r.FormValue("password"))
	data := models.AuthPageData{Error: "Неверный email или пароль"}

	if email == "" || password == "" {
		data.Error = "Введи email и пароль"
		h.render(w, "login.html", data, http.StatusBadRequest)
		return
	}

	if !validator.ValidateEmail(email) {
		data.Error = "Неверный формат email"
		h.render(w, "login.html", data, http.StatusBadRequest)
		return
	}

	user, err := h.Users.GetByEmail(email)
	if err != nil {
		if err == sql.ErrNoRows {
			h.render(w, "login.html", data, http.StatusUnauthorized)
			return
		}
		logger.Error("GetByEmail", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		h.render(w, "login.html", data, http.StatusUnauthorized)
		return
	}
	if user.IsBanned() {
		h.renderBanned(w, user)
		return
	}
	if !h.startSession(w, user.ID) {
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// renderBanned shows a blocked user why and until when.
func (h *Handler) renderBanned(w http.ResponseWriter, user *models.User) {
	token := h.generateCSRFToken()
	h.setCSRFCookie(w, token)
	h.render(w, "login.html", models.AuthPageData{Error: user.BanText(), CSRFToken: token}, http.StatusForbidden)
}

// startSession creates a session (one per user) and sets its cookie. On
// failure it renders an error page and returns false.
func (h *Handler) startSession(w http.ResponseWriter, userID int) bool {
	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(h.SessionMaxAge)
	if err := h.Sessions.Replace(sessionID, userID, expiresAt); err != nil {
		logger.Error("Session.Replace", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(h.SessionMaxAge.Seconds()),
	})
	return true
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}
	if !h.validateCSRFToken(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
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
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
