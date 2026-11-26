package handlers

import (
	"forum/internal/repository"
	"net/http"
	"strconv"
	"strings"
)

// wantsJSON reports whether the request came from fetch() and expects JSON.
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// ReactPost toggles a like/dislike. Plain form posts are redirected back;
// fetch() requests get {"likes", "dislikes", "mine"} so the page can update
// the counters without reloading.
func (h *Handler) ReactPost(w http.ResponseWriter, r *http.Request) {
	if wantsJSON(r) {
		user, ok := h.jsonAction(w, r, 0)
		if !ok {
			return
		}
		postID, err := parseID(r.FormValue("post_id"))
		value, verr := parseReactionValue(r.FormValue("value"))
		if err != nil || verr != nil || (value != 1 && value != -1) {
			jsonError(w, http.StatusBadRequest, "Неверный запрос")
			return
		}
		if err := h.Reactions.TogglePost(user.ID, postID, value); err != nil {
			jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
			return
		}
		likes, dislikes, mine, err := h.Posts.Reactions(postID, user.ID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
			return
		}
		if mine == 1 {
			h.notifyPostAuthor(postID, repository.NotifyLike, user.ID)
		}
		h.afterChange(h.postAuthorID(postID))
		writeJSON(w, http.StatusOK, map[string]int{"likes": likes, "dislikes": dislikes, "mine": mine})
		return
	}

	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	if !h.validateCSRFToken(r) {
		http.Error(w, "Invalid CSRF token", http.StatusForbidden)
		return
	}

	postID, err := parseID(r.FormValue("post_id"))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	value, err := parseReactionValue(r.FormValue("value"))
	if err != nil || (value != 1 && value != -1) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := h.Reactions.TogglePost(user.ID, postID, value); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if _, _, mine, err := h.Posts.Reactions(postID, user.ID); err == nil && mine == 1 {
		h.notifyPostAuthor(postID, repository.NotifyLike, user.ID)
	}
	h.afterChange(h.postAuthorID(postID))
	http.Redirect(w, r, safeRedirect(r, "/"), http.StatusSeeOther)
}

func (h *Handler) ReactComment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	if !h.validateCSRFToken(r) {
		http.Error(w, "Invalid CSRF token", http.StatusForbidden)
		return
	}

	commentID, err := parseID(r.FormValue("comment_id"))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	value, err := parseReactionValue(r.FormValue("value"))
	if err != nil || (value != 1 && value != -1) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := h.Reactions.ToggleComment(user.ID, commentID, value); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, safeRedirect(r, "/"), http.StatusSeeOther)
}

func parseReactionValue(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}
