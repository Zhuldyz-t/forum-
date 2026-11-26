package handlers

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/repository"
	"forum/internal/validator"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	avatarDir       = "uploads/avatars"
	avatarMaxSize   = 10 << 20   // 10 MB
	avatarMaxPixels = 50_000_000 // guards against decompression bombs
)

var avatarNameRegex = regexp.MustCompile(`^[a-f0-9]{32}\.(jpg|png|gif|webp)$`)

const bioMaxLen = 200 // characters

// Tabs on the profile page; the first one is the default.
var profileTabs = []string{"posts", "comments", "liked", "saved"}

// Profile shows the viewer's own profile, or another user's public profile
// with ?id=N (also for guests). Private bits — email, liked posts, likes in
// the activity feed, editing — are only shown to the owner.
func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	viewer, _ := h.getCurrentUser(r)

	// Old links: /profile?id=N → /user/N.
	if idStr := r.URL.Query().Get("id"); idStr != "" {
		q := r.URL.Query()
		q.Del("id")
		target := "/user/" + url.PathEscape(idStr)
		if len(q) > 0 {
			target += "?" + q.Encode()
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	profile := viewer
	if profile == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	success := ""
	if r.URL.Query().Get("updated") == "1" {
		success = "Профиль обновлён"
	}
	h.renderProfile(w, r, "profile.html", viewer, profile, "", success, http.StatusOK)
}

// renderProfilePage renders the owner's own profile or edit page.
func (h *Handler) renderProfilePage(w http.ResponseWriter, r *http.Request, page string, user *models.User, errMsg, success string, status int) {
	h.renderProfile(w, r, page, user, user, errMsg, success, status)
}

func (h *Handler) renderProfile(w http.ResponseWriter, r *http.Request, page string, viewer, user *models.User, errMsg, success string, status int) {
	token := h.generateCSRFToken()
	h.setCSRFCookie(w, token)

	data := models.ProfilePageData{
		CurrentUser: viewer,
		Profile:     user,
		IsOwner:     viewer != nil && viewer.ID == user.ID,
		CSRFToken:   token,
		Error:       errMsg,
		Success:     success,
	}
	viewerID := 0
	if viewer != nil {
		viewerID = viewer.ID
	}
	var err error
	if data.Photos, err = h.Photos.ListByUser(user.ID); err != nil {
		logger.Error("profile: list photos", err)
	}

	if page == "profile.html" {
		if err := h.DB.QueryRow(`SELECT COUNT(*) FROM posts WHERE user_id = ?`, user.ID).Scan(&data.PostCount); err != nil {
			logger.Error("profile: count posts", err)
		}
		if err := h.DB.QueryRow(`SELECT COUNT(*) FROM comments WHERE user_id = ? AND is_deleted = 0`, user.ID).Scan(&data.CommentCount); err != nil {
			logger.Error("profile: count comments", err)
		}
		if data.LikesReceived, err = h.Users.LikesReceived(user.ID); err != nil {
			logger.Error("profile: likes received", err)
		}

		data.Tab = profileTabs[0]
		if t := r.URL.Query().Get("tab"); slices.Contains(profileTabs, t) && ((t != "liked" && t != "saved") || data.IsOwner) {
			data.Tab = t
		}
		// All tabs are rendered up front so switching doesn't reload the page.
		list := func(filter string) ([]models.PostSummary, bool, error) {
			return h.Posts.Feed(repository.FeedOptions{Filter: filter, UserID: user.ID, ViewerID: viewerID, Limit: 20})
		}
		if data.Posts, data.MorePosts, err = list("my_posts"); err != nil {
			logger.Error("profile: list posts", err)
		}
		if data.Comments, err = h.Comments.ListByUser(user.ID, 20); err != nil {
			logger.Error("profile: list comments", err)
		}
		if data.IsOwner {
			if data.LikedPosts, data.MoreLiked, err = list("liked"); err != nil {
				logger.Error("profile: list liked", err)
			}
			if data.SavedPosts, data.MoreSaved, err = list("bookmarks"); err != nil {
				logger.Error("profile: list saved", err)
			}
		}
		if data.TopCategories, err = h.Posts.TopCategories(user.ID, 3); err != nil {
			logger.Error("profile: top categories", err)
		}
		if data.Activity, err = h.Users.RecentActivity(user.ID, 3, data.IsOwner); err != nil {
			logger.Error("profile: activity", err)
		}
		if data.IsOwner && !user.IsModerator() {
			if data.ModApplied, err = h.Moderation.PendingApplication(user.ID); err != nil {
				logger.Error("profile: application", err)
			}
		}
		if data.Achievements, data.EarnedCount, err = h.Achievements.ForUser(user.ID); err != nil {
			logger.Error("profile: achievements", err)
		}
		if data.Followers, data.Following, err = h.Follows.Counts(user.ID); err != nil {
			logger.Error("profile: follow counts", err)
		}
		if viewer != nil && !data.IsOwner {
			set, err := h.Follows.FollowingSet(viewer.ID)
			if err != nil {
				logger.Error("profile: following set", err)
			}
			data.IsFollowing = set[user.ID]
		}
	}

	h.render(w, page, data, status)
}

// EditProfile shows the edit page (GET) and saves the username and bio
// (POST). Photos are uploaded separately via UploadPhoto.
func (h *Handler) EditProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		success := ""
		switch r.URL.Query().Get("msg") {
		case "current":
			success = "Основное фото изменено"
		case "deleted":
			success = "Фото удалено"
		}
		h.renderProfilePage(w, r, "profile_edit.html", user, "", success, http.StatusOK)
		return
	case http.MethodPost:
	default:
		h.methodRequired(w, r, http.MethodPost)
		return
	}

	fail := func(msg string, status int) {
		h.renderProfilePage(w, r, "profile_edit.html", user, msg, "", status)
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		fail("Слишком большой запрос", http.StatusBadRequest)
		return
	}
	if !h.validateCSRFToken(r) {
		fail("Сессия устарела, попробуй снова", http.StatusOK)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	bio := strings.TrimSpace(strings.ReplaceAll(r.FormValue("bio"), "\r\n", "\n"))

	// Show what the user typed if validation fails.
	shown := *user
	shown.Username, shown.Bio = username, bio
	failWith := func(msg string) {
		h.renderProfilePage(w, r, "profile_edit.html", &shown, msg, "", http.StatusBadRequest)
	}

	if utf8.RuneCountInString(bio) > bioMaxLen {
		failWith("«О себе»: не больше 200 символов")
		return
	}
	if username != user.Username {
		if !validator.ValidateUsername(username) {
			failWith("Имя пользователя: 3–20 символов, только буквы, цифры и _")
			return
		}
		exists, err := h.Users.UsernameExists(username)
		if err != nil {
			logger.Error("UsernameExists", err)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
		if exists {
			failWith("Это имя пользователя уже занято")
			return
		}
		if err := h.Users.UpdateUsername(user.ID, username); err != nil {
			logger.Error("UpdateUsername", err)
			failWith("Не удалось сохранить имя, попробуй другое")
			return
		}
	}
	if bio != user.Bio {
		if err := h.Users.UpdateBio(user.ID, bio); err != nil {
			logger.Error("UpdateBio", err)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
	}

	http.Redirect(w, r, "/profile?updated=1", http.StatusSeeOther)
}

type uploadResponse struct {
	Error   string `json:"error,omitempty"`
	Avatar  string `json:"avatar,omitempty"`  // URL of the new current photo
	Gallery string `json:"gallery,omitempty"` // re-rendered "Мои фото" grid
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// jsonAction authenticates a fetch() POST from the profile pages: it needs a
// session, POST, and the CSRF token as a form field. maxBody > 0 means a
// multipart upload of up to maxBody bytes. On failure it writes a JSON error.
func (h *Handler) jsonAction(w http.ResponseWriter, r *http.Request, maxBody int64) (*models.User, bool) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return nil, false
	}
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return nil, false
	}
	if maxBody > 0 {
		// Leave headroom over the file limit for the other form fields.
		r.Body = http.MaxBytesReader(w, r.Body, maxBody+(512<<10))
		if err := r.ParseMultipartForm(maxBody); err != nil {
			jsonError(w, http.StatusBadRequest, "Файл слишком большой (максимум 10 МБ)")
			return nil, false
		}
	} else {
		// Small body; fetch() sends FormData as multipart, forms as urlencoded.
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := r.ParseMultipartForm(8 << 10); err != nil && !errors.Is(err, http.ErrNotMultipart) {
			jsonError(w, http.StatusBadRequest, "Неверный запрос")
			return nil, false
		}
	}
	if !h.validateCSRFToken(r) {
		jsonError(w, http.StatusForbidden, "Сессия устарела, обновите страницу")
		return nil, false
	}
	return user, true
}

// UploadPhoto receives a photo from the edit page via fetch, adds it to the
// gallery as current and answers with JSON.
func (h *Handler) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, avatarMaxSize)
	if !ok {
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	name, ok := h.receiveImage(w, r, "avatar", avatarImages)
	if !ok {
		return
	}
	if err := h.Photos.Add(user.ID, name); err != nil {
		logger.Error("Photos.Add", err)
		removeAvatarFile(name)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}

	photos, err := h.Photos.ListByUser(user.ID)
	if err != nil {
		logger.Error("profile: list photos", err)
	}
	var gallery bytes.Buffer
	data := models.ProfilePageData{CurrentUser: user, Photos: photos, CSRFToken: r.FormValue("csrf_token")}
	if err := h.Tmpl.ExecuteTemplate(&gallery, "photo-gallery", data); err != nil {
		logger.Error("profile: render gallery", err)
	}
	writeJSON(w, http.StatusOK, uploadResponse{Avatar: "/uploads/avatars/" + name, Gallery: gallery.String()})
}

// receiveImage stores the image from form field `field`. On failure it
// writes a JSON error and returns ok=false.
func (h *Handler) receiveImage(w http.ResponseWriter, r *http.Request, field string, kind imageKind) (string, bool) {
	file, _, err := r.FormFile(field)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Выберите файл")
		return "", false
	}
	defer file.Close()

	name, msg := saveImage(file, kind)
	if msg == "" && name == "" {
		msg = "Файл пустой"
	}
	if msg != "" {
		jsonError(w, http.StatusBadRequest, msg)
		return "", false
	}
	return name, true
}

// photoAction parses a POST with csrf_token and photo_id for the gallery actions.
func (h *Handler) photoAction(w http.ResponseWriter, r *http.Request) (*models.User, int, bool) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return nil, 0, false
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return nil, 0, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil || !h.validateCSRFToken(r) {
		h.renderProfilePage(w, r, "profile_edit.html", user, "Сессия устарела, попробуй снова", "", http.StatusBadRequest)
		return nil, 0, false
	}
	id, err := parseID(r.FormValue("photo_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Некорректный идентификатор фото.")
		return nil, 0, false
	}
	return user, id, true
}

func (h *Handler) SetCurrentPhoto(w http.ResponseWriter, r *http.Request) {
	user, id, ok := h.photoAction(w, r)
	if !ok {
		return
	}
	if err := h.Photos.SetCurrent(user.ID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.NotFound(w, r)
			return
		}
		logger.Error("Photos.SetCurrent", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	http.Redirect(w, r, "/profile/edit?msg=current", http.StatusSeeOther)
}

func (h *Handler) DeletePhoto(w http.ResponseWriter, r *http.Request) {
	user, id, ok := h.photoAction(w, r)
	if !ok {
		return
	}
	name, err := h.Photos.Delete(user.ID, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.NotFound(w, r)
			return
		}
		logger.Error("Photos.Delete", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	removeAvatarFile(name)
	http.Redirect(w, r, "/profile/edit?msg=deleted", http.StatusSeeOther)
}

// imageKind describes one kind of uploaded image and where it is stored.
type imageKind struct {
	dir      string
	types    map[string]bool // allowed content types, detected from the bytes
	typesMsg string          // error shown for any other type
	maxSide  int             // longest side after downscaling
	name     *regexp.Regexp  // stored file names; anything else is never served or deleted
}

var avatarImages = imageKind{
	dir:      avatarDir,
	types:    map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true},
	typesMsg: "Можно загрузить только JPG, PNG, GIF или WEBP",
	maxSide:  1280,
	name:     avatarNameRegex,
}

// saveImage validates an uploaded image, re-encodes it and writes it to
// kind.dir. It returns the stored file name, or a user-facing error message.
// Both are empty for an empty upload.
func saveImage(file io.Reader, kind imageKind) (string, string) {
	data, err := io.ReadAll(io.LimitReader(file, avatarMaxSize+1))
	if err != nil {
		return "", "Не удалось загрузить файл"
	}
	if len(data) == 0 {
		return "", ""
	}
	if len(data) > avatarMaxSize {
		return "", "Файл слишком большой (максимум 10 МБ)"
	}
	if !kind.types[http.DetectContentType(data)] {
		return "", kind.typesMsg
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", "Не удалось прочитать изображение"
	}
	if cfg.Width*cfg.Height > avatarMaxPixels {
		return "", "Изображение слишком большое по разрешению"
	}
	data, err = processImage(data, kind.maxSide)
	if err != nil {
		logger.Error("image: process", err)
		return "", "Не удалось обработать изображение"
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		logger.Error("image: rand", err)
		return "", "Не удалось загрузить файл"
	}
	name := hex.EncodeToString(b) + ".jpg"

	if err := os.MkdirAll(kind.dir, 0o755); err != nil {
		logger.Error("image: mkdir", err)
		return "", "Не удалось загрузить файл"
	}
	if err := os.WriteFile(filepath.Join(kind.dir, name), data, 0o644); err != nil {
		logger.Error("image: write", err)
		return "", "Не удалось загрузить файл"
	}
	return name, ""
}

func removeImageFile(kind imageKind, name string) {
	if !kind.name.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(kind.dir, name)); err != nil && !os.IsNotExist(err) {
		logger.Error("image: remove", err)
	}
}

func removeAvatarFile(name string) { removeImageFile(avatarImages, name) }

// serveImage serves a stored image by its generated name only, so the
// handler can't be used to read other files or list the directory.
func (h *Handler) serveImage(w http.ResponseWriter, r *http.Request, kind imageKind, prefix string) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, prefix)
	if !kind.name.MatchString(name) {
		h.NotFound(w, r)
		return
	}
	path := filepath.Join(kind.dir, name)
	if _, err := os.Stat(path); err != nil {
		h.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}

// Avatar serves uploaded avatar images.
func (h *Handler) Avatar(w http.ResponseWriter, r *http.Request) {
	h.serveImage(w, r, avatarImages, "/uploads/avatars/")
}
