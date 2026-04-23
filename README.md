# Forum

A simple web forum built with Go, SQLite, HTML templates, cookies-based sessions, and Docker.

## Features

- User registration and login
- One active session per user with cookies
- Password hashing with bcrypt
- Create posts with one or more categories
- View all posts and post details
- Create comments
- Like and dislike posts
- Like and dislike comments
- Filter posts by category
- Filter posts by current user's created posts
- Filter posts by current user's liked posts
- Docker support

## Stack

- Go
- SQLite
- HTML templates
- CSS
- Docker

## Project Structure

```bash
forum/
├── cmd/
│   └── main.go
├── internal/
│   ├── db/
│   ├── handlers/
│   ├── models/
│   └── repository/
├── templates/
├── static/
├── Dockerfile
├── docker-compose.yml
├── go.mod
└── README.md
```

## Run locally

```bash
go mod tidy
go run ./cmd
```

Open:

```bash
http://localhost:8080
```

## Run with Docker

```bash
docker compose up --build
```

Then open:

```bash
http://localhost:8080
```

## Default categories

- Go
- SQL
- HTML
- Docker
- Backend
- Frontend
- SysAdmin

## Notes

- Sessions are stored in SQLite.
- Only authenticated users can create posts, comments, likes, and dislikes.
- Guests can only read posts and comments.
- The database file `forum.db` is created automatically on first launch.
