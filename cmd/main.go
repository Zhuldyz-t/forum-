package main

import (
	"context"
	"database/sql"
	"errors"
	"forum/internal/db"
	"forum/internal/handlers"
	"forum/internal/logger"
	"forum/internal/middleware"
	"forum/internal/view"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	loc, err := time.LoadLocation("Asia/Almaty")
	if err != nil {
		log.Fatal("timezone: ", err)
	}
	time.Local = loc

	if err := chdirToProjectRoot(); err != nil {
		log.Fatal(err)
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "forum.db"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.RunMigrations(database); err != nil {
		log.Fatal(err)
	}

	if err := db.CreateIndexes(database); err != nil {
		log.Fatal(err)
	}

	// Console command: go run ./cmd make-admin user@example.com
	if len(os.Args) >= 2 && os.Args[1] == "make-admin" {
		if len(os.Args) != 3 {
			log.Fatal("usage: forum make-admin <email>")
		}
		promoteAdmin(database, os.Args[2], true)
		return
	}
	// ADMIN_EMAIL appoints the first admin on start (once the user exists).
	if email := os.Getenv("ADMIN_EMAIL"); email != "" {
		promoteAdmin(database, email, false)
	}

	certFile, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	useTLS := certFile != "" && keyFile != ""

	tmpl, err := view.ParseGlob("templates/*.html")
	if err != nil {
		log.Fatal(err)
	}

	h := handlers.NewHandler(database, tmpl)
	// Secure cookies over HTTPS, or behind a TLS-terminating proxy (COOKIE_SECURE=1).
	h.SecureCookies = useTLS || os.Getenv("COOKIE_SECURE") == "1"
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		scheme := "http"
		if useTLS {
			scheme = "https"
		}
		baseURL = scheme + "://localhost:" + port
	}
	h.ConfigureOAuth(baseURL, os.Getenv)

	mux := http.NewServeMux()

	rl := middleware.NewRateLimiter(middleware.DefaultLimits, h.CurrentUserID, tmpl)

	var handler http.Handler = mux
	handler = rl.Middleware(handler)
	handler = middleware.SecurityHeaders(useTLS)(handler)
	handler = middleware.RecoveryMiddleware(handler, tmpl)
	handler = logger.LoggingMiddleware(handler)

	// "/" is the catch-all in Go's mux — serve home for exact "/", 404 for everything else
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			h.NotFound(w, r)
			return
		}
		h.Home(w, r)
	})
	mux.HandleFunc("/register", h.Register)
	mux.HandleFunc("/login", h.Login)
	mux.HandleFunc("/logout", h.Logout)
	mux.HandleFunc("/profile", h.Profile)
	mux.HandleFunc("/profile/edit", h.EditProfile)
	mux.HandleFunc("/profile/photo/upload", h.UploadPhoto)
	mux.HandleFunc("/profile/photo/current", h.SetCurrentPhoto)
	mux.HandleFunc("/profile/photo/delete", h.DeletePhoto)
	mux.HandleFunc("/uploads/avatars/", h.Avatar)
	mux.HandleFunc("/uploads/covers/", h.CoverFile)
	mux.HandleFunc("/uploads/posts/", h.PostImage)
	mux.HandleFunc("/feed", h.Feed)
	mux.HandleFunc("/user/", h.UserRoutes)
	mux.HandleFunc("/follow", h.Follow)
	mux.HandleFunc("/unfollow", h.Unfollow)
	mux.HandleFunc("/notifications", h.NotificationsList)
	mux.HandleFunc("/notifications/count", h.NotificationsCount)
	mux.HandleFunc("/notifications/read", h.NotificationsRead)
	mux.HandleFunc("/profile/cover/upload", h.UploadCover)
	mux.HandleFunc("/profile/cover/preset", h.SetCoverPreset)
	mux.HandleFunc("/profile/cover/delete", h.DeleteCover)
	mux.HandleFunc("/post", h.ViewPost)
	mux.HandleFunc("/post/create", h.CreatePost)
	mux.HandleFunc("/post/delete", h.DeletePost)
	mux.HandleFunc("/post/edit", h.EditPost)
	mux.HandleFunc("/comment/create", h.CreateComment)
	mux.HandleFunc("/comment/delete", h.DeleteComment)
	mux.HandleFunc("/comment/edit", h.EditComment)
	mux.HandleFunc("/markdown/preview", h.MarkdownPreview)
	mux.HandleFunc("/users/search", h.UserSearch)
	mux.HandleFunc("/bookmark", h.ToggleBookmark)
	mux.HandleFunc("/ws", h.WebSocket)
	mux.HandleFunc("/messages", h.MessagesPage)
	mux.HandleFunc("/messages/conversations", h.ConversationList)
	mux.HandleFunc("/messages/history", h.MessageHistory)
	mux.HandleFunc("/messages/send", h.SendMessage)
	mux.HandleFunc("/messages/read", h.MarkMessagesRead)
	mux.HandleFunc("/messages/unread", h.UnreadMessages)
	mux.HandleFunc("/comment/react", h.ReactCommentEmoji)
	mux.HandleFunc("/poll/vote", h.VotePoll)
	mux.HandleFunc("/comment/best", h.ToggleBestAnswer)
	mux.HandleFunc("/achievements/toasts", h.AchievementToasts)
	mux.HandleFunc("/report", h.Report)
	mux.HandleFunc("/moderator/apply", h.ApplyModerator)
	mux.HandleFunc("/mod/post", h.ModeratePost)
	mux.HandleFunc("/mod/comment", h.ModerateComment)
	mux.HandleFunc("/mod/report/dismiss", h.DismissReport)
	mux.HandleFunc("/mod/user/request-delete", h.RequestUserDeletion)
	mux.HandleFunc("/admin", h.AdminPage)
	mux.HandleFunc("/auth/", h.OAuthRoutes)
	mux.HandleFunc("/admin/user/role", h.SetUserRole)
	mux.HandleFunc("/admin/user/ban", h.BanUser)
	mux.HandleFunc("/admin/user/unban", h.UnbanUser)
	mux.HandleFunc("/admin/user/delete", h.DeleteUser)
	mux.HandleFunc("/admin/request", h.DecideRequest)
	mux.HandleFunc("/admin/category", h.ManageCategory)
	mux.HandleFunc("/posts/search", h.SearchPosts)
	mux.HandleFunc("/reaction/post", h.ReactPost)
	mux.HandleFunc("/reaction/comment", h.ReactComment)
	mux.Handle("/static/", noCache(http.StripPrefix("/static/", http.FileServer(http.Dir("./static")))))

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
		// Timeouts against slow clients. Writes allow a 10 MB upload reply
		// over a slow link; WebSockets set their own deadlines after upgrade.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var err error
		if useTLS {
			log.Println("server started on https://localhost:" + port)
			err = srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			log.Println("server started on :" + port)
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-quit
	log.Println("shutting down server...")

	rl.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	if err := database.Close(); err != nil {
		log.Printf("database close error: %v", err)
	}
	log.Println("server stopped")
}

// noCache makes browsers revalidate static files on every load (a cheap 304
// when unchanged), so CSS/JS edits show up without a hard refresh.
func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// chdirToProjectRoot walks up from the working directory until it finds the
// templates folder, so relative paths (templates, static, forum.db) work no
// matter which directory the server is started from.
func chdirToProjectRoot() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "templates")); err == nil && info.IsDir() {
			return os.Chdir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return errors.New("templates directory not found in current or parent directories")
		}
		dir = parent
	}
}

// promoteAdmin makes the user with this email an admin. With strict set
// (the console command) a missing user is an error.
func promoteAdmin(database *sql.DB, email string, strict bool) {
	res, err := database.Exec(`UPDATE users SET role = 'admin' WHERE email = ? COLLATE NOCASE`, email)
	if err != nil {
		log.Fatal("make admin: ", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if strict {
			log.Fatalf("make admin: no user with email %s — register first", email)
		}
		log.Printf("ADMIN_EMAIL: no user with email %s yet; register and restart", email)
		return
	}
	log.Printf("%s is now an admin", email)
}
