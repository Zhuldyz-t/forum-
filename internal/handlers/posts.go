package handlers

import (
	"database/sql"
	"fmt"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/validator"
	"net/http"
	"net/url"
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
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор поста.")
		return
	}
	post, err := h.Posts.Get(postID)
	if err != nil {
		if err == sql.ErrNoRows {
			h.renderError(w, http.StatusNotFound, "Пост не найден", "Этот пост не существует или был удалён.")
			return
		}
		logger.Error("Posts.Get", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	page := parsePage(r.URL.Query().Get("page"))
	comments, hasNext, err := h.Comments.ListByPost(postID, page)
	if err != nil {
		logger.Error("Comments.ListByPost", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	prevURL, nextURL := "", ""
	if page > 1 {
		prevURL = fmt.Sprintf("/post?id=%d&page=%d", postID, page-1)
	}
	if hasNext {
		nextURL = fmt.Sprintf("/post?id=%d&page=%d", postID, page+1)
	}

	token := h.generateCSRFToken()
	h.setCSRFCookie(w, token)
	data := models.PostPageData{
		CurrentUser: user,
		Post:        post,
		Comments:    comments,
		CSRFToken:   token,
		Page:        page,
		HasNext:     hasNext,
		PrevURL:     prevURL,
		NextURL:     nextURL,
	}
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
			logger.Error("Categories.List", err)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
		token := h.generateCSRFToken()
		h.setCSRFCookie(w, token)
		data := models.CreatePostPageData{CurrentUser: user, Categories: cats, SelectedIDs: map[int]bool{}, CSRFToken: token}
		h.render(w, "create_post.html", data, http.StatusOK)
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	if !h.validateCSRFToken(r) {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Неверный токен безопасности. Обнови страницу и попробуй снова.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	selectedRaw := r.Form["categories"]

	cats, err := h.Categories.List()
	if err != nil {
		logger.Error("Categories.List", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	validCatIDs := make(map[int]bool, len(cats))
	for _, c := range cats {
		validCatIDs[c.ID] = true
	}
	categoryIDs := make([]int, 0, len(selectedRaw))
	selectedMap := map[int]bool{}
	for _, raw := range selectedRaw {
		id, err := strconv.Atoi(raw)
		if err == nil && id > 0 && validCatIDs[id] {
			categoryIDs = append(categoryIDs, id)
			selectedMap[id] = true
		}
	}

	errToken := h.generateCSRFToken()
	h.setCSRFCookie(w, errToken)
	errData := models.CreatePostPageData{
		CurrentUser: user,
		Categories:  cats,
		Title:       title,
		Content:     content,
		SelectedIDs: selectedMap,
		CSRFToken:   errToken,
	}

	if len(categoryIDs) == 0 {
		errData.Error = "Необходимо выбрать хотя бы одну категорию"
		h.render(w, "create_post.html", errData, http.StatusBadRequest)
		return
	}
	if !validator.ValidateTitle(title) {
		errData.Error = "Заголовок должен содержать от 1 до 200 символов"
		h.render(w, "create_post.html", errData, http.StatusBadRequest)
		return
	}
	if !validator.ValidateContent(content, 10000) {
		errData.Error = "Содержимое должно содержать от 1 до 10000 символов"
		h.render(w, "create_post.html", errData, http.StatusBadRequest)
		return
	}

	postID, err := h.Posts.Create(user.ID, title, content, categoryIDs)
	if err != nil {
		logger.Error("Posts.Create", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
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

	if !h.validateCSRFToken(r) {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Неверный токен безопасности. Обнови страницу и попробуй снова.")
		return
	}

	postID, err := parseID(r.FormValue("post_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор поста.")
		return
	}

	deleted, err := h.Posts.DeleteByAuthor(postID, user.ID)
	if err != nil {
		logger.Error("Posts.DeleteByAuthor", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if !deleted {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Ты не можешь удалить этот пост.")
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) EditPost(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}

	postID, err := parseID(r.URL.Query().Get("id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор поста.")
		return
	}

	post, err := h.Posts.Get(postID)
	if err != nil {
		if err == sql.ErrNoRows {
			h.renderError(w, http.StatusNotFound, "Пост не найден", "Этот пост не существует или был удалён.")
			return
		}
		logger.Error("Posts.Get", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if post.AuthorID != user.ID {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Ты не можешь редактировать этот пост.")
		return
	}

	cats, err := h.Categories.List()
	if err != nil {
		logger.Error("Categories.List", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	if r.Method == http.MethodGet {
		catIDs, err := h.Posts.GetCategoryIDs(postID)
		if err != nil {
			logger.Error("Posts.GetCategoryIDs", err)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
		selectedMap := map[int]bool{}
		for _, id := range catIDs {
			selectedMap[id] = true
		}
		token := h.generateCSRFToken()
		h.setCSRFCookie(w, token)
		data := models.EditPostPageData{
			CurrentUser: user,
			PostID:      postID,
			Title:       post.Title,
			Content:     post.Content,
			Categories:  cats,
			SelectedIDs: selectedMap,
			CSRFToken:   token,
		}
		h.render(w, "edit_post.html", data, http.StatusOK)
		return
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return
	}

	if !h.validateCSRFToken(r) {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Неверный токен безопасности. Обнови страницу и попробуй снова.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	selectedRaw := r.Form["categories"]

	validCatIDs := make(map[int]bool, len(cats))
	for _, c := range cats {
		validCatIDs[c.ID] = true
	}
	categoryIDs := make([]int, 0, len(selectedRaw))
	selectedMap := map[int]bool{}
	for _, raw := range selectedRaw {
		id, err := strconv.Atoi(raw)
		if err == nil && id > 0 && validCatIDs[id] {
			categoryIDs = append(categoryIDs, id)
			selectedMap[id] = true
		}
	}

	errToken := h.generateCSRFToken()
	h.setCSRFCookie(w, errToken)
	errData := models.EditPostPageData{
		CurrentUser: user,
		PostID:      postID,
		Categories:  cats,
		Title:       title,
		Content:     content,
		SelectedIDs: selectedMap,
		CSRFToken:   errToken,
	}

	if len(categoryIDs) == 0 {
		errData.Error = "Необходимо выбрать хотя бы одну категорию"
		h.render(w, "edit_post.html", errData, http.StatusBadRequest)
		return
	}
	if !validator.ValidateTitle(title) {
		errData.Error = "Заголовок должен содержать от 1 до 200 символов"
		h.render(w, "edit_post.html", errData, http.StatusBadRequest)
		return
	}
	if !validator.ValidateContent(content, 10000) {
		errData.Error = "Содержимое должно содержать от 1 до 10000 символов"
		h.render(w, "edit_post.html", errData, http.StatusBadRequest)
		return
	}

	updated, err := h.Posts.Update(postID, user.ID, title, content, categoryIDs)
	if err != nil {
		logger.Error("Posts.Update", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if !updated {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Ты не можешь редактировать этот пост.")
		return
	}

	http.Redirect(w, r, "/post?id="+strconv.Itoa(postID), http.StatusSeeOther)
}

func (h *Handler) SearchPosts(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	user, _ := h.getCurrentUser(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := parsePage(r.URL.Query().Get("page"))

	posts, hasNext, err := h.Posts.Search(q, page)
	if err != nil {
		logger.Error("Posts.Search", err)
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
		prevURL = fmt.Sprintf("/posts/search?q=%s&page=%d", url.QueryEscape(q), page-1)
	}
	if hasNext {
		nextURL = fmt.Sprintf("/posts/search?q=%s&page=%d", url.QueryEscape(q), page+1)
	}

	data := models.HomePageData{
		CurrentUser: user,
		Posts:       posts,
		Categories:  categories,
		CSRFToken:   token,
		Page:        page,
		HasNext:     hasNext,
		PrevURL:     prevURL,
		NextURL:     nextURL,
		SearchQuery: q,
	}
	h.render(w, "index.html", data, http.StatusOK)
}

// parsePage parses the page query param, defaulting to 1.
func parsePage(s string) int {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p < 1 {
		return 1
	}
	return p
}
