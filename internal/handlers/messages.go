package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/realtime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const messageMaxLen = 2000 // characters

// wireRealtime connects the hub's callbacks to the message store.
func (h *Handler) wireRealtime() {
	h.Hub = realtime.NewHub()
	h.Hub.OnOnline = func(userID int) {
		if _, err := h.Messages.TouchLastSeen(userID); err != nil {
			logger.Error("ws: last seen", err)
		}
		// Messages sent while the user was away are delivered now.
		pending, err := h.Messages.DeliverPending(userID)
		if err != nil {
			logger.Error("ws: deliver pending", err)
		}
		for sender, ids := range pending {
			h.Hub.Send(sender, map[string]any{"type": "delivered", "ids": ids})
		}
		h.broadcastPresence(userID, true)
		// Time-based achievements («Неделя на форуме») are checked on visit.
		h.afterChange(userID)
	}
	h.Hub.OnOffline = func(userID int) {
		if _, err := h.Messages.TouchLastSeen(userID); err != nil {
			logger.Error("ws: last seen", err)
		}
		h.broadcastPresence(userID, false)
	}
	h.Hub.OnEvent = h.handleClientEvent
}

// broadcastPresence tells everyone who has a chat with the user whether they
// are online now.
func (h *Handler) broadcastPresence(userID int, online bool) {
	partners, err := h.Messages.Partners(userID)
	if err != nil {
		logger.Error("ws: partners", err)
		return
	}
	status := models.LastSeenText(online, nil)
	if !online {
		status = "был(а) в сети только что"
	}
	for _, p := range partners {
		h.Hub.Send(p, map[string]any{"type": "presence", "user_id": userID, "online": online, "status": status})
	}
}

// handleClientEvent handles what the browser sends over the socket: only
// «печатает…» notices. Everything that changes data goes through POST.
func (h *Handler) handleClientEvent(userID int, data []byte) {
	var ev struct {
		Type           string `json:"type"`
		ConversationID int    `json:"conversation_id"`
	}
	if json.Unmarshal(data, &ev) != nil || ev.Type != "typing" || ev.ConversationID <= 0 {
		return
	}
	partner, ok, err := h.Messages.Partner(ev.ConversationID, userID)
	if err != nil || !ok {
		return
	}
	h.Hub.Send(partner, map[string]any{"type": "typing", "conversation_id": ev.ConversationID, "user_id": userID})
}

// WebSocket upgrades a logged-in user's connection: GET /ws.
func (h *Handler) WebSocket(w http.ResponseWriter, r *http.Request) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	h.Hub.Serve(w, r, user.ID)
}

// MessagesPage renders /messages: the dialog list and, with ?c=ID or ?to=USER,
// an open chat.
func (h *Handler) MessagesPage(w http.ResponseWriter, r *http.Request) {
	user, ok := h.mustAuth(w, r)
	if !ok {
		return
	}
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	data := models.MessagesPageData{CurrentUser: user, CSRFToken: h.pageCSRFToken(w, r)}

	partnerID := 0
	if to := r.URL.Query().Get("to"); to != "" {
		id, err := parseID(to)
		if err != nil {
			h.NotFound(w, r)
			return
		}
		if id == user.ID {
			h.renderError(w, http.StatusBadRequest, "Так нельзя", "Нельзя написать самому себе.")
			return
		}
		conv, err := h.Messages.FindConversation(user.ID, id)
		if err != nil {
			logger.Error("messages: find", err)
			h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
			return
		}
		if conv > 0 {
			http.Redirect(w, r, "/messages?c="+strconv.Itoa(conv), http.StatusSeeOther)
			return
		}
		partnerID = id // new chat: the conversation is created with the first message
	} else if c := r.URL.Query().Get("c"); c != "" {
		id, err := parseID(c)
		if err == nil {
			partnerID, ok, err = h.Messages.Partner(id, user.ID)
		}
		if err != nil || !ok {
			h.renderError(w, http.StatusNotFound, "Диалог не найден", "Такого диалога нет или он не ваш.")
			return
		}
		data.ActiveID = id
	}

	if partnerID > 0 {
		p, err := h.Users.GetByID(partnerID)
		if err != nil {
			h.renderError(w, http.StatusNotFound, "Пользователь не найден", "Такого пользователя не существует.")
			return
		}
		var seen sql.NullTime
		h.DB.QueryRow(`SELECT last_seen_at FROM users WHERE id = ?`, p.ID).Scan(&seen) //nolint:errcheck
		data.Partner = &models.ChatPartner{ID: p.ID, Username: p.Username, Avatar: p.Avatar, Online: h.Hub.Online(p.ID)}
		if seen.Valid {
			data.Partner.LastSeen = &seen.Time
		}
	}

	list, err := h.loadConversations(user.ID)
	if err != nil {
		logger.Error("messages: list", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	data.Conversations = list
	h.render(w, "messages.html", data, http.StatusOK)
}

func (h *Handler) loadConversations(userID int) ([]models.Conversation, error) {
	list, err := h.Messages.List(userID)
	for i := range list {
		list[i].PartnerOnline = h.Hub.Online(list[i].PartnerID)
	}
	return list, err
}

// ConversationList re-renders the dialog list (after a message arrives in a
// chat that isn't in the list yet): GET /messages/conversations?active=ID.
func (h *Handler) ConversationList(w http.ResponseWriter, r *http.Request) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	list, err := h.loadConversations(user.ID)
	if err != nil {
		logger.Error("messages: list", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	active, _ := strconv.Atoi(r.URL.Query().Get("active"))
	var buf bytes.Buffer
	if err := h.Tmpl.ExecuteTemplate(&buf, "conversation-list", map[string]any{"Conversations": list, "ActiveID": active, "Me": user.ID}); err != nil {
		logger.Error("messages: render list", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": buf.String()})
}

// MessageHistory returns 30 messages before ?before=ID: GET /messages/history?c=ID.
func (h *Handler) MessageHistory(w http.ResponseWriter, r *http.Request) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	conv, err := parseID(r.URL.Query().Get("c"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	if _, ok, err := h.Messages.Partner(conv, user.ID); err != nil || !ok {
		jsonError(w, http.StatusNotFound, "Диалог не найден")
		return
	}
	before, _ := strconv.Atoi(r.URL.Query().Get("before"))
	msgs, more, err := h.Messages.History(conv, before)
	if err != nil {
		logger.Error("messages: history", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	if msgs == nil {
		msgs = []models.Message{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs, "has_more": more})
}

// SendMessage stores a message and pushes it to both sides: POST /messages/send
// with conversation_id, or to (user id) for a new chat.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 32<<10)
	if !ok {
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		jsonError(w, http.StatusBadRequest, "Сообщение пустое")
		return
	}
	if utf8.RuneCountInString(text) > messageMaxLen {
		jsonError(w, http.StatusBadRequest, "Сообщение длиннее 2000 символов")
		return
	}

	var conv, partner int
	if c := r.FormValue("conversation_id"); c != "" && c != "0" {
		id, err := parseID(c)
		if err == nil {
			partner, ok, err = h.Messages.Partner(id, user.ID)
		}
		if err != nil || !ok {
			jsonError(w, http.StatusNotFound, "Диалог не найден")
			return
		}
		conv = id
	} else {
		to, err := parseID(r.FormValue("to"))
		if err != nil {
			jsonError(w, http.StatusBadRequest, "Неверный получатель")
			return
		}
		if to == user.ID {
			jsonError(w, http.StatusBadRequest, "Нельзя написать самому себе")
			return
		}
		if _, err := h.Users.GetByID(to); err != nil {
			jsonError(w, http.StatusNotFound, "Пользователь не найден")
			return
		}
		if conv, err = h.Messages.GetOrCreateConversation(user.ID, to); err != nil {
			logger.Error("messages: create conversation", err)
			jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
			return
		}
		partner = to
	}

	msg, err := h.Messages.Send(conv, user.ID, text)
	if err != nil {
		logger.Error("messages: send", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	if h.Hub.Online(partner) {
		if err := h.Messages.MarkDelivered(msg.ID); err == nil {
			msg.IsDelivered = true
		}
	}

	event := map[string]any{"type": "message", "message": msg,
		"from": map[string]any{"id": user.ID, "username": user.Username}}
	h.Hub.Send(partner, event)
	h.Hub.Send(user.ID, event) // the sender's other tabs
	h.pushUnread(partner)
	writeJSON(w, http.StatusOK, map[string]any{"message": msg})
}

// MarkMessagesRead marks the open chat as read: POST /messages/read.
func (h *Handler) MarkMessagesRead(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	conv, err := parseID(r.FormValue("conversation_id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	partner, ok, err := h.Messages.Partner(conv, user.ID)
	if err != nil || !ok {
		jsonError(w, http.StatusNotFound, "Диалог не найден")
		return
	}
	upTo, err := h.Messages.MarkRead(conv, user.ID)
	if err != nil {
		logger.Error("messages: mark read", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	if upTo > 0 {
		h.Hub.Send(partner, map[string]any{"type": "read", "conversation_id": conv, "up_to": upTo})
	}
	h.pushUnread(user.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// UnreadMessages: GET /messages/unread → {"count": N} for the header badge.
func (h *Handler) UnreadMessages(w http.ResponseWriter, r *http.Request) {
	user, err := h.getCurrentUser(r)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	n, err := h.Messages.UnreadCount(user.ID)
	if err != nil {
		logger.Error("messages: unread", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": n})
}

// pushUnread sends the user's current unread-messages count to their tabs.
func (h *Handler) pushUnread(userID int) {
	n, err := h.Messages.UnreadCount(userID)
	if err != nil {
		logger.Error("messages: unread", err)
		return
	}
	h.Hub.Send(userID, map[string]any{"type": "unread_messages", "count": n})
}
