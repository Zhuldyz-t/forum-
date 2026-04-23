package handlers

import (
	"net/http"
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
	postID, err := parseID(r.FormValue("post_id"))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Redirect(w, r, "/post?id="+r.FormValue("post_id"), http.StatusSeeOther)
		return
	}
	if err := h.Comments.Create(postID, user.ID, content); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/post?id="+r.FormValue("post_id"), http.StatusSeeOther)
}
