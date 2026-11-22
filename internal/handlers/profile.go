package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/validator"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	avatarDir     = "uploads/avatars"
	avatarMaxSize = 2 << 20 // 2 MB
)

// Allowed avatar types, detected from file content (not from the client's filename).
var avatarExts = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var avatarNameRegex = regexp.MustCompile(`^[a-f0-9]{32}\.(jpg|png|gif|webp)$`)

func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}

	success := ""
	if r.URL.Query().Get("updated") == "1" {
		success = "Профиль обновлён"
	}
	h.renderProfile(w, user, "", success, http.StatusOK)
}

func (h *Handler) renderProfile(w http.ResponseWriter, user *models.User, errMsg, success string, status int) {
	var postCount, commentCount int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM posts WHERE user_id = ?`, user.ID).Scan(&postCount); err != nil {
		logger.Error("profile: count posts", err)
	}
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM comments WHERE user_id = ?`, user.ID).Scan(&commentCount); err != nil {
		logger.Error("profile: count comments", err)
	}

	token := h.generateCSRFToken()
	h.setCSRFCookie(w, token)

	data := models.ProfilePageData{
		CurrentUser:  user,
		PostCount:    postCount,
		CommentCount: commentCount,
		CSRFToken:    token,
		Error:        errMsg,
		Success:      success,
	}
	h.render(w, "profile.html", data, status)
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	// Leave headroom over avatarMaxSize for the other form fields.
	r.Body = http.MaxBytesReader(w, r.Body, avatarMaxSize+(512<<10))
	if err := r.ParseMultipartForm(avatarMaxSize); err != nil {
		h.renderProfile(w, user, "Файл слишком большой (максимум 2 МБ)", "", http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	if !h.validateCSRFToken(r) {
		h.renderProfile(w, user, "Сессия устарела, попробуй снова", "", http.StatusOK)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	if username != user.Username {
		if !validator.ValidateUsername(username) {
			h.renderProfile(w, user, "Имя пользователя: 3–20 символов, только буквы, цифры и _", "", http.StatusBadRequest)
			return
		}
		exists, err := h.Users.UsernameExists(username)
		if err != nil {
			logger.Error("UsernameExists", err)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
		if exists {
			h.renderProfile(w, user, "Это имя пользователя уже занято", "", http.StatusBadRequest)
			return
		}
		if err := h.Users.UpdateUsername(user.ID, username); err != nil {
			logger.Error("UpdateUsername", err)
			h.renderProfile(w, user, "Не удалось сохранить имя, попробуй другое", "", http.StatusBadRequest)
			return
		}
		user.Username = username
	}

	if r.FormValue("remove_avatar") == "1" && user.Avatar != "" {
		if err := h.Users.UpdateAvatar(user.ID, ""); err != nil {
			logger.Error("UpdateAvatar", err)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
		removeAvatarFile(user.Avatar)
		user.Avatar = ""
	}

	file, _, err := r.FormFile("avatar")
	if err == nil {
		defer file.Close()
		name, msg := saveAvatar(file)
		if msg != "" {
			h.renderProfile(w, user, msg, "", http.StatusBadRequest)
			return
		}
		if err := h.Users.UpdateAvatar(user.ID, name); err != nil {
			logger.Error("UpdateAvatar", err)
			removeAvatarFile(name)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
		removeAvatarFile(user.Avatar)
	} else if !errors.Is(err, http.ErrMissingFile) {
		h.renderProfile(w, user, "Не удалось загрузить файл", "", http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/profile?updated=1", http.StatusSeeOther)
}

// saveAvatar validates the uploaded image and writes it to avatarDir.
// It returns the stored filename, or a user-facing error message.
func saveAvatar(file io.Reader) (string, string) {
	data, err := io.ReadAll(io.LimitReader(file, avatarMaxSize+1))
	if err != nil {
		return "", "Не удалось загрузить файл"
	}
	if len(data) == 0 {
		return "", ""
	}
	if len(data) > avatarMaxSize {
		return "", "Файл слишком большой (максимум 2 МБ)"
	}
	ext, ok := avatarExts[http.DetectContentType(data)]
	if !ok {
		return "", "Можно загрузить только JPG, PNG, GIF или WEBP"
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		logger.Error("avatar: rand", err)
		return "", "Не удалось загрузить файл"
	}
	name := hex.EncodeToString(b) + ext

	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		logger.Error("avatar: mkdir", err)
		return "", "Не удалось загрузить файл"
	}
	if err := os.WriteFile(filepath.Join(avatarDir, name), data, 0o644); err != nil {
		logger.Error("avatar: write", err)
		return "", "Не удалось загрузить файл"
	}
	return name, ""
}

func removeAvatarFile(name string) {
	if !avatarNameRegex.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(avatarDir, name)); err != nil && !os.IsNotExist(err) {
		logger.Error("avatar: remove", err)
	}
}

// Avatar serves uploaded avatar images. Only generated filenames are accepted,
// so the handler can't be used to read other files or list the directory.
func (h *Handler) Avatar(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/uploads/avatars/")
	if !avatarNameRegex.MatchString(name) {
		h.NotFound(w, r)
		return
	}
	path := filepath.Join(avatarDir, name)
	if _, err := os.Stat(path); err != nil {
		h.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}
