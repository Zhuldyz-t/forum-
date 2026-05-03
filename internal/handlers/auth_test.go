package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func setupAuthHandler() (*Handler, *httptest.ResponseRecorder) {
	db := setupTestDB()
	tmpl := setupTestTemplate()
	h := NewHandler(db, tmpl)
	return h, httptest.NewRecorder()
}

func TestRegister_PasswordTooShort(t *testing.T) {
	h, w := setupAuthHandler()

	form := url.Values{}
	form.Set("email", "test@example.com")
	form.Set("username", "testuser")
	form.Set("password", "123")
	form.Set("csrf_token", "test")

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})

	h.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Пароль должен быть не менее 6 символов") {
		t.Errorf("Expected Russian password length error, got body: %s", body)
	}
}

func TestRegister_MissingFields(t *testing.T) {
	h, w := setupAuthHandler()

	form := url.Values{}
	form.Set("email", "")
	form.Set("username", "testuser")
	form.Set("password", "password123")
	form.Set("csrf_token", "test")

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})

	h.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	h, w := setupAuthHandler()

	form := url.Values{}
	form.Set("email", "test@example.com")
	form.Set("password", "")
	form.Set("csrf_token", "test")

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "test"})

	h.Login(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCSRFValidation(t *testing.T) {
	h, w := setupAuthHandler()

	form := url.Values{}
	form.Set("email", "test@example.com")
	form.Set("password", "password123")
	form.Set("csrf_token", "wrong_token")

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: "different_token"})

	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for CSRF re-render, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Сессия устарела") {
		t.Errorf("Expected CSRF session error message, got body: %s", body)
	}
}
