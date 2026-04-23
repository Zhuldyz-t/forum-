package handlers

import (
	"database/sql"
	"forum/internal/models"
	"net/http"
	"strconv"
	"strings"
)

func (h *Handler) ViewPost(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	user, _ := h.getCurrentUser(r)
	postID, err := parseID(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	post, err := h.Posts.Get(postID)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Post not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	comments, err := h.Comments.ListByPost(postID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	data := models.PostPageData{CurrentUser: user, Post: post, Comments: comments}
	h.render(w, "post.html", data, http.StatusOK)
}

func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}

	if r.Method == http.MethodGet {
		cats, err := h.Categories.List()
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		data := models.CreatePostPageData{CurrentUser: user, Categories: cats, SelectedIDs: map[int]bool{}}
		h.render(w, "create_post.html", data, http.StatusOK)
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	selectedRaw := r.Form["categories"]

	categoryIDs := make([]int, 0, len(selectedRaw))
	selectedMap := map[int]bool{}
	for _, raw := range selectedRaw {
		id, err := strconv.Atoi(raw)
		if err == nil && id > 0 {
			categoryIDs = append(categoryIDs, id)
			selectedMap[id] = true
		}
	}

	cats, err := h.Categories.List()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if title == "" || content == "" || len(categoryIDs) == 0 {
		data := models.CreatePostPageData{
			CurrentUser: user,
			Categories:  cats,
			Error:       "Title, content, and at least one category are required",
			Title:       title,
			Content:     content,
			SelectedIDs: selectedMap,
		}
		h.render(w, "create_post.html", data, http.StatusBadRequest)
		return
	}

	postID, err := h.Posts.Create(user.ID, title, content, categoryIDs)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/post?id="+strconv.Itoa(postID), http.StatusSeeOther)
}

func (h *Handler) DeletePost(w http.ResponseWriter, r *http.Request) {
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

	deleted, err := h.Posts.DeleteByAuthor(postID, user.ID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}
