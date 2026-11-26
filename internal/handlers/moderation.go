package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/repository"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)


// staffAction checks a moderation form post: logged in, POST, CSRF, and the
// required role. On failure it writes the response and returns false.
func (h *Handler) staffAction(w http.ResponseWriter, r *http.Request, adminOnly bool) (*models.User, bool) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return nil, false
	}
	if !h.methodRequired(w, r, http.MethodPost) {
		return nil, false
	}
	if !h.validateCSRFToken(r) {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Неверный токен безопасности. Обнови страницу и попробуй снова.")
		return nil, false
	}
	if !user.IsModerator() || (adminOnly && !user.IsAdmin()) {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Это действие доступно только модераторам и администраторам.")
		return nil, false
	}
	return user, true
}

// back redirects to the page the form came from with a one-shot message
// that site.js shows as a toast.
func (h *Handler) back(w http.ResponseWriter, r *http.Request, fallback, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name: "flash", Value: url.QueryEscape(msg), Path: "/", MaxAge: 30,
		SameSite: http.SameSiteLaxMode, Secure: h.SecureCookies,
	})
	http.Redirect(w, r, safeRedirect(r, fallback), http.StatusSeeOther)
}

func (h *Handler) logAction(actor *models.User, action, targetType string, targetID int, details string) {
	if err := h.Moderation.Log(actor, action, targetType, targetID, details); err != nil {
		logger.Error("mod log", err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, where string, err error) {
	logger.Error(where, err)
	h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
}


// Report files a complaint: POST /report (target_type, target_id, reason,
// details), fetch with a JSON reply.
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	kind := r.FormValue("target_type")
	id, err := parseID(r.FormValue("target_id"))
	reason := r.FormValue("reason")
	details := strings.TrimSpace(r.FormValue("details"))
	if err != nil || (kind != "post" && kind != "comment") || !models.IsReportReason(reason) {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	if utf8.RuneCountInString(details) > 500 {
		jsonError(w, http.StatusBadRequest, "Пояснение — не больше 500 символов")
		return
	}
	var author int
	table := map[string]string{"post": `SELECT user_id FROM posts WHERE id = ?`, "comment": `SELECT user_id FROM comments WHERE id = ? AND is_deleted = 0`}[kind]
	if err := h.DB.QueryRow(table, id).Scan(&author); err != nil {
		jsonError(w, http.StatusNotFound, "Не найдено")
		return
	}
	if author == user.ID {
		jsonError(w, http.StatusBadRequest, "Нельзя пожаловаться на свой материал")
		return
	}
	if err := h.Moderation.Report(user.ID, kind, id, reason, details); err != nil {
		if errors.Is(err, repository.ErrDuplicateReport) {
			jsonError(w, http.StatusConflict, "Вы уже отправили жалобу на это")
			return
		}
		logger.Error("report", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Жалоба отправлена модераторам"})
}

// ApplyModerator files «Стать модератором»: POST /moderator/apply (text).
func (h *Handler) ApplyModerator(w http.ResponseWriter, r *http.Request) {
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
	if user.IsModerator() {
		h.back(w, r, "/profile", "Вы уже модератор")
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || utf8.RuneCountInString(text) > 1000 {
		h.back(w, r, "/profile", "Расскажите, почему хотите стать модератором (до 1000 символов)")
		return
	}
	created, err := h.Moderation.Apply("moderator", user.ID, 0, text)
	if err != nil {
		h.serverError(w, "apply moderator", err)
		return
	}
	msg := "Заявка отправлена администратору"
	if !created {
		msg = "Ваша заявка уже на рассмотрении"
	}
	h.back(w, r, "/profile", msg)
}


// ModeratePost hides, unhides or deletes a post: POST /mod/post (post_id, action).
func (h *Handler) ModeratePost(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, false)
	if !ok {
		return
	}
	id, err := parseID(r.FormValue("post_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор поста.")
		return
	}
	post, err := h.Posts.Get(id, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		h.back(w, r, "/admin", "Пост уже удалён")
		return
	} else if err != nil {
		h.serverError(w, "mod post", err)
		return
	}
	title := fmt.Sprintf("«%s» (автор %s)", post.Title, post.AuthorName)

	switch r.FormValue("action") {
	case "hide", "unhide":
		hide := r.FormValue("action") == "hide"
		if _, err := h.Moderation.SetPostHidden(id, hide); err != nil {
			h.serverError(w, "hide post", err)
			return
		}
		if hide {
			h.closeReports("post", id, "resolved", user)
			h.logAction(user, "hide_post", "post", id, title)
			h.back(w, r, "/admin", "Пост скрыт")
		} else {
			h.logAction(user, "unhide_post", "post", id, title)
			h.back(w, r, "/admin", "Пост снова виден")
		}
	case "delete":
		touched := h.usersTouchedByPost(id)
		if _, err := h.DB.Exec(`DELETE FROM posts WHERE id = ?`, id); err != nil {
			h.serverError(w, "delete post", err)
			return
		}
		removeImageFile(postImages, post.ImagePath)
		h.closeReports("post", id, "resolved", user)
		h.logAction(user, "delete_post", "post", id, title)
		h.afterChange(touched...)
		// The post page is gone: return to the admin panel or the feed.
		fallback := "/"
		if strings.Contains(r.Referer(), "/admin") {
			fallback = "/admin"
		}
		r.Header.Del("Referer")
		h.back(w, r, fallback, "Пост удалён")
	default:
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неизвестное действие.")
	}
}

// ModerateComment hides, unhides or deletes a comment: POST /mod/comment.
func (h *Handler) ModerateComment(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, false)
	if !ok {
		return
	}
	id, err := parseID(r.FormValue("comment_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор комментария.")
		return
	}
	ref, err := h.Comments.Ref(id)
	if err != nil || ref.IsDeleted {
		h.back(w, r, "/admin", "Комментарий уже удалён")
		return
	}
	var author string
	h.DB.QueryRow(`SELECT username FROM users WHERE id = ?`, ref.UserID).Scan(&author) //nolint:errcheck
	details := fmt.Sprintf("к посту #%d (автор %s)", ref.PostID, author)

	switch r.FormValue("action") {
	case "hide", "unhide":
		hide := r.FormValue("action") == "hide"
		if _, err := h.Moderation.SetCommentHidden(id, hide); err != nil {
			h.serverError(w, "hide comment", err)
			return
		}
		if hide {
			h.closeReports("comment", id, "resolved", user)
			h.logAction(user, "hide_comment", "comment", id, details)
			h.back(w, r, "/admin", "Комментарий скрыт")
		} else {
			h.logAction(user, "unhide_comment", "comment", id, details)
			h.back(w, r, "/admin", "Комментарий снова виден")
		}
	case "delete":
		// Same rules as the author's own delete: a comment with replies
		// becomes a placeholder.
		if _, err := h.Comments.DeleteByAuthor(id, ref.UserID); err != nil {
			h.serverError(w, "delete comment", err)
			return
		}
		h.closeReports("comment", id, "resolved", user)
		h.logAction(user, "delete_comment", "comment", id, details)
		h.afterChange(ref.UserID)
		h.back(w, r, "/admin", "Комментарий удалён")
	default:
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неизвестное действие.")
	}
}

func (h *Handler) closeReports(kind string, id int, status string, by *models.User) {
	if err := h.Moderation.CloseReports(kind, id, status, by.ID); err != nil {
		logger.Error("close reports", err)
	}
}

// DismissReport rejects a complaint: POST /mod/report/dismiss (report_id).
func (h *Handler) DismissReport(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, false)
	if !ok {
		return
	}
	id, err := parseID(r.FormValue("report_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор жалобы.")
		return
	}
	kind, target, err := h.Moderation.ReportTarget(id)
	if err != nil {
		h.back(w, r, "/admin", "Жалоба не найдена")
		return
	}
	h.closeReports(kind, target, "dismissed", user)
	h.logAction(user, "dismiss_report", kind, target, fmt.Sprintf("жалоба #%d", id))
	h.back(w, r, "/admin", "Жалоба отклонена")
}

// RequestUserDeletion lets a moderator ask an admin to delete a user:
// POST /mod/user/request-delete (user_id, reason).
func (h *Handler) RequestUserDeletion(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, false)
	if !ok {
		return
	}
	target, err := h.targetUser(r)
	if err != nil {
		h.back(w, r, "/admin?tab=users", "Пользователь не найден")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" || utf8.RuneCountInString(reason) > 500 {
		h.back(w, r, "/admin?tab=users", "Укажите причину (до 500 символов)")
		return
	}
	if target.IsModerator() {
		h.back(w, r, "/admin?tab=users", "Нельзя запросить удаление модератора или админа")
		return
	}
	created, err := h.Moderation.Apply("delete_user", user.ID, target.ID, reason)
	if err != nil {
		h.serverError(w, "request delete", err)
		return
	}
	if !created {
		h.back(w, r, "/admin?tab=users", "Запрос на этого пользователя уже отправлен")
		return
	}
	h.logAction(user, "request_delete_user", "user", target.ID, target.Username+": "+reason)
	h.back(w, r, "/admin?tab=users", "Запрос отправлен администратору")
}

// ─── Admin actions ──────────────────────────────────────────

func (h *Handler) targetUser(r *http.Request) (*models.User, error) {
	id, err := parseID(r.FormValue("user_id"))
	if err != nil {
		return nil, err
	}
	return h.Users.GetByID(id)
}

// guardTarget refuses actions on admins and on oneself.
func guardTarget(actor, target *models.User) string {
	switch {
	case target.ID == actor.ID:
		return "Это действие нельзя применить к себе"
	case target.IsAdmin():
		return "Это действие нельзя применить к администратору"
	}
	return ""
}

// SetUserRole makes a user a moderator or back a regular user:
// POST /admin/user/role (user_id, role).
func (h *Handler) SetUserRole(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, true)
	if !ok {
		return
	}
	target, err := h.targetUser(r)
	if err != nil {
		h.back(w, r, "/admin?tab=users", "Пользователь не найден")
		return
	}
	role := r.FormValue("role")
	if role != "moderator" && role != "user" {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неизвестная роль.")
		return
	}
	if msg := guardTarget(user, target); msg != "" {
		h.back(w, r, "/admin?tab=users", msg)
		return
	}
	if err := h.Users.SetRole(target.ID, role); err != nil {
		h.serverError(w, "set role", err)
		return
	}
	label := map[string]string{"moderator": "модератор", "user": "пользователь"}[role]
	h.logAction(user, "set_role", "user", target.ID, target.Username+" → "+label)
	h.back(w, r, "/admin?tab=users", target.Username+" теперь "+label)
}

// BanUser blocks a user for a period or for ever: POST /admin/user/ban
// (user_id, duration: 1d|7d|30d|forever, reason).
func (h *Handler) BanUser(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, true)
	if !ok {
		return
	}
	target, err := h.targetUser(r)
	if err != nil {
		h.back(w, r, "/admin?tab=users", "Пользователь не найден")
		return
	}
	if msg := guardTarget(user, target); msg != "" {
		h.back(w, r, "/admin?tab=users", msg)
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" || utf8.RuneCountInString(reason) > 300 {
		h.back(w, r, "/admin?tab=users", "Укажите причину блокировки (до 300 символов)")
		return
	}
	durations := map[string]time.Duration{"1d": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
	until := models.PermanentBan
	term := "навсегда"
	if d, ok := durations[r.FormValue("duration")]; ok {
		until = time.Now().Add(d)
		term = "до " + until.Local().Format("02.01.2006 15:04")
	} else if r.FormValue("duration") != "forever" {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неизвестный срок блокировки.")
		return
	}
	if err := h.Users.Ban(target.ID, until, reason); err != nil {
		h.serverError(w, "ban", err)
		return
	}
	if h.Hub != nil {
		h.Hub.Send(target.ID, map[string]any{"type": "banned"})
	}
	h.logAction(user, "ban_user", "user", target.ID, fmt.Sprintf("%s %s: %s", target.Username, term, reason))
	h.back(w, r, "/admin?tab=users", target.Username+" заблокирован(а) "+term)
}

// UnbanUser lifts a ban: POST /admin/user/unban (user_id).
func (h *Handler) UnbanUser(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, true)
	if !ok {
		return
	}
	target, err := h.targetUser(r)
	if err != nil {
		h.back(w, r, "/admin?tab=users", "Пользователь не найден")
		return
	}
	if err := h.Users.Unban(target.ID); err != nil {
		h.serverError(w, "unban", err)
		return
	}
	h.logAction(user, "unban_user", "user", target.ID, target.Username)
	h.back(w, r, "/admin?tab=users", target.Username+" разблокирован(а)")
}

// DeleteUser removes an account and its files: POST /admin/user/delete.
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, true)
	if !ok {
		return
	}
	target, err := h.targetUser(r)
	if err != nil {
		h.back(w, r, "/admin?tab=users", "Пользователь не найден")
		return
	}
	if msg := guardTarget(user, target); msg != "" {
		h.back(w, r, "/admin?tab=users", msg)
		return
	}
	if err := h.deleteAccount(target); err != nil {
		h.serverError(w, "delete user", err)
		return
	}
	h.logAction(user, "delete_user", "user", target.ID, target.Username+" ("+target.Email+")")
	h.back(w, r, "/admin?tab=users", "Пользователь "+target.Username+" удалён")
}

// deleteAccount deletes the user and their uploaded files, then fixes the
// reputation of people whose scores depended on them.
func (h *Handler) deleteAccount(target *models.User) error {
	files, err := h.Users.Files(target.ID)
	if err != nil {
		return err
	}
	// Authors whose posts the user commented on or liked lose those points.
	var affected []int
	rows, err := h.DB.Query(`SELECT DISTINCT p.user_id FROM posts p WHERE p.id IN (
		SELECT post_id FROM comments WHERE user_id = ? UNION SELECT post_id FROM post_reactions WHERE user_id = ?)
		UNION SELECT following_id FROM follows WHERE follower_id = ?`, target.ID, target.ID, target.ID)
	if err == nil {
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				affected = append(affected, id)
			}
		}
		rows.Close()
	}
	if err := h.Users.Delete(target.ID); err != nil {
		return err
	}
	for _, f := range files.Avatars {
		removeAvatarFile(f)
	}
	removeImageFile(coverImages, files.Cover)
	for _, f := range files.PostImages {
		removeImageFile(postImages, f)
	}
	h.afterChange(affected...)
	return nil
}

// DecideRequest approves or rejects an application or a deletion request:
// POST /admin/request (request_id, decision: approve|reject).
func (h *Handler) DecideRequest(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, true)
	if !ok {
		return
	}
	id, err := parseID(r.FormValue("request_id"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор заявки.")
		return
	}
	q, err := h.Moderation.Request(id)
	if err != nil || q.Status != "pending" {
		h.back(w, r, "/admin?tab=requests", "Заявка уже рассмотрена")
		return
	}
	approve := r.FormValue("decision") == "approve"
	if approve {
		switch q.Type {
		case "moderator":
			if err := h.Users.SetRole(q.UserID, "moderator"); err != nil {
				h.serverError(w, "approve moderator", err)
				return
			}
		case "delete_user":
			target, err := h.Users.GetByID(q.TargetID)
			if err != nil {
				h.back(w, r, "/admin?tab=requests", "Пользователь уже удалён")
				return
			}
			if msg := guardTarget(user, target); msg != "" {
				h.back(w, r, "/admin?tab=requests", msg)
				return
			}
			if err := h.deleteAccount(target); err != nil {
				h.serverError(w, "delete user", err)
				return
			}
			h.logAction(user, "delete_user", "user", target.ID, target.Username+" (по запросу)")
		}
	}
	// A deleted target removes the request with it (ON DELETE CASCADE).
	if err := h.Moderation.DecideRequest(id, approve, user.ID); err != nil {
		h.serverError(w, "decide request", err)
		return
	}
	action, msg := "reject_request", "Заявка отклонена"
	if approve {
		action, msg = "approve_request", "Заявка одобрена"
	}
	h.logAction(user, action, "request", id, q.TypeLabel())
	h.back(w, r, "/admin?tab=requests", msg)
}

// ManageCategory creates, renames or deletes a category:
// POST /admin/category (action, id, name).
func (h *Handler) ManageCategory(w http.ResponseWriter, r *http.Request) {
	user, ok := h.staffAction(w, r, true)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	validName := name != "" && utf8.RuneCountInString(name) <= 30
	switch r.FormValue("action") {
	case "create":
		if !validName {
			h.back(w, r, "/admin?tab=categories", "Название: от 1 до 30 символов")
			return
		}
		if err := h.Moderation.CreateCategory(name); err != nil {
			h.back(w, r, "/admin?tab=categories", "Такая категория уже есть")
			return
		}
		h.logAction(user, "create_category", "category", 0, name)
		h.back(w, r, "/admin?tab=categories", "Категория «"+name+"» создана")
	case "rename":
		id, err := parseID(r.FormValue("id"))
		if err != nil || !validName {
			h.back(w, r, "/admin?tab=categories", "Название: от 1 до 30 символов")
			return
		}
		old, err := h.Moderation.RenameCategory(id, name)
		if err != nil {
			h.back(w, r, "/admin?tab=categories", "Не удалось переименовать: такое имя уже занято")
			return
		}
		h.logAction(user, "rename_category", "category", id, old+" → "+name)
		h.back(w, r, "/admin?tab=categories", "Категория переименована")
	case "delete":
		id, err := parseID(r.FormValue("id"))
		if err != nil {
			h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неверный идентификатор категории.")
			return
		}
		old, err := h.Moderation.DeleteCategory(id)
		if err != nil {
			h.back(w, r, "/admin?tab=categories", "Категория не найдена")
			return
		}
		h.logAction(user, "delete_category", "category", id, old)
		h.back(w, r, "/admin?tab=categories", "Категория «"+old+"» удалена")
	default:
		h.renderError(w, http.StatusBadRequest, "Неверный запрос", "Неизвестное действие.")
	}
}
