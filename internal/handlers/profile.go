package handlers

import (
	"forum/internal/logger"
	"forum/internal/models"
	"net/http"
)

func (h *Handler) Profile(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}

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
	}
	h.render(w, "profile.html", data, http.StatusOK)
}
