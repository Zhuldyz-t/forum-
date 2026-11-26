package handlers

import (
	"bytes"
	"forum/internal/logger"
	"forum/internal/models"
	"forum/internal/repository"
	"net/http"
	"strings"
	"time"
)

const feedPageSize = 10

// feedQuery reads the feed parameters shared by the home page and /feed.
type feedQuery struct {
	Category string
	Query    string
	Sort     string
	Filter   string
	Tab      string // "all" or "following"
	Page     int
}

func parseFeedQuery(r *http.Request) feedQuery {
	q := r.URL.Query()
	f := feedQuery{
		Category: q.Get("category"),
		Query:    strings.TrimSpace(q.Get("q")),
		Sort:     q.Get("sort"),
		Filter:   q.Get("filter"),
		Tab:      q.Get("feed"),
		Page:     parsePage(q.Get("page")),
	}
	if f.Tab != "following" {
		f.Tab = "all"
	} else {
		f.Sort = "new" // the subscriptions feed is always by date
	}
	if len([]rune(f.Query)) > 100 {
		f.Query = string([]rune(f.Query)[:100])
	}
	if !models.IsFeedSort(f.Sort) {
		f.Sort = "new"
	}
	if f.Filter != "my_posts" && f.Filter != "liked" {
		f.Filter = ""
	}
	return f
}

// loadFeed returns one page of the feed, with search matches highlighted.
func (h *Handler) loadFeed(f feedQuery, user *models.User) ([]models.PostSummary, bool, error) {
	opts := repository.FeedOptions{
		Filter:   f.Filter,
		Category: f.Category,
		Query:    f.Query,
		Sort:     f.Sort,
		Limit:    feedPageSize,
		Offset:   (f.Page - 1) * feedPageSize,
	}
	if user != nil {
		opts.ViewerID = user.ID
		opts.UserID = user.ID
		opts.Following = f.Tab == "following"
	}
	posts, hasNext, err := h.Posts.Feed(opts)
	if err != nil {
		return nil, false, err
	}
	if f.Query != "" {
		for i := range posts {
			posts[i].TitleHL = models.Highlight(posts[i].Title, f.Query)
			posts[i].ContentHL = models.Highlight(posts[i].Plain(), f.Query)
		}
	}
	return posts, hasNext, nil
}

// pageCSRFToken reuses the token already in the browser, so fragments loaded
// by fetch() carry the same token as the forms already on the page.
func (h *Handler) pageCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie("csrf_token"); err == nil && c.Value != "" {
		return c.Value
	}
	token := h.generateCSRFToken()
	h.setCSRFCookie(w, token)
	return token
}

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	if !h.methodRequired(w, r, http.MethodGet) {
		return
	}
	user, _ := h.getCurrentUser(r)
	f := parseFeedQuery(r)
	f.Page = 1 // further pages come from /feed
	if (f.Filter != "" || f.Tab == "following") && user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	posts, hasNext, err := h.loadFeed(f, user)
	if err != nil {
		logger.Error("Posts.Feed", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}
	categories, err := h.Categories.ListWithCounts()
	if err != nil {
		logger.Error("Categories.ListWithCounts", err)
		h.renderError(w, http.StatusInternalServerError, "Ошибка сервера", "Попробуй позже.")
		return
	}

	data := models.HomePageData{
		CurrentUser: user,
		Posts:       posts,
		Categories:  categories,
		SelectedCat: f.Category,
		Filter:      f.Filter,
		Sort:        f.Sort,
		SearchQuery: f.Query,
		CSRFToken:   h.pageCSRFToken(w, r),
		HasNext:     hasNext,
		FeedTab:     f.Tab,
	}
	h.fillFollowData(&data, user)
	// Widgets are decoration: log failures and render the page anyway.
	if data.Stats, err = h.Posts.Stats(); err != nil {
		logger.Error("Posts.Stats", err)
	}
	data.TotalPosts = data.Stats.Posts
	if data.Popular, err = h.Posts.PopularSince(time.Now().AddDate(0, 0, -7), 5); err != nil {
		logger.Error("Posts.PopularSince", err)
	}
	if data.Authors, err = h.Posts.TopAuthors(5); err != nil {
		logger.Error("Posts.TopAuthors", err)
	}
	if data.TopRep, err = h.Users.TopByReputation(5); err != nil {
		logger.Error("Users.TopByReputation", err)
	}
	h.render(w, "index.html", data, http.StatusOK)
}

// Feed returns a page of post cards as HTML for «Показать ещё» and live
// search: {"html": "...", "hasMore": bool}.
func (h *Handler) Feed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Метод не поддерживается")
		return
	}
	user, _ := h.getCurrentUser(r)
	f := parseFeedQuery(r)
	if (f.Filter != "" || f.Tab == "following") && user == nil {
		jsonError(w, http.StatusUnauthorized, "Войдите в аккаунт")
		return
	}
	posts, hasNext, err := h.loadFeed(f, user)
	if err != nil {
		logger.Error("Posts.Feed", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}

	page := models.HomePageData{CurrentUser: user, CSRFToken: h.pageCSRFToken(w, r), SearchQuery: f.Query, FeedTab: f.Tab}
	if f.Page == 1 {
		h.fillFollowData(&page, user)
	}
	var buf bytes.Buffer
	err = h.Tmpl.ExecuteTemplate(&buf, "feed-items", map[string]any{
		"Posts": posts, "Page": page, "First": f.Page == 1,
	})
	if err != nil {
		logger.Error("feed: render", err)
		jsonError(w, http.StatusInternalServerError, "Ошибка сервера, попробуйте позже")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"html": buf.String(), "hasMore": hasNext})
}

// fillFollowData adds who the viewer follows and, for an empty
// subscriptions feed, authors to suggest.
func (h *Handler) fillFollowData(d *models.HomePageData, user *models.User) {
	if user == nil {
		return
	}
	set, err := h.Follows.FollowingSet(user.ID)
	if err != nil {
		logger.Error("Follows.FollowingSet", err)
		return
	}
	d.FollowingSet = set
	d.FollowsCount = len(set)
	if d.FeedTab == "following" && d.FollowsCount == 0 {
		authors, err := h.Posts.TopAuthors(6)
		if err != nil {
			logger.Error("Posts.TopAuthors", err)
		}
		for _, a := range authors {
			if a.ID != user.ID && len(d.Suggestions) < 5 {
				d.Suggestions = append(d.Suggestions, a)
			}
		}
	}
}
