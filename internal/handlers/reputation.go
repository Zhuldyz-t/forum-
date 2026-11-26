package handlers

import (
	"database/sql"
	"errors"
	"forum/internal/logger"
	"forum/internal/repository"
	"net/http"
	"strconv"
)

// afterChange runs after anything that can change reputation or earn an
// achievement: it recomputes the users' reputation, awards achievements, and
// tells their open tabs to show the «Новое достижение» toast.
func (h *Handler) afterChange(userIDs ...int) {
	seen := map[int]bool{}
	for _, id := range userIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if err := h.Users.RecalcReputation(id); err != nil {
			logger.Error("reputation", err)
		}
		fresh, err := h.Achievements.Check(id)
		if err != nil {
			logger.Error("achievements", err)
		}
		if len(fresh) > 0 && h.Hub != nil {
			h.Hub.Send(id, map[string]any{"type": "achievement"})
		}
	}
}

// postAuthorID returns the author of a post (0 if it doesn't exist).
func (h *Handler) postAuthorID(postID int) int {
	var id int
	if err := h.DB.QueryRow(`SELECT user_id FROM posts WHERE id = ?`, postID).Scan(&id); err != nil && !errors.Is(err, sql.ErrNoRows) {
		logger.Error("post author", err)
	}
	return id
}

// usersTouchedByPost lists everyone whose score depends on the post: the
// author and the authors of its comments. Collected before deleting it.
func (h *Handler) usersTouchedByPost(postID int) []int {
	ids := []int{h.postAuthorID(postID)}
	rows, err := h.DB.Query(`SELECT DISTINCT user_id FROM comments WHERE post_id = ?`, postID)
	if err != nil {
		logger.Error("post users", err)
		return ids
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// AchievementToasts returns achievements whose toast is still pending and
// marks them shown: GET /achievements/toasts.
func (h *Handler) AchievementToasts(w http.ResponseWriter, r *http.Request) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	list, err := h.Achievements.TakeToasts(user.ID)
	if err != nil {
		logger.Error("achievement toasts", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	type item struct {
		Code  string `json:"code"`
		Title string `json:"title"`
	}
	out := make([]item, 0, len(list))
	for _, a := range list {
		out = append(out, item{a.Code, a.Title})
	}
	writeJSON(w, http.StatusOK, map[string]any{"achievements": out})
}

// ToggleBestAnswer lets the author of a «Вопросы» post mark one comment as
// the best answer, or unmark it: POST /comment/best (comment_id).
func (h *Handler) ToggleBestAnswer(w http.ResponseWriter, r *http.Request) {
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
	id, err := parseID(r.FormValue("comment_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор комментария.")
		return
	}
	ref, err := h.Comments.Ref(id)
	if err != nil || ref.IsDeleted {
		h.renderError(w, http.StatusNotFound, "Комментарий не найден", "Комментарий удалён или не существует.")
		return
	}
	best, author, question, err := h.Posts.BestAnswerInfo(ref.PostID)
	if err != nil {
		logger.Error("best answer info", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	switch {
	case author != user.ID:
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Лучший ответ выбирает только автор поста.")
		return
	case !question:
		h.renderError(w, http.StatusBadRequest, "Нельзя", "Лучший ответ есть только у постов из категории «Вопросы».")
		return
	case ref.UserID == user.ID:
		h.renderError(w, http.StatusBadRequest, "Нельзя", "Свой комментарий нельзя отметить лучшим ответом.")
		return
	}

	newBest := id
	if best == id {
		newBest = 0 // clicking again removes the mark
	}
	if err := h.Posts.SetBestComment(ref.PostID, newBest); err != nil {
		logger.Error("set best answer", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	previousAuthor := 0
	if best > 0 && best != id {
		if prev, err := h.Comments.Ref(best); err == nil {
			previousAuthor = prev.UserID
		}
	}
	if newBest > 0 {
		if err := h.Notifications.NotifyComment(ref.UserID, repository.NotifyBest, user.ID, ref.PostID, id); err != nil {
			logger.Error("notify best", err)
		}
	}
	h.afterChange(ref.UserID, previousAuthor)
	http.Redirect(w, r, "/post?id="+strconv.Itoa(ref.PostID)+"#comment-"+strconv.Itoa(id), http.StatusSeeOther)
}
