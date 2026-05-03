package handlers

import (
	"fmt"
	"forum/internal/logger"
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
	page := parsePage(r.URL.Query().Get("page"))
	currentUserID := 0
	if user != nil {
		currentUserID = user.ID
	}

	if (filterType == "my_posts" || filterType == "liked") && user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	posts, hasNext, err := h.Posts.List(filterType, category, currentUserID, page)
	if err != nil {
		logger.Error("Posts.List", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	categories, err := h.Categories.List()
	if err != nil {
		logger.Error("Categories.List", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	token := h.generateCSRFToken()
	h.setCSRFCookie(w, token)

	prevURL, nextURL := "", ""
	if page > 1 {
		u := fmt.Sprintf("/?page=%d", page-1)
		if category != "" {
			u += "&category=" + category
		}
		if filterType != "" {
			u += "&filter=" + filterType
		}
		prevURL = u
	}
	if hasNext {
		u := fmt.Sprintf("/?page=%d", page+1)
		if category != "" {
			u += "&category=" + category
		}
		if filterType != "" {
			u += "&filter=" + filterType
		}
		nextURL = u
	}

	data := models.HomePageData{
		CurrentUser: user,
		Posts:       posts,
		Categories:  categories,
		SelectedCat: category,
		Selected:    filterType,
		CSRFToken:   token,
		Page:        page,
		HasNext:     hasNext,
		PrevURL:     prevURL,
		NextURL:     nextURL,
	}
	h.render(w, "index.html", data, http.StatusOK)
}
