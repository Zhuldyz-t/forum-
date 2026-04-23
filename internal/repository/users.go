package repository

import (
	"database/sql"
	"errors"
	"forum/internal/models"
)

type UserRepository struct{ DB *sql.DB }

func (r *UserRepository) Create(email, username, passwordHash string) error {
	_, err := r.DB.Exec(`INSERT INTO users (email, username, password_hash) VALUES (?, ?, ?)`, email, username, passwordHash)
	return err
}

func (r *UserRepository) GetByEmail(email string) (*models.User, error) {
	user := &models.User{}
	err := r.DB.QueryRow(`SELECT id, email, username, password_hash, created_at FROM users WHERE email = ?`, email).
		Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) GetByID(id int) (*models.User, error) {
	user := &models.User{}
	err := r.DB.QueryRow(`SELECT id, email, username, password_hash, created_at FROM users WHERE id = ?`, id).
		Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) EmailExists(email string) (bool, error) {
	var exists int
	err := r.DB.QueryRow(`SELECT 1 FROM users WHERE email = ? LIMIT 1`, email).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *UserRepository) UsernameExists(username string) (bool, error) {
	var exists int
	err := r.DB.QueryRow(`SELECT 1 FROM users WHERE username = ? LIMIT 1`, username).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
