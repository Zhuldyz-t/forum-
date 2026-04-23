package models

import "time"

type User struct {
	ID           int
	Email        string
	Username     string
	PasswordHash string
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
	CreatedAt    time.Time
	Categories   []string
	Likes        int
	Dislikes     int
	CommentCount int
}

type CommentView struct {
	ID         int
	PostID     int
	UserID     int
	AuthorName string
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
}

type HomePageData struct {
	CurrentUser *User
	Posts       []PostSummary
	Categories  []Category
	SelectedCat string
	Selected    string
	Error       string
}

type AuthPageData struct {
	CurrentUser *User
	Error       string
}

type CreatePostPageData struct {
	CurrentUser *User
	Categories  []Category
	Error       string
	Title       string
	Content     string
	SelectedIDs map[int]bool
}
