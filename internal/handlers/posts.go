package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"forum/internal/logger"
	"forum/internal/markdown"
	"forum/internal/models"
	"forum/internal/validator"
	"net/http"
	"net/url"
	"regexp"
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
	viewerID := 0
	if user != nil {
		viewerID = user.ID
	}
	post, err := h.Posts.Get(postID, viewerID)
	if err != nil {
		if err == sql.ErrNoRows {
			h.renderError(w, http.StatusNotFound, "Пост не найден", "Этот пост не существует или был удалён.")
			return
		}
		logger.Error("Posts.Get", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	// A hidden post is visible only to moderators and its author.
	if post.IsHidden && (user == nil || (!user.IsModerator() && user.ID != post.AuthorID)) {
		h.renderError(w, http.StatusNotFound, "Пост не найден", "Этот пост не существует или был удалён.")
		return
	}
	flat, err := h.Comments.ListByPost(postID)
	if err != nil {
		logger.Error("Comments.ListByPost", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	roots, total := h.buildThread(flat, viewerID, user != nil && user.IsModerator())

	// Best answer: pinned first. A top-level comment moves to the top of the
	// thread; a reply is also shown as a pinned copy above the comments.
	bestID, _, isQuestion, err := h.Posts.BestAnswerInfo(postID)
	if err != nil {
		logger.Error("best answer info", err)
	}
	var pinned *models.CommentView
	roots, pinned = markBest(roots, bestID)

	// Paginate top-level comments; ?comment=N opens the page that shows N.
	page := parsePage(r.URL.Query().Get("page"))
	if cid, err := parseID(r.URL.Query().Get("comment")); err == nil && r.URL.Query().Get("page") == "" {
		page = pageOfComment(roots, cid)
	}
	start := min((page-1)*commentsPerPage, len(roots))
	end := min(start+commentsPerPage, len(roots))
	comments, hasNext := roots[start:end], end < len(roots)

	poll, err := h.Polls.ByPost(postID, viewerID)
	if err != nil {
		logger.Error("Polls.ByPost", err)
	}
	post.HTML = markdown.Render(post.Content, markdown.Options{Headings: true, Mentions: h.mentionMap(post.Content)})

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
		CurrentUser:  user,
		Poll:         poll,
		Best:         pinned,
		CanMarkBest:  user != nil && user.ID == post.AuthorID && isQuestion,
		CommentTotal: total,
		Post:         post,
		Comments:     comments,
		CSRFToken:    token,
		Page:         page,
		HasNext:      hasNext,
		PrevURL:      prevURL,
		NextURL:      nextURL,
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

	// The form is multipart because of the optional image.
	r.Body = http.MaxBytesReader(w, r.Body, avatarMaxSize+(512<<10))
	if err := r.ParseMultipartForm(avatarMaxSize); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		h.renderError(w, http.StatusRequestEntityTooLarge, "Файл слишком большой", "Картинка может весить не больше 10 МБ.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll() //nolint:errcheck
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

	poll, pollErr := parsePoll(r)
	errData.Poll = poll.formData()
	if pollErr != "" {
		errData.Error = pollErr
		h.render(w, "create_post.html", errData, http.StatusBadRequest)
		return
	}

	imagePath := ""
	if file, _, err := r.FormFile("image"); err == nil {
		name, msg := saveImage(file, postImages)
		file.Close()
		if msg != "" {
			errData.Error = msg
			h.render(w, "create_post.html", errData, http.StatusBadRequest)
			return
		}
		imagePath = name
	} else if !errors.Is(err, http.ErrMissingFile) && !errors.Is(err, http.ErrNotMultipart) {
		errData.Error = "Не удалось загрузить картинку"
		h.render(w, "create_post.html", errData, http.StatusBadRequest)
		return
	}

	postID, err := h.Posts.Create(user.ID, title, content, imagePath, categoryIDs)
	if err != nil {
		logger.Error("Posts.Create", err)
		removeImageFile(postImages, imagePath)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	if poll.Enabled {
		if err := h.Polls.Create(postID, poll.Question, poll.Multiple, poll.EndsAt, poll.Options); err != nil {
			logger.Error("Polls.Create", err)
		}
	}
	if err := h.Notifications.NotifyFollowers(user.ID, postID); err != nil {
		logger.Error("notify followers", err)
	}
	h.notifyMentions(content, "", user.ID, postID, 0)
	h.afterChange(user.ID)
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

	post, err := h.Posts.Get(postID, user.ID)
	if err != nil && err != sql.ErrNoRows {
		logger.Error("Posts.Get", err)
	}
	touched := h.usersTouchedByPost(postID)
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
	removeImageFile(postImages, post.ImagePath)
	h.afterChange(touched...)

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

	post, err := h.Posts.Get(postID, user.ID)
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
			ImagePath:   post.ImagePath,
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

	r.Body = http.MaxBytesReader(w, r.Body, avatarMaxSize+(512<<10))
	if err := r.ParseMultipartForm(avatarMaxSize); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		h.renderError(w, http.StatusRequestEntityTooLarge, "Файл слишком большой", "Картинка может весить не больше 10 МБ.")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll() //nolint:errcheck
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
		ImagePath:   post.ImagePath,
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

	// Image: keep, replace with a new upload, or remove.
	imagePath := post.ImagePath
	if r.FormValue("remove_image") == "1" {
		imagePath = ""
	}
	newImage := ""
	if file, _, err := r.FormFile("image"); err == nil {
		name, msg := saveImage(file, postImages)
		file.Close()
		if msg != "" {
			errData.Error = msg
			h.render(w, "edit_post.html", errData, http.StatusBadRequest)
			return
		}
		newImage, imagePath = name, name
	}

	updated, err := h.Posts.Update(postID, user.ID, title, content, imagePath, categoryIDs)
	if err != nil {
		logger.Error("Posts.Update", err)
		removeImageFile(postImages, newImage)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if !updated {
		removeImageFile(postImages, newImage)
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Ты не можешь редактировать этот пост.")
		return
	}
	if imagePath != post.ImagePath {
		removeImageFile(postImages, post.ImagePath)
	}
	h.notifyMentions(content, post.Content, user.ID, postID, 0)

	http.Redirect(w, r, "/post?id="+strconv.Itoa(postID), http.StatusSeeOther)
}

// SearchPosts is the old search URL; search now lives on the home page.
func (h *Handler) SearchPosts(w http.ResponseWriter, r *http.Request) {
	v := url.Values{}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		v.Set("q", q)
	}
	target := "/"
	if len(v) > 0 {
		target += "?" + v.Encode()
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// PostImage serves images attached to posts.
func (h *Handler) PostImage(w http.ResponseWriter, r *http.Request) {
	h.serveImage(w, r, postImages, "/uploads/posts/")
}

var postImages = imageKind{
	dir:      "uploads/posts",
	types:    map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true},
	typesMsg: "Можно загрузить только JPG, PNG, GIF или WEBP",
	maxSide:  1600,
	name:     regexp.MustCompile(`^[a-f0-9]{32}\.jpg$`),
}

// parsePage parses the page query param, defaulting to 1.
func parsePage(s string) int {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p < 1 {
		return 1
	}
	return p
}

// pageOfComment finds the comments page that shows commentID.
func pageOfComment(roots []models.CommentView, commentID int) int {
	var contains func(c models.CommentView) bool
	contains = func(c models.CommentView) bool {
		if c.ID == commentID {
			return true
		}
		for _, r := range c.Replies {
			if contains(r) {
				return true
			}
		}
		return false
	}
	for i, root := range roots {
		if contains(root) {
			return i/commentsPerPage + 1
		}
	}
	return 1
}

// markBest flags the best answer in the thread. A best top-level comment is
// moved to the front; for a best reply a copy (without its replies) is
// returned to pin above the thread.
func markBest(roots []models.CommentView, bestID int) ([]models.CommentView, *models.CommentView) {
	if bestID == 0 {
		return roots, nil
	}
	var pinned *models.CommentView
	var mark func(cs []models.CommentView)
	mark = func(cs []models.CommentView) {
		for i := range cs {
			if cs[i].ID == bestID {
				cs[i].IsBest = true
				c := cs[i]
				c.Replies, c.ReplyCount = nil, 0
				pinned = &c
			}
			mark(cs[i].Replies)
		}
	}
	mark(roots)
	for i, r := range roots {
		if r.ID == bestID {
			out := append([]models.CommentView{r}, roots[:i]...)
			return append(out, roots[i+1:]...), nil
		}
	}
	return roots, pinned
}
