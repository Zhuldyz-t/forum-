package handlers

import (
	"forum/internal/models"
	"net/http"
)

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	user, _ := h.getCurrentUser(r)
	category := r.URL.Query().Get("category")
	filterType := r.URL.Query().Get("filter")
	currentUserID := 0
	if user != nil {
		currentUserID = user.ID
	}

	if (filterType == "my_posts" || filterType == "liked") && user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	posts, err := h.Posts.List(filterType, category, currentUserID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	categories, err := h.Categories.List()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	data := models.HomePageData{
		CurrentUser: user,
		Posts:       posts,
		Categories:  categories,
		SelectedCat: category,
		Selected:    filterType,
	}
	h.render(w, "index.html", data, http.StatusOK)
}
