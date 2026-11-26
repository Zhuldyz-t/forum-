package handlers

import (
	"bytes"
	"errors"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/repository"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ReactCommentEmoji toggles one of the six reactions on a comment and returns
// the re-rendered reactions bar: POST /comment/react (comment_id, kind).
func (h *Handler) ReactCommentEmoji(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	id, err := parseID(r.FormValue("comment_id"))
	kind := r.FormValue("kind")
	if err != nil || !models.IsCommentReaction(kind) {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	ref, err := h.Comments.Ref(id)
	if err != nil || ref.IsDeleted {
		jsonError(w, http.StatusNotFound, "Комментарий не найден")
		return
	}
	if err := h.Reactions.ToggleCommentReaction(user.ID, id, kind); err != nil {
		logger.Error("comment reaction", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	h.afterChange(ref.UserID)
	sums, err := h.Reactions.CommentReactions([]int{id}, user.ID)
	if err != nil {
		logger.Error("comment reactions", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	var buf bytes.Buffer
	err = h.Tmpl.ExecuteTemplate(&buf, "comment-reactions", map[string]any{
		"ID": id, "Reactions": sums[id], "Page": models.PostPageData{CurrentUser: user, CSRFToken: r.FormValue("csrf_token")},
	})
	if err != nil {
		logger.Error("comment reactions: render", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": buf.String()})
}

// pollInput is the poll part of the create-post form.
type pollInput struct {
	Enabled  bool
	Question string
	Options  []string
	Multiple bool
	Ends     string // datetime-local value, as typed
	EndsAt   *time.Time
}

// parsePoll reads and validates the poll fields; it returns a user-facing
// error message, or "" when the poll is fine or not requested.
func parsePoll(r *http.Request) (pollInput, string) {
	p := pollInput{
		Enabled:  r.FormValue("poll_enabled") == "1",
		Question: strings.TrimSpace(r.FormValue("poll_question")),
		Multiple: r.FormValue("poll_multiple") == "1",
		Ends:     strings.TrimSpace(r.FormValue("poll_ends")),
	}
	for _, o := range r.Form["poll_option"] {
		p.Options = append(p.Options, strings.TrimSpace(o))
	}
	if !p.Enabled {
		return p, ""
	}
	if p.Question == "" || utf8.RuneCountInString(p.Question) > 200 {
		return p, "Вопрос опроса: от 1 до 200 символов"
	}
	var opts []string
	seen := map[string]bool{}
	for _, o := range p.Options {
		if o == "" {
			continue
		}
		if utf8.RuneCountInString(o) > 100 {
			return p, "Вариант ответа: не больше 100 символов"
		}
		key := strings.ToLower(o)
		if seen[key] {
			return p, "Варианты ответа не должны повторяться"
		}
		seen[key] = true
		opts = append(opts, o)
	}
	if len(opts) < 2 || len(opts) > 6 {
		return p, "В опросе должно быть от 2 до 6 вариантов"
	}
	p.Options = opts
	if p.Ends != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", p.Ends, time.Local)
		if err != nil {
			return p, "Неверная дата окончания опроса"
		}
		if !t.After(time.Now()) {
			return p, "Дата окончания опроса должна быть в будущем"
		}
		p.EndsAt = &t
	}
	return p, ""
}

// VotePoll records a vote and returns the poll with results: POST /poll/vote
// (poll_id, option_id…).
func (h *Handler) VotePoll(w http.ResponseWriter, r *http.Request) {
	user, ok := h.jsonAction(w, r, 0)
	if !ok {
		return
	}
	pollID, err := parseID(r.FormValue("poll_id"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Неверный запрос")
		return
	}
	var options []int
	for _, raw := range r.Form["option_id"] {
		if id, err := strconv.Atoi(raw); err == nil && id > 0 {
			options = append(options, id)
		}
	}
	status, msg := http.StatusOK, ""
	if err := h.Polls.Vote(pollID, user.ID, options); err != nil {
		switch {
		case errors.Is(err, repository.ErrAlreadyVoted):
			status, msg = http.StatusConflict, "Вы уже голосовали в этом опросе"
		case errors.Is(err, repository.ErrPollEnded):
			status, msg = http.StatusConflict, "Опрос уже завершён"
		case errors.Is(err, repository.ErrSingleChoice):
			status, msg = http.StatusBadRequest, "В этом опросе можно выбрать только один вариант"
		default:
			status, msg = http.StatusBadRequest, "Выберите вариант ответа"
		}
	}
	poll, err := h.Polls.ByID(pollID, user.ID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Опрос не найден")
		return
	}
	var buf bytes.Buffer
	if err := h.Tmpl.ExecuteTemplate(&buf, "poll", map[string]any{
		"Poll": poll, "Page": models.PostPageData{CurrentUser: user, CSRFToken: r.FormValue("csrf_token")},
	}); err != nil {
		logger.Error("poll: render", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера")
		return
	}
	writeJSON(w, status, map[string]any{"html": buf.String(), "error": msg})
}

// formData is what the create form needs to show the poll fields again.
func (p pollInput) formData() models.PollForm {
	opts := append([]string(nil), p.Options...)
	for len(opts) < 2 {
		opts = append(opts, "")
	}
	return models.PollForm{Enabled: p.Enabled, Question: p.Question, Options: opts, Multiple: p.Multiple, Ends: p.Ends}
}
