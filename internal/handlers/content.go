package handlers

import (
	"database/sql"
	"errors"
	"forum/internal/logger"
	"forum/internal/markdown"
	"forum/internal/models"
	"forum/internal/repository"
	"forum/internal/validator"
	"html/template"
	"net/http"
	"strings"
)

const commentsPerPage = 20 // top-level comments per page; replies come along

// mentionMap resolves the @mentions found in texts to existing users, for
// rendering links (lowercase name → stored username).
func (h *Handler) mentionMap(texts ...string) map[string]string {
	var names []string
	seen := map[string]bool{}
	for _, t := range texts {
		for _, n := range markdown.Mentions(t) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	out := map[string]string{}
	if len(names) == 0 {
		return out
	}
	targets, err := h.Users.ByUsernames(names)
	if err != nil {
		logger.Error("mentions: lookup", err)
		return out
	}
	for k, t := range targets {
		out[k] = t.Username
	}
	return out
}

// notifyMentions notifies users @mentioned in text, except those mentioned
// in previous (the text before an edit) and the author.
func (h *Handler) notifyMentions(text, previous string, actorID, postID, commentID int) {
	old := map[string]bool{}
	for _, n := range markdown.Mentions(previous) {
		old[n] = true
	}
	var names []string
	for _, n := range markdown.Mentions(text) {
		if !old[n] {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return
	}
	targets, err := h.Users.ByUsernames(names)
	if err != nil {
		logger.Error("mentions: lookup", err)
		return
	}
	for _, t := range targets {
		if err := h.Notifications.NotifyComment(t.ID, repository.NotifyMention, actorID, postID, commentID); err != nil {
			logger.Error("notify mention", err)
		}
	}
}

// buildThread turns the flat comment list into a tree (at most
// MaxCommentDepth levels), renders Markdown and returns the top-level
// comments plus the number of comments that aren't deleted placeholders.
func (h *Handler) buildThread(flat []models.CommentView, viewerID int, viewerIsMod bool) ([]models.CommentView, int) {
	texts := make([]string, 0, len(flat))
	for _, c := range flat {
		texts = append(texts, c.Content)
	}
	mentions := h.mentionMap(texts...)
	ids := make([]int, len(flat))
	for i, c := range flat {
		ids[i] = c.ID
	}
	reactions, err := h.Reactions.CommentReactions(ids, viewerID)
	if err != nil {
		logger.Error("comment reactions", err)
	}

	byID := make(map[int]*models.CommentView, len(flat))
	children := map[int][]int{}
	var roots []int
	total := 0
	for i := range flat {
		c := &flat[i]
		// Hidden comments: only moderators and the author see the text.
		c.HiddenForViewer = c.IsHidden && !viewerIsMod && c.UserID != viewerID
		if c.HiddenForViewer {
			c.Content = ""
		}
		if !c.IsDeleted && !c.HiddenForViewer {
			total++
			c.HTML = markdown.Render(c.Content, markdown.Options{Mentions: mentions})
			c.Reactions = reactions[c.ID]
		}
		byID[c.ID] = c
	}
	for i := range flat {
		c := &flat[i]
		if _, ok := byID[c.ParentID]; c.ParentID != 0 && ok {
			children[c.ParentID] = append(children[c.ParentID], c.ID)
		} else {
			roots = append(roots, c.ID)
		}
	}

	var build func(id, depth int) models.CommentView
	build = func(id, depth int) models.CommentView {
		c := *byID[id]
		c.Depth = depth
		for _, kid := range children[id] {
			child := build(kid, min(depth+1, repository.MaxCommentDepth))
			c.ReplyCount += 1 + child.ReplyCount
			c.Replies = append(c.Replies, child)
		}
		return c
	}
	out := make([]models.CommentView, 0, len(roots))
	for _, id := range roots {
		out = append(out, build(id, 1))
	}
	return out, total
}

// EditComment updates the author's comment in place (fetch, JSON reply with
// the rendered HTML).
func (h *Handler) EditComment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 64<<10)
	if !ok {
		return
	}
	id, err := parseID(r.FormValue("comment_id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	if !validator.ValidateContent(content, 10000) {
		jsonError(w, http.StatusBadRequest, "Комментарий должен быть от 1 до 10000 символов")
		return
	}
	ref, err := h.Comments.Ref(id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (ref.UserID != user.ID || ref.IsDeleted)) {
		jsonError(w, http.StatusForbidden, "Этот комментарий нельзя изменить")
		return
	}
	if err != nil {
		logger.Error("Comments.Ref", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	var previous string
	if err := h.DB.QueryRow(`SELECT content FROM comments WHERE id = ?`, id).Scan(&previous); err != nil {
		logger.Error("comment: previous content", err)
	}
	if _, err := h.Comments.Update(id, user.ID, content); err != nil {
		logger.Error("Comments.Update", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	h.notifyMentions(content, previous, user.ID, ref.PostID, id)
	html := markdown.Render(content, markdown.Options{Mentions: h.mentionMap(content)})
	writeJSON(w, http.StatusOK, map[string]any{"html": html, "content": content})
}

// MarkdownPreview renders the editor's text for the «Предпросмотр» tab.
func (h *Handler) MarkdownPreview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.jsonAction(w, r, 64<<10); !ok {
		return
	}
	text := r.FormValue("text")
	if strings.TrimSpace(text) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"html": template.HTML(`<p class="md-empty">Нечего показать</p>`)})
		return
	}
	html := markdown.Render(text, markdown.Options{Headings: r.FormValue("headings") == "1", Mentions: h.mentionMap(text)})
	writeJSON(w, http.StatusOK, map[string]any{"html": html})
}

// UserSearch suggests users for @mentions: GET /users/search?q=prefix.
func (h *Handler) UserSearch(w http.ResponseWriter, r *http.Request) {
	if _, err := h.getCurrentUser(r); err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || len(q) > 20 || !validator.ValidateUsernamePrefix(q) {
		writeJSON(w, http.StatusOK, map[string]any{"users": []any{}})
		return
	}
	users, err := h.Users.SearchByPrefix(q, 6)
	if err != nil {
		logger.Error("users: search", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	type item struct {
		Username string `json:"username"`
		Avatar   string `json:"avatar,omitempty"`
		Initial  string `json:"initial"`
		Color    string `json:"color"`
	}
	out := make([]item, 0, len(users))
	for _, u := range users {
		it := item{Username: u.Username, Initial: u.Initial(), Color: u.AvatarColor()}
		if u.Avatar != "" {
			it.Avatar = "/uploads/avatars/" + u.Avatar
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// ToggleBookmark saves or unsaves a post: POST /bookmark → {"bookmarked": bool}.
func (h *Handler) ToggleBookmark(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	postID, err := parseID(r.FormValue("post_id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	var exists int
	if err := h.DB.QueryRow(`SELECT 1 FROM posts WHERE id = ?`, postID).Scan(&exists); err != nil {
		jsonError(w, http.StatusNotFound, "Пост не найден")
		return
	}
	saved, err := h.Bookmarks.Toggle(user.ID, postID)
	if err != nil {
		logger.Error("Bookmarks.Toggle", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"bookmarked": saved})
}
