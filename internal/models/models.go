package models

import (
	"fmt"
	"forum/internal/markdown"
	"html/template"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type User struct {
	ID           int
	Email        string
	Username     string
	PasswordHash string
	Avatar       string
	Bio          string
	CoverImage   string // file in uploads/covers, empty when a gradient is used
	CoverPreset  string // gradient name, see CoverPresets
	Reputation   int
	Role         string     // "user", "moderator" or "admin"
	BannedUntil  *time.Time // blocked until this moment
	BanReason    string
	// Admin list only
	PostCount    int
	CommentCount int
	CreatedAt    time.Time
}

type Session struct {
	ID        string
	UserID    int
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Category struct {
	ID    int
	Name  string
	Count int // number of posts, filled by ListWithCounts
}

type Post struct {
	ID        int
	UserID    int
	Title     string
	Content   string
	CreatedAt time.Time
}

type Comment struct {
	ID        int
	PostID    int
	UserID    int
	Content   string
	CreatedAt time.Time
}

type PostSummary struct {
	ID           int
	Title        string
	Content      string
	AuthorID     int
	AuthorName   string
	AuthorAvatar string
	AuthorRep    int
	CreatedAt    time.Time
	UpdatedAt    *time.Time
	Categories   []string
	Likes        int
	Dislikes     int
	CommentCount int
	ImagePath    string // file in uploads/posts, empty if none
	MyReaction   int    // the viewer's reaction: 1, -1 or 0
	Bookmarked   bool   // the viewer saved this post
	HasPoll      bool
	Solved       bool          // has a best answer
	IsHidden     bool          // hidden by a moderator
	HTML         template.HTML // rendered Markdown, on the post page

	// Search results: title and text with matches wrapped in <mark>.
	TitleHL   template.HTML
	ContentHL template.HTML
}

type CommentView struct {
	ID              int
	PostID          int
	UserID          int
	ParentID        int // 0 for top-level comments
	AuthorName      string
	AuthorAvatar    string
	AuthorRep       int
	IsBest          bool
	Content         string
	CreatedAt       time.Time
	UpdatedAt       *time.Time
	IsDeleted       bool // shown as «Комментарий удалён» to keep the thread
	IsHidden        bool // hidden by a moderator
	HiddenForViewer bool // hidden and the viewer may not see it
	Likes           int
	Dislikes        int

	// Filled by the handler when building the thread
	HTML       template.HTML // rendered Markdown
	Reactions  []ReactionSummary
	Depth      int // 1 for top-level
	Replies    []CommentView
	ReplyCount int // all replies below, at any depth
}

func (c CommentView) Ago() string { return TimeAgo(c.CreatedAt, time.Now()) }

// RepliesLabel is the collapsed-thread button text, e.g. «Показать 3 ответа».
func (c CommentView) RepliesLabel() string {
	return fmt.Sprintf("Показать %d %s", c.ReplyCount, plural(c.ReplyCount, "ответ", "ответа", "ответов"))
}

type PostPageData struct {
	CurrentUser  *User
	Best         *CommentView // pinned best answer when it's a reply
	CanMarkBest  bool         // the viewer may mark a best answer
	Poll         *Poll
	CommentTotal int // comments that aren't deleted placeholders
	Post         PostSummary
	Comments     []CommentView
	Error        string
	CSRFToken    string
	Page         int
	HasNext      bool
	PrevURL      string
	NextURL      string
}

type HomePageData struct {
	CurrentUser *User
	Posts       []PostSummary
	Categories  []Category
	TotalPosts  int
	SelectedCat string
	Filter      string // "my_posts" or "liked" (links from the profile)
	Sort        string
	SearchQuery string
	Error       string
	CSRFToken   string
	HasNext     bool

	// "all" or "following" (posts by followed authors, logged-in only)
	FeedTab      string
	FollowsCount int          // how many users the viewer follows
	FollowingSet map[int]bool // IDs the viewer follows, for follow buttons
	Suggestions  []AuthorStat // «Кого почитать» when the viewer follows no one

	// Right-column widgets
	Popular []PostSummary
	Authors []AuthorStat
	TopRep  []AuthorStat
	Stats   ForumStats
}

// FeedTabURL links to «Все посты» / «Подписки», keeping category and search.
func (d HomePageData) FeedTabURL(tab string) string {
	d.FeedTab = tab
	if tab == "following" {
		d.Sort = "new"
	}
	return d.homeURL(d.SelectedCat, d.Sort)
}

// Feed sort orders, in the order of the switcher on the home page.
var FeedSorts = []struct{ Key, Label string }{
	{"new", "Новые"},
	{"top", "Популярные"},
	{"discussed", "Обсуждаемые"},
}

func IsFeedSort(s string) bool {
	for _, o := range FeedSorts {
		if o.Key == s {
			return true
		}
	}
	return false
}

func (HomePageData) Sorts() []struct{ Key, Label string } { return FeedSorts }

// homeURL builds a home page link that keeps the current search, sort and
// filter while changing the category.
func (d HomePageData) homeURL(category, sort string) string {
	v := url.Values{}
	if category != "" {
		v.Set("category", category)
	}
	if d.SearchQuery != "" {
		v.Set("q", d.SearchQuery)
	}
	if sort != "" && sort != "new" {
		v.Set("sort", sort)
	}
	if d.Filter != "" {
		v.Set("filter", d.Filter)
	}
	if d.FeedTab == "following" {
		v.Set("feed", "following")
	}
	if len(v) == 0 {
		return "/"
	}
	return "/?" + v.Encode()
}

func (d HomePageData) CategoryURL(category string) string { return d.homeURL(category, d.Sort) }
func (d HomePageData) SortURL(sort string) string         { return d.homeURL(d.SelectedCat, sort) }

// ClearFilterURL drops the my_posts/liked filter.
func (d HomePageData) ClearFilterURL() string {
	d.Filter = ""
	return d.homeURL(d.SelectedCat, d.Sort)
}

func (d HomePageData) FilterLabel() string {
	switch d.Filter {
	case "my_posts":
		return "Мои посты"
	case "liked":
		return "Понравившиеся"
	}
	return ""
}

type AuthorStat struct {
	ID         int
	Username   string
	Avatar     string
	Posts      int
	Reputation int
}

func (a AuthorStat) AvatarColor() string { return avatarColor(a.ID) }
func (a AuthorStat) Initial() string     { return initial(a.Username) }

type ForumStats struct {
	Users    int
	Posts    int
	Comments int
}

type AuthPageData struct {
	CurrentUser *User
	Error       string
	CSRFToken   string
}

type CreatePostPageData struct {
	CurrentUser *User
	Poll        PollForm
	Categories  []Category
	Error       string
	Title       string
	Content     string
	SelectedIDs map[int]bool
	CSRFToken   string
}

type EditPostPageData struct {
	CurrentUser *User
	ImagePath   string
	PostID      int
	Title       string
	Content     string
	Error       string
	Categories  []Category
	SelectedIDs map[int]bool
	CSRFToken   string
}

type UserPhoto struct {
	ID        int
	UserID    int
	FilePath  string
	CreatedAt time.Time
	IsCurrent bool
}

type UserComment struct {
	ID        int
	PostID    int
	PostTitle string
	Content   string
	CreatedAt time.Time
}

func (c UserComment) Plain() string { return markdown.PlainText(c.Content) }

type ProfilePageData struct {
	CurrentUser   *User // the viewer (nil for guests)
	Profile       *User // the user whose profile is shown
	IsOwner       bool
	PostCount     int
	CommentCount  int
	LikesReceived int
	Photos        []UserPhoto
	Tab           string
	Posts         []PostSummary
	MorePosts     bool
	Comments      []UserComment
	LikedPosts    []PostSummary
	MoreLiked     bool
	SavedPosts    []PostSummary // bookmarks, owner only
	MoreSaved     bool
	TopCategories []CategoryCount
	Activity      []Activity
	Followers     int
	Following     int
	ModApplied    bool // the owner's moderator application is pending
	IsFollowing   bool // the viewer follows this profile
	Achievements  []UserAchievement
	EarnedCount   int
	CSRFToken     string
	Error         string
	Success       string
}

// Fallback avatar backgrounds, picked by user ID so each user keeps a stable color.
var avatarColors = []string{"#4361ee", "#7c3aed", "#db2777", "#ea580c", "#0d9488", "#0284c7", "#65a30d", "#c026d3"}

func avatarColor(userID int) string {
	if userID < 0 {
		userID = -userID
	}
	return avatarColors[userID%len(avatarColors)]
}

func initial(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// Template helpers for the letter shown when a user has no photo.
func (u User) AvatarColor() string          { return avatarColor(u.ID) }
func (u User) Initial() string              { return initial(u.Username) }
func (p PostSummary) AuthorColor() string   { return avatarColor(p.AuthorID) }
func (p PostSummary) AuthorInitial() string { return initial(p.AuthorName) }

// Ago is the post time relative to now, e.g. «5 минут назад».
func (p PostSummary) Ago() string { return TimeAgo(p.CreatedAt, time.Now()) }

// Plain is the post text without Markdown syntax, for previews.
func (p PostSummary) Plain() string { return markdown.PlainText(p.Content) }

// IsLong reports whether the preview is likely clamped, to show «Читать дальше».
func (p PostSummary) IsLong() bool {
	plain := p.Plain()
	return utf8.RuneCountInString(plain) > 220 || strings.Count(plain, "\n") >= 3
}

// Excerpt is the start of the post text for previews.
func (p PostSummary) Excerpt() string { return excerpt(p.Plain(), 150) }

func excerpt(s string, n int) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= n {
		return string(rs)
	}
	return strings.TrimSpace(string(rs[:n])) + "…"
}
func (c CommentView) AuthorColor() string   { return avatarColor(c.UserID) }
func (c CommentView) AuthorInitial() string { return initial(c.AuthorName) }

// ─── Profile cover ───────────────────────────────────────────

type CoverPreset struct {
	Name  string
	Label string
}

// CoverPresets are the built-in cover gradients; each has a .cover-<Name> CSS class.
var CoverPresets = []CoverPreset{
	{"blue-violet", "Сине-фиолетовый"},
	{"sunset", "Закат"},
	{"mint", "Мятный"},
	{"night", "Ночной"},
	{"pink-violet", "Розово-фиолетовый"},
	{"graphite", "Графитовый"},
}

const DefaultCoverPreset = "blue-violet"

func IsCoverPreset(name string) bool {
	for _, p := range CoverPresets {
		if p.Name == name {
			return true
		}
	}
	return false
}

// CoverClass is the CSS class for the user's cover gradient.
func (u User) CoverClass() string {
	if IsCoverPreset(u.CoverPreset) {
		return "cover-" + u.CoverPreset
	}
	return "cover-" + DefaultCoverPreset
}

func (ProfilePageData) CoverPresets() []CoverPreset { return CoverPresets }

// ─── Profile sidebar ─────────────────────────────────────────

type CategoryCount struct {
	Name  string
	Count int
}

// Activity is one entry of a user's recent activity.
type Activity struct {
	Kind      string // "post", "comment" or "like"
	PostID    int
	PostTitle string
	At        time.Time
}

func (a Activity) Verb() string {
	switch a.Kind {
	case "post":
		return "Создал(а) пост"
	case "comment":
		return "Оставил(а) комментарий к посту"
	default:
		return "Понравился пост"
	}
}

func (a Activity) Ago() string { return TimeAgo(a.At, time.Now()) }

// TimeAgo formats t relative to now in Russian: «5 минут назад», «вчера».
func TimeAgo(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "только что"
	case d < time.Hour:
		n := int(d.Minutes())
		return fmt.Sprintf("%d %s назад", n, plural(n, "минуту", "минуты", "минут"))
	case d < 24*time.Hour:
		n := int(d.Hours())
		return fmt.Sprintf("%d %s назад", n, plural(n, "час", "часа", "часов"))
	case d < 48*time.Hour:
		return "вчера"
	case d < 30*24*time.Hour:
		n := int(d.Hours() / 24)
		return fmt.Sprintf("%d %s назад", n, plural(n, "день", "дня", "дней"))
	default:
		return t.Local().Format("02.01.2006")
	}
}

// plural picks the Russian word form for n: 1 минуту, 2 минуты, 5 минут.
func plural(n int, one, few, many string) string {
	n %= 100
	if n >= 11 && n <= 14 {
		return many
	}
	switch n % 10 {
	case 1:
		return one
	case 2, 3, 4:
		return few
	}
	return many
}

// TabURL links to a profile tab, keeping the profile id for other users.
func (d ProfilePageData) TabURL(tab string) string {
	if d.IsOwner || d.Profile == nil {
		return "/profile?tab=" + tab
	}
	return fmt.Sprintf("/user/%d?tab=%s", d.Profile.ID, tab)
}

func (d ProfilePageData) FollowersText() string {
	s, _ := FollowersLabel(d.Followers, d.Following)
	return s
}
func (d ProfilePageData) FollowingText() string {
	_, s := FollowersLabel(d.Followers, d.Following)
	return s
}

// Highlight HTML-escapes text and wraps case-insensitive matches of query
// in <mark>. Matching is done on runes so Cyrillic works too.
func Highlight(text, query string) template.HTML {
	q := []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(q) == 0 {
		return template.HTML(template.HTMLEscapeString(text))
	}
	src := []rune(text)
	lower := make([]rune, len(src))
	for i, r := range src {
		lower[i] = unicode.ToLower(r)
	}

	var b strings.Builder
	last := 0
	for i := 0; i+len(q) <= len(lower); {
		if string(lower[i:i+len(q)]) == string(q) {
			b.WriteString(template.HTMLEscapeString(string(src[last:i])))
			b.WriteString("<mark>")
			b.WriteString(template.HTMLEscapeString(string(src[i : i+len(q)])))
			b.WriteString("</mark>")
			i += len(q)
			last = i
			continue
		}
		i++
	}
	b.WriteString(template.HTMLEscapeString(string(src[last:])))
	return template.HTML(b.String())
}

// ─── Follows & notifications ────────────────────────────────

// FollowUser is a row in the followers/following list.
type FollowUser struct {
	ID       int
	Username string
	Avatar   string
	Bio      string
}

func (u FollowUser) AvatarColor() string { return avatarColor(u.ID) }
func (u FollowUser) Initial() string     { return initial(u.Username) }

type Notification struct {
	ID          int
	Type        string // "follow", "post", "comment", "like"
	ActorID     int
	ActorName   string
	ActorAvatar string
	PostID      int
	PostTitle   string
	CommentID   int
	Achievement string
	IsRead      bool
	CreatedAt   time.Time
}

func (n Notification) AvatarColor() string { return avatarColor(n.ActorID) }
func (n Notification) Initial() string     { return initial(n.ActorName) }
func (n Notification) Ago() string         { return TimeAgo(n.CreatedAt, time.Now()) }

// Text is the message after the actor's name.
func (n Notification) Text() string {
	switch n.Type {
	case "follow":
		return "подписался(-ась) на вас"
	case "post":
		return "опубликовал(а) пост"
	case "comment":
		return "прокомментировал(а) ваш пост"
	case "like":
		return "оценил(а) ваш пост"
	case "reply":
		return "ответил(а) на ваш комментарий к посту"
	case "mention":
		return "упомянул(а) вас в посте"
	case "best":
		return "отметил(а) ваш комментарий как лучший ответ к посту"
	case "achievement":
		if a, ok := AchievementByCode(n.Achievement); ok {
			return "Новое достижение: «" + a.Title + "»"
		}
	}
	return ""
}

// URL is where clicking the notification leads.
func (n Notification) URL() string {
	if n.Type == "achievement" {
		return "/profile#achievements"
	}
	if n.Type == "follow" || n.PostID == 0 {
		return fmt.Sprintf("/user/%d", n.ActorID)
	}
	if n.CommentID > 0 {
		return fmt.Sprintf("/post?id=%d&comment=%d#comment-%d", n.PostID, n.CommentID, n.CommentID)
	}
	return fmt.Sprintf("/post?id=%d", n.PostID)
}

// FollowersLabel formats «N подписчиков · M подписок» with Russian plurals.
func FollowersLabel(followers, following int) (string, string) {
	return fmt.Sprintf("%d %s", followers, plural(followers, "подписчик", "подписчика", "подписчиков")),
		fmt.Sprintf("%d %s", following, plural(following, "подписка", "подписки", "подписок"))
}

// ─── Private messages ───────────────────────────────────────

type Message struct {
	ID             int       `json:"id"`
	ConversationID int       `json:"conversation_id"`
	SenderID       int       `json:"sender_id"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"created_at"`
	IsRead         bool      `json:"is_read"`
	IsDelivered    bool      `json:"is_delivered"`
}

// Conversation is a row in the dialog list.
type Conversation struct {
	ID              int
	PartnerID       int
	PartnerName     string
	PartnerAvatar   string
	PartnerLastSeen *time.Time
	PartnerOnline   bool
	LastText        string
	LastSenderID    int
	LastAt          time.Time
	Unread          int
}

func (c Conversation) AvatarColor() string { return avatarColor(c.PartnerID) }
func (c Conversation) Initial() string     { return initial(c.PartnerName) }

// LastTime is a short time for the list: «14:05», «вчера» or «03.05».
func (c Conversation) LastTime() string { return ShortTime(c.LastAt, time.Now()) }

func ShortTime(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return t.Format("15:04")
	case now.Sub(t) < 48*time.Hour && now.AddDate(0, 0, -1).Day() == d1:
		return "вчера"
	case y1 == y2:
		return t.Format("02.01")
	}
	return t.Format("02.01.06")
}

// LastSeenText is «в сети» or «был(а) в сети 5 минут назад».
func LastSeenText(online bool, seen *time.Time) string {
	switch {
	case online:
		return "в сети"
	case seen == nil:
		return "не в сети"
	}
	return "был(а) в сети " + TimeAgo(*seen, time.Now())
}

// ChatPartner is the person in the open chat.
type ChatPartner struct {
	ID       int
	Username string
	Avatar   string
	Online   bool
	LastSeen *time.Time
}

func (p ChatPartner) AvatarColor() string { return avatarColor(p.ID) }
func (p ChatPartner) Initial() string     { return initial(p.Username) }
func (p ChatPartner) Status() string      { return LastSeenText(p.Online, p.LastSeen) }

type MessagesPageData struct {
	CurrentUser   *User
	CSRFToken     string
	Conversations []Conversation
	ActiveID      int          // open conversation, 0 for a new one
	Partner       *ChatPartner // nil when no chat is open
	Error         string
}

// ─── Comment reactions ──────────────────────────────────────

// CommentReactionKinds are the six reactions, in display order.
var CommentReactionKinds = []struct{ Kind, Label string }{
	{"like", "Нравится"}, {"heart", "Сердце"}, {"laugh", "Смешно"},
	{"wow", "Удивительно"}, {"sad", "Грустно"}, {"fire", "Огонь"},
}

func IsCommentReaction(kind string) bool {
	for _, k := range CommentReactionKinds {
		if k.Kind == kind {
			return true
		}
	}
	return false
}

type ReactionSummary struct {
	Kind  string   `json:"kind"`
	Label string   `json:"label"`
	Count int      `json:"count"`
	Mine  bool     `json:"mine"`
	Users []string `json:"users"` // who reacted, for the tooltip (first few)
}

// Who is the tooltip text: «alice, bob и ещё 3».
func (r ReactionSummary) Who() string {
	s := strings.Join(r.Users, ", ")
	if extra := r.Count - len(r.Users); extra > 0 {
		s += fmt.Sprintf(" и ещё %d", extra)
	}
	return r.Label + ": " + s
}

// ─── Polls ──────────────────────────────────────────────────

type PollOption struct {
	ID      int
	Text    string
	Votes   int
	Percent int
	Mine    bool
}

type Poll struct {
	ID       int
	PostID   int
	Question string
	Multiple bool
	EndsAt   *time.Time
	Options  []PollOption
	Voters   int  // people who voted
	Voted    bool // the viewer has voted
	Ended    bool
}

// ShowResults is true once the viewer voted or the poll is over.
func (p Poll) ShowResults() bool { return p.Voted || p.Ended }

func (p Poll) VotersText() string {
	return fmt.Sprintf("%d %s", p.Voters, plural(p.Voters, "голос", "голоса", "голосов"))
}

func (p Poll) EndsText() string {
	switch {
	case p.EndsAt == nil:
		return ""
	case p.Ended:
		return "Опрос завершён"
	}
	return "До " + p.EndsAt.Local().Format("02.01.2006 15:04")
}

// PollForm holds the poll fields of the create-post form.
type PollForm struct {
	Enabled  bool
	Question string
	Options  []string
	Multiple bool
	Ends     string
}

// OptionList returns the options to show: at least two inputs.
func (f PollForm) OptionList() []string {
	if len(f.Options) >= 2 {
		return f.Options
	}
	return []string{"", ""}
}

// ─── Reputation levels ──────────────────────────────────────

type levelDef struct {
	Key  string // CSS class suffix: .lvl-<Key>
	Name string
	Min  int
}

var levels = []levelDef{
	{"newbie", "Новичок", 0},
	{"member", "Участник", 50},
	{"expert", "Знаток", 200},
	{"pro", "Эксперт", 500},
	{"guru", "Гуру", 1000},
}

// Level describes a reputation score: its level and the way to the next one.
type Level struct {
	Key      string
	Name     string
	Rep      int
	NextName string // empty at the top level
	ToNext   int    // points left to the next level
	Progress int    // 0–100 within the current level
}

func LevelFor(rep int) Level {
	i := 0
	for j, l := range levels {
		if rep >= l.Min {
			i = j
		}
	}
	lv := Level{Key: levels[i].Key, Name: levels[i].Name, Rep: rep, Progress: 100}
	if i+1 < len(levels) {
		next := levels[i+1]
		lv.NextName = next.Name
		lv.ToNext = next.Min - rep
		span := next.Min - levels[i].Min
		lv.Progress = max(0, min(100, (rep-levels[i].Min)*100/span))
	}
	return lv
}

func (l Level) ToNextText() string {
	return fmt.Sprintf("%d %s", l.ToNext, plural(l.ToNext, "очко", "очка", "очков"))
}

func (l Level) RepText() string {
	return fmt.Sprintf("%d %s", l.Rep, plural(abs(l.Rep), "очко", "очка", "очков"))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (u User) Level() Level              { return LevelFor(u.Reputation) }
func (p PostSummary) AuthorLevel() Level { return LevelFor(p.AuthorRep) }
func (c CommentView) AuthorLevel() Level { return LevelFor(c.AuthorRep) }
func (a AuthorStat) Level() Level        { return LevelFor(a.Reputation) }

// ─── Achievements ───────────────────────────────────────────

type Achievement struct {
	Code        string
	Title       string
	Description string // how to get it
}

// Achievements is the catalogue, in display order. The icons are SVG
// templates named "ach-<Code>".
var Achievements = []Achievement{
	{"first_post", "Первый пост", "Опубликуйте свой первый пост"},
	{"first_comment", "Первый комментарий", "Оставьте первый комментарий"},
	{"posts_10", "10 постов", "Опубликуйте 10 постов"},
	{"comments_50", "50 комментариев", "Оставьте 50 комментариев"},
	{"followers_10", "10 подписчиков", "Наберите 10 подписчиков"},
	{"likes_100", "100 лайков", "Получите 100 лайков на постах и комментариях"},
	{"best_answer", "Лучший ответ", "Ваш комментарий отметили лучшим ответом"},
	{"week", "Неделя на форуме", "Будьте с нами 7 дней с момента регистрации"},
	{"night_owl", "Полуночник", "Опубликуйте пост между 00:00 и 05:00"},
}

func AchievementByCode(code string) (Achievement, bool) {
	for _, a := range Achievements {
		if a.Code == code {
			return a, true
		}
	}
	return Achievement{}, false
}

// UserAchievement is an achievement as shown on a profile.
type UserAchievement struct {
	Achievement
	Earned   bool
	EarnedAt *time.Time
}

func (a UserAchievement) Tooltip() string {
	if a.Earned && a.EarnedAt != nil {
		return a.Title + " — получено " + a.EarnedAt.Local().Format("02.01.2006")
	}
	return a.Title + " — " + a.Description
}

// ─── Roles and bans ─────────────────────────────────────────

// PermanentBan is the «until» of a ban without an end.
var PermanentBan = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

func (u User) IsModerator() bool { return u.Role == "moderator" || u.Role == "admin" }
func (u User) IsAdmin() bool     { return u.Role == "admin" }

func (u User) IsBanned() bool { return u.BannedUntil != nil && u.BannedUntil.After(time.Now()) }

// BanText describes the ban for the user: reason and term.
func (u User) BanText() string {
	if !u.IsBanned() {
		return ""
	}
	term := "навсегда"
	if u.BannedUntil.Year() < 9999 {
		term = "до " + u.BannedUntil.Local().Format("02.01.2006 15:04")
	}
	reason := u.BanReason
	if reason == "" {
		reason = "не указана"
	}
	return "Аккаунт заблокирован " + term + ". Причина: " + reason
}

func (u User) RoleLabel() string {
	switch u.Role {
	case "admin":
		return "Админ"
	case "moderator":
		return "Модератор"
	}
	return "Пользователь"
}

// ─── Moderation ─────────────────────────────────────────────

// ReportReasons are the reasons a user can pick when reporting content.
var ReportReasons = []struct{ Key, Label string }{
	{"spam", "Спам"},
	{"abuse", "Оскорбления"},
	{"offtopic", "Не по теме"},
	{"other", "Другое"},
}

func IsReportReason(r string) bool {
	for _, x := range ReportReasons {
		if x.Key == r {
			return true
		}
	}
	return false
}

type Report struct {
	ID           int
	ReporterID   int
	ReporterName string
	TargetType   string // "post" or "comment"
	TargetID     int
	Reason       string
	Details      string
	Status       string // "open", "resolved", "dismissed"
	CreatedAt    time.Time
	Excerpt      string // post title or comment text; empty if deleted
	PostID       int    // the post to open (0 if gone)
	Hidden       bool
	AuthorName   string
}

func (r Report) ReasonLabel() string {
	for _, x := range ReportReasons {
		if x.Key == r.Reason {
			return x.Label
		}
	}
	return r.Reason
}

func (r Report) StatusLabel() string {
	switch r.Status {
	case "open":
		return "Открыта"
	case "resolved":
		return "Принята"
	}
	return "Отклонена"
}

func (r Report) Short() string { return excerpt(markdown.PlainText(r.Excerpt), 120) }

func (r Report) URL() string {
	if r.TargetType == "comment" {
		return fmt.Sprintf("/post?id=%d&comment=%d#comment-%d", r.PostID, r.TargetID, r.TargetID)
	}
	return fmt.Sprintf("/post?id=%d", r.PostID)
}

func (r Report) Ago() string { return TimeAgo(r.CreatedAt, time.Now()) }

type Request struct {
	ID         int
	Type       string // "moderator" or "delete_user"
	UserID     int
	UserName   string
	TargetID   int
	TargetName string
	Text       string
	Status     string // "pending", "approved", "rejected"
	CreatedAt  time.Time
}

func (q Request) TypeLabel() string {
	if q.Type == "moderator" {
		return "Стать модератором"
	}
	return "Удалить пользователя"
}

func (q Request) StatusLabel() string {
	switch q.Status {
	case "pending":
		return "Ожидает"
	case "approved":
		return "Одобрена"
	}
	return "Отклонена"
}

func (q Request) Ago() string { return TimeAgo(q.CreatedAt, time.Now()) }

type LogEntry struct {
	ID         int
	ActorID    int
	ActorName  string
	Action     string
	TargetType string
	TargetID   int
	Details    string
	CreatedAt  time.Time
}

// LogActions are the actions written to the log, with their labels.
var LogActions = []struct{ Key, Label string }{
	{"hide_post", "Скрыл пост"}, {"unhide_post", "Вернул пост"}, {"delete_post", "Удалил пост"},
	{"hide_comment", "Скрыл комментарий"}, {"unhide_comment", "Вернул комментарий"}, {"delete_comment", "Удалил комментарий"},
	{"dismiss_report", "Отклонил жалобу"},
	{"ban_user", "Заблокировал"}, {"unban_user", "Разблокировал"}, {"set_role", "Изменил роль"},
	{"request_delete_user", "Запросил удаление"}, {"delete_user", "Удалил пользователя"},
	{"approve_request", "Одобрил заявку"}, {"reject_request", "Отклонил заявку"},
	{"create_category", "Создал категорию"}, {"rename_category", "Переименовал категорию"}, {"delete_category", "Удалил категорию"},
}

func (e LogEntry) ActionLabel() string {
	for _, a := range LogActions {
		if a.Key == e.Action {
			return a.Label
		}
	}
	return e.Action
}

func (e LogEntry) When() string { return e.CreatedAt.Local().Format("02.01.2006 15:04") }

// AdminPageData is /admin. Lists are filled only for the open tab.
type AdminPageData struct {
	CurrentUser *User
	CSRFToken   string
	Tab         string // reports, users, categories, requests, log

	Reports    []Report
	Users      []User
	Categories []Category
	Requests   []Request
	Log        []LogEntry

	// Filters, echoed back into the forms
	Q, Status, Type, Role, State, Action string

	OpenReports     int
	PendingRequests int
}

func (AdminPageData) ReportReasons() []struct{ Key, Label string } { return ReportReasons }
func (AdminPageData) LogActions() []struct{ Key, Label string }    { return LogActions }

// OAuthEnabled tells templates which «Войти через …» providers are
// configured (set once at startup by handlers.ConfigureOAuth).
var OAuthEnabled = map[string]bool{}

func (AuthPageData) GoogleEnabled() bool { return OAuthEnabled["google"] }
func (AuthPageData) GitHubEnabled() bool { return OAuthEnabled["github"] }
