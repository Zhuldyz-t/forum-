package models

import "time"

type User struct {
	ID           int
	Email        string
	Username     string
	PasswordHash string
	Avatar       string
	CreatedAt    time.Time
}

type Session struct {
	ID        string
	UserID    int
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Category struct {
	ID   int
	Name string
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
	CreatedAt    time.Time
	UpdatedAt    *time.Time
	Categories   []string
	Likes        int
	Dislikes     int
	CommentCount int
}

type CommentView struct {
	ID         int
	PostID     int
	UserID     int
	AuthorName   string
	AuthorAvatar string
	Content    string
	CreatedAt  time.Time
	Likes      int
	Dislikes   int
}

type PostPageData struct {
	CurrentUser *User
	Post        PostSummary
	Comments    []CommentView
	Error       string
	CSRFToken   string
	Page        int
	HasNext     bool
	PrevURL     string
	NextURL     string
}

type HomePageData struct {
	CurrentUser *User
	Posts       []PostSummary
	Categories  []Category
	SelectedCat string
	Selected    string
	Error       string
	CSRFToken   string
	Page        int
	HasNext     bool
	PrevURL     string
	NextURL     string
	SearchQuery string
}

type AuthPageData struct {
	CurrentUser *User
	Error       string
	CSRFToken   string
}

type CreatePostPageData struct {
	CurrentUser *User
	Categories  []Category
	Error       string
	Title       string
	Content     string
	SelectedIDs map[int]bool
	CSRFToken   string
}

type EditPostPageData struct {
	CurrentUser *User
	PostID      int
	Title       string
	Content     string
	Error       string
	Categories  []Category
	SelectedIDs map[int]bool
	CSRFToken   string
}

type ProfilePageData struct {
	CurrentUser  *User
	PostCount    int
	CommentCount int
	CSRFToken    string
	Error        string
	Success      string
}
