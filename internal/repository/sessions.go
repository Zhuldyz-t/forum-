package repository

import (
	"database/sql"
	"forum/internal/models"
	"time"
)

type SessionRepository struct{ DB *sql.DB }

func (r *SessionRepository) Replace(sessionID string, userID int, expiresAt time.Time) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`, sessionID, userID, expiresAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SessionRepository) GetByID(sessionID string) (*models.Session, error) {
	s := &models.Session{}
	err := r.DB.QueryRow(`SELECT id, user_id, expires_at, created_at FROM sessions WHERE id = ?`, sessionID).
		Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *SessionRepository) DeleteByID(sessionID string) error {
	_, err := r.DB.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

func (r *SessionRepository) DeleteExpired() error {
	_, err := r.DB.Exec(`DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP`)
	return err
}
