package handlers

import (
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) ReactPost(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
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
	back := r.Referer()
	if back == "" {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) ReactComment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
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
	back := r.Referer()
	if back == "" {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func parseReactionValue(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(s))
}
