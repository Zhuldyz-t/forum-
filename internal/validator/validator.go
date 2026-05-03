package validator

import (
	"net/mail"
	"regexp"
	"strings"
)

var (
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)
	titleRegex    = regexp.MustCompile(`^[\s\S]{1,200}$`)
)

// ValidateEmail проверяет формат email
func ValidateEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	_, err := mail.ParseAddress(email)
	return err == nil
}

// ValidateUsername проверяет имя пользователя
func ValidateUsername(username string) bool {
	username = strings.TrimSpace(username)
	return usernameRegex.MatchString(username) && len(username) >= 3
}

// ValidatePassword проверяет пароль
func ValidatePassword(password string) bool {
	return len(password) >= 6
}

// ValidateTitle проверяет заголовок поста
func ValidateTitle(title string) bool {
	title = strings.TrimSpace(title)
	return titleRegex.MatchString(title) && len(title) > 0
}

// ValidateContent проверяет контент поста/комментария
func ValidateContent(content string, maxLen int) bool {
	content = strings.TrimSpace(content)
	return len(content) > 0 && len(content) <= maxLen
}
