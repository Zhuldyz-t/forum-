package handlers

import (
	"bytes"
	"database/sql"
	"errors"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/repository"
	"net/http"
	"strconv"
	"strings"
)

// lookupUser finds a user by numeric id or by username.
func (h *Handler) lookupUser(key string) (*models.User, error) {
	if id, err := strconv.Atoi(key); err == nil && id > 0 {
		return h.Users.GetByID(id)
	}
	return h.Users.GetByUsername(key)
}

// UserRoutes serves /user/{id|username} (public profile) and
// /user/{id|username}/followers|following (list for the modal, JSON).
func (h *Handler) UserRoutes(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/user/"), "/"), "/")
	if parts[0] == "" || len(parts) > 2 {
		h.NotFound(w, r)
		return
	}
	user, err := h.lookupUser(parts[0])
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if len(parts) == 2 {
				jsonError(w, http.StatusNotFound, "Пользователь не найден")
				return
			}
			h.renderError(w, http.StatusNotFound, "Пользователь не найден", "Такого профиля не существует.")
			return
		}
		logger.Error("user: lookup", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	if len(parts) == 2 {
		switch parts[1] {
		case "followers":
			h.followList(w, r, user, true)
		case "following":
			h.followList(w, r, user, false)
		default:
			h.NotFound(w, r)
		}
		return
	}

	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	viewer, _ := h.getCurrentUser(r)
	if viewer != nil && viewer.ID == user.ID {
		target := "/profile"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	h.renderProfile(w, r, "profile.html", viewer, user, "", "", http.StatusOK)
}

// followList renders the followers/following list for the profile modal.
func (h *Handler) followList(w http.ResponseWriter, r *http.Request, user *models.User, followers bool) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	viewer, _ := h.getCurrentUser(r)
	users, err := h.Follows.List(user.ID, followers, 500)
	if err != nil {
		logger.Error("follows: list", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	viewerID := 0
	if viewer != nil {
		viewerID = viewer.ID
	}
	set, err := h.Follows.FollowingSet(viewerID)
	if err != nil {
		logger.Error("follows: set", err)
	}
	var buf bytes.Buffer
	err = h.Tmpl.ExecuteTemplate(&buf, "follow-list", map[string]any{
		"Users": users, "Following": set, "Followers": followers,
		"Page": models.HomePageData{CurrentUser: viewer, CSRFToken: h.pageCSRFToken(w, r)},
	})
	if err != nil {
		logger.Error("follows: render", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": buf.String()})
}

// followAction handles POST /follow and /unfollow (fetch, JSON reply).
func (h *Handler) followAction(w http.ResponseWriter, r *http.Request, follow bool) {
	viewer, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	targetID, err := parseID(r.FormValue("user_id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	if targetID == viewer.ID {
		jsonError(w, http.StatusBadRequest, "Нельзя подписаться на себя")
		return
	}
	if _, err := h.Users.GetByID(targetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Пользователь не найден")
			return
		}
		logger.Error("follow: get user", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}

	if follow {
		created, err := h.Follows.Follow(viewer.ID, targetID)
		if err != nil {
			logger.Error("Follows.Follow", err)
			jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
			return
		}
		if created {
			if err := h.Notifications.Notify(targetID, repository.NotifyFollow, viewer.ID, 0); err != nil {
				logger.Error("notify follow", err)
			}
		}
	} else if err := h.Follows.Unfollow(viewer.ID, targetID); err != nil {
		logger.Error("Follows.Unfollow", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}

	h.afterChange(targetID)
	followers, _, err := h.Follows.Counts(targetID)
	if err != nil {
		logger.Error("Follows.Counts", err)
	}
	label, _ := models.FollowersLabel(followers, 0)
	writeJSON(w, http.StatusOK, map[string]any{"following": follow, "followers": followers, "followersText": label})
}

func (h *Handler) Follow(w http.ResponseWriter, r *http.Request)   { h.followAction(w, r, true) }
func (h *Handler) Unfollow(w http.ResponseWriter, r *http.Request) { h.followAction(w, r, false) }

// ─── Notifications ──────────────────────────────────────────

// notifyPostAuthor notifies the author of postID about actorID's action.
// Failures are logged: a missing notification must not break the action.
func (h *Handler) notifyPostAuthor(postID int, kind string, actorID int) {
	var authorID int
	if err := h.DB.QueryRow(`SELECT user_id FROM posts WHERE id = ?`, postID).Scan(&authorID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logger.Error("notify: post author", err)
		}
		return
	}
	if err := h.Notifications.Notify(authorID, kind, actorID, postID); err != nil {
		logger.Error("notify "+kind, err)
	}
}

// NotificationsCount: GET → {"count": N} for the bell badge.
func (h *Handler) NotificationsCount(w http.ResponseWriter, r *http.Request) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	n, err := h.Notifications.UnreadCount(user.ID)
	if err != nil {
		logger.Error("notifications: count", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

// Notifications: GET → {"html": "..."} with the latest 20 for the dropdown.
func (h *Handler) NotificationsList(w http.ResponseWriter, r *http.Request) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	list, err := h.Notifications.List(user.ID, 20)
	if err != nil {
		logger.Error("notifications: list", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	var buf bytes.Buffer
	if err := h.Tmpl.ExecuteTemplate(&buf, "notification-items", list); err != nil {
		logger.Error("notifications: render", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": buf.String()})
}

// NotificationsRead: POST marks all as read.
func (h *Handler) NotificationsRead(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	if err := h.Notifications.MarkAllRead(user.ID); err != nil {
		logger.Error("notifications: mark read", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": 0})
}
