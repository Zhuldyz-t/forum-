package main

import (
	"forum/internal/db"
	"forum/internal/handlers"
	"html/template"
	"log"
	"net/http"
)

func main() {
	database, err := db.InitDB("forum.db")
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	if err := db.RunMigrations(database); err != nil {
		log.Fatal(err)
	}

	tmpl, err := template.ParseGlob("templates/*.html")
	if err != nil {
		log.Fatal(err)
	}

	h := handlers.NewHandler(database, tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Home)
	mux.HandleFunc("/register", h.Register)
	mux.HandleFunc("/login", h.Login)
	mux.HandleFunc("/logout", h.Logout)
	mux.HandleFunc("/post", h.ViewPost)
	mux.HandleFunc("/post/create", h.CreatePost)
	mux.HandleFunc("/post/delete", h.DeletePost)
	mux.HandleFunc("/comment/create", h.CreateComment)
	mux.HandleFunc("/reaction/post", h.ReactPost)
	mux.HandleFunc("/reaction/comment", h.ReactComment)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	log.Println("server started on :8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}
