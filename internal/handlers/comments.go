package handlers

import (
	"forum/internal/logger"
	"forum/internal/validator"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) CreateComment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	if !h.validateCSRFToken(r) {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Неверный токен безопасности. Обнови страницу и попробуй снова.")
		return
	}

	postID, err := parseID(r.FormValue("post_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор поста.")
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	if !validator.ValidateContent(content, 10000) {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Комментарий должен быть от 1 до 10000 символов.")
		return
	}
	if err := h.Comments.Create(postID, user.ID, content); err != nil {
		logger.Error("Comments.Create", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	http.Redirect(w, r, "/post?id="+r.FormValue("post_id"), http.StatusSeeOther)
}

func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	if !h.validateCSRFToken(r) {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Неверный токен безопасности. Обнови страницу и попробуй снова.")
		return
	}

	commentID, err := parseID(r.FormValue("comment_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор комментария.")
		return
	}
	postID, err := parseID(r.FormValue("post_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор поста.")
		return
	}

	deleted, err := h.Comments.DeleteByAuthor(commentID, user.ID)
	if err != nil {
		logger.Error("Comments.DeleteByAuthor", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if !deleted {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Ты не можешь удалить этот комментарий.")
		return
	}

	http.Redirect(w, r, "/post?id="+strconv.Itoa(postID), http.StatusSeeOther)
}
