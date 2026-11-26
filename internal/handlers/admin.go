package handlers

import (
	"forum/internal/logger"
	"forum/internal/models"
	"net/http"
	"strings"
)

const adminListLimit = 100

// AdminPage renders /admin for moderators and admins. Tabs that change
// roles, bans, categories or requests are read-only for moderators; the
// actions themselves are checked again on the server.
func (h *Handler) AdminPage(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	if !user.IsModerator() {
		h.renderError(w, http.StatusForbidden, "Нет доступа", "Панель доступна только модераторам и администраторам.")
		return
	}
	q := r.URL.Query()
	data := models.AdminPageData{
		CurrentUser: user,
		CSRFToken:   h.pageCSRFToken(w, r),
		Tab:         q.Get("tab"),
		Q:           strings.TrimSpace(q.Get("q")),
		Status:      q.Get("status"),
		Type:        q.Get("type"),
		Role:        q.Get("role"),
		State:       q.Get("state"),
		Action:      q.Get("action"),
	}
	var err error
	switch data.Tab {
	case "users":
		if data.Role != "user" && data.Role != "moderator" && data.Role != "admin" {
			data.Role = ""
		}
		data.Users, err = h.Users.AdminList(data.Q, data.Role, data.State, adminListLimit)
	case "categories":
		data.Categories, err = h.Categories.ListWithCounts()
	case "requests":
		if data.Status == "" {
			data.Status = "pending"
		}
		status := data.Status
		if status == "all" {
			status = ""
		}
		data.Requests, err = h.Moderation.Requests(status, adminListLimit)
	case "log":
		data.Log, err = h.Moderation.LogEntries(data.Action, data.Q, adminListLimit)
	default:
		data.Tab = "reports"
		if data.Status == "" {
			data.Status = "open"
		}
		status := data.Status
		if status == "all" {
			status = ""
		}
		if data.Type != "post" && data.Type != "comment" {
			data.Type = ""
		}
		data.Reports, err = h.Moderation.Reports(status, data.Type, adminListLimit)
	}
	if err != nil {
		logger.Error("admin: "+data.Tab, err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	if data.OpenReports, err = h.Moderation.OpenReportCount(); err != nil {
		logger.Error("admin: open reports", err)
	}
	if data.PendingRequests, err = h.Moderation.PendingRequestCount(); err != nil {
		logger.Error("admin: pending requests", err)
	}
	h.render(w, "admin.html", data, http.StatusOK)
}
