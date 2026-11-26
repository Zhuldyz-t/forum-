package handlers

import (
	"forum/internal/logger"
	"forum/internal/models"
	"net/http"
	"regexp"
)

var coverImages = imageKind{
	dir:      "uploads/covers",
	types:    map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true},
	typesMsg: "Можно загрузить только JPG, PNG или WEBP",
	maxSide:  2000,
	name:     regexp.MustCompile(`^[a-f0-9]{32}\.jpg$`),
}

type coverResponse struct {
	Image  string `json:"image"`  // URL of the uploaded cover, empty for a gradient
	Preset string `json:"preset"` // gradient name
}

// CoverFile serves uploaded cover images.
func (h *Handler) CoverFile(w http.ResponseWriter, r *http.Request) {
	h.serveImage(w, r, coverImages, "/uploads/covers/")
}

// setCover saves the user's cover, removes the previous uploaded file if it
// changed, and answers with the new state.
func (h *Handler) setCover(w http.ResponseWriter, user *models.User, image, preset string) {
	if err := h.Users.UpdateCover(user.ID, image, preset); err != nil {
		logger.Error("UpdateCover", err)
		removeImageFile(coverImages, image)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	if user.CoverImage != image {
		removeImageFile(coverImages, user.CoverImage)
	}
	resp := coverResponse{Preset: preset}
	if image != "" {
		resp.Image = "/uploads/covers/" + image
	}
	writeJSON(w, http.StatusOK, resp)
}

// UploadCover stores an uploaded cover image (fetch POST, JSON reply).
func (h *Handler) UploadCover(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, avatarMaxSize)
	if !ok {
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	name, ok := h.receiveImage(w, r, "cover", coverImages)
	if !ok {
		return
	}
	h.setCover(w, user, name, user.CoverPreset)
}

// SetCoverPreset switches the cover to a built-in gradient, dropping any
// uploaded image.
func (h *Handler) SetCoverPreset(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	preset := r.FormValue("preset")
	if !models.IsCoverPreset(preset) {
		jsonError(w, http.StatusBadRequest, "Неизвестная обложка")
		return
	}
	h.setCover(w, user, "", preset)
}

// DeleteCover resets the cover to the default gradient.
func (h *Handler) DeleteCover(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	h.setCover(w, user, "", models.DefaultCoverPreset)
}
