package handlers

import (
	"fmt"
	"forum/internal/logger"
	"forum/internal/repository"
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

	// Replies: the parent must be a live comment of the same post. Deeper
	// than MaxCommentDepth, the reply attaches to the parent's parent.
	parentID, replyTo := 0, repository.CommentRef{}
	if raw := r.FormValue("parent_id"); raw != "" && raw != "0" {
		id, err := parseID(raw)
		if err == nil {
			replyTo, err = h.Comments.Ref(id)
		}
		if err != nil || replyTo.PostID != postID || replyTo.IsDeleted {
			h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Комментарий, на который ты отвечаешь, не найден.")
			return
		}
		parentID = replyTo.ID
		if replyTo.Depth >= repository.MaxCommentDepth {
			parentID = replyTo.ParentID
		}
	}

	commentID, err := h.Comments.Create(postID, user.ID, parentID, content)
	if err != nil {
		logger.Error("Comments.Create", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	// The replied-to author hears about the reply; the post author about a
	// new comment (unless that's the same person).
	postAuthor := 0
	if err := h.DB.QueryRow(`SELECT user_id FROM posts WHERE id = ?`, postID).Scan(&postAuthor); err != nil {
		logger.Error("comment: post author", err)
	}
	if replyTo.ID != 0 {
		if err := h.Notifications.NotifyComment(replyTo.UserID, repository.NotifyReply, user.ID, postID, commentID); err != nil {
			logger.Error("notify reply", err)
		}
	}
	if postAuthor != replyTo.UserID {
		if err := h.Notifications.NotifyComment(postAuthor, repository.NotifyComment, user.ID, postID, commentID); err != nil {
			logger.Error("notify comment", err)
		}
	}
	h.notifyMentions(content, "", user.ID, postID, commentID)
	h.afterChange(user.ID)

	http.Redirect(w, r, h.commentURL(postID, commentID), http.StatusSeeOther)
}

// commentURL links to a comment on the page of the post where it is shown.
func (h *Handler) commentURL(postID, commentID int) string {
	return fmt.Sprintf("/post?id=%d&comment=%d#comment-%d", postID, commentID, commentID)
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

	h.afterChange(user.ID)
	http.Redirect(w, r, "/post?id="+strconv.Itoa(postID)+"#comments", http.StatusSeeOther)
}
