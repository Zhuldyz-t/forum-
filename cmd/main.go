package main

import (
	"context"
	"forum/internal/db"
	"forum/internal/handlers"
	"forum/internal/logger"
	"forum/internal/middleware"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	loc, err := time.LoadLocation("Asia/Almaty")
	if err != nil {
		log.Fatal("timezone: ", err)
	}
	time.Local = loc

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

	tmpl, err := template.ParseGlob("templates/*.html")
	if err != nil {
		log.Fatal(err)
	}

	h := handlers.NewHandler(database, tmpl)

	mux := http.NewServeMux()

	rl := middleware.NewRateLimiter(10, 20, tmpl)

	var handler http.Handler = mux
	handler = rl.Middleware(handler)
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
	mux.HandleFunc("/post", h.ViewPost)
	mux.HandleFunc("/post/create", h.CreatePost)
	mux.HandleFunc("/post/delete", h.DeletePost)
	mux.HandleFunc("/post/edit", h.EditPost)
	mux.HandleFunc("/comment/create", h.CreateComment)
	mux.HandleFunc("/comment/delete", h.DeleteComment)
	mux.HandleFunc("/posts/search", h.SearchPosts)
	mux.HandleFunc("/reaction/post", h.ReactPost)
	mux.HandleFunc("/reaction/comment", h.ReactComment)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: handler,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Println("server started on :" + port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
