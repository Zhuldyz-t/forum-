package repository

import (
	"database/sql"
	"forum/internal/models"
)

// PhotoRepository manages a user's profile photo gallery. Every change also
// updates users.avatar, which mirrors the current photo's file name.
type PhotoRepository struct{ DB *sql.DB }

// ListByUser returns the user's photos, current first, then newest first.
func (r *PhotoRepository) ListByUser(userID int) ([]models.UserPhoto, error) {
	rows, err := r.DB.Query(`
SELECT id, user_id, file_path, created_at, is_current
FROM user_photos WHERE user_id = ?
ORDER BY is_current DESC, created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var photos []models.UserPhoto
	for rows.Next() {
		var p models.UserPhoto
		if err := rows.Scan(&p.ID, &p.UserID, &p.FilePath, &p.CreatedAt, &p.IsCurrent); err != nil {
			return nil, err
		}
		photos = append(photos, p)
	}
	return photos, rows.Err()
}

// Add stores a new photo and makes it the current one.
func (r *PhotoRepository) Add(userID int, filePath string) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`UPDATE user_photos SET is_current = 0 WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO user_photos (user_id, file_path, is_current) VALUES (?, ?, 1)`, userID, filePath); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, filePath, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetCurrent makes photoID the current photo. It returns sql.ErrNoRows if the
// photo doesn't exist or belongs to another user.
func (r *PhotoRepository) SetCurrent(userID, photoID int) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var filePath string
	if err := tx.QueryRow(`SELECT file_path FROM user_photos WHERE id = ? AND user_id = ?`, photoID, userID).Scan(&filePath); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE user_photos SET is_current = (id = ?) WHERE user_id = ?`, photoID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, filePath, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes photoID and returns its file name so the caller can delete
// the file. If it was the current photo, the newest remaining one takes its
// place (or the avatar is cleared). Returns sql.ErrNoRows if the photo
// doesn't exist or belongs to another user.
func (r *PhotoRepository) Delete(userID, photoID int) (string, error) {
	tx, err := r.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck

	var filePath string
	var isCurrent bool
	if err := tx.QueryRow(`SELECT file_path, is_current FROM user_photos WHERE id = ? AND user_id = ?`, photoID, userID).
		Scan(&filePath, &isCurrent); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM user_photos WHERE id = ?`, photoID); err != nil {
		return "", err
	}

	if isCurrent {
		var nextID int
		var nextPath string
		err := tx.QueryRow(`SELECT id, file_path FROM user_photos WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`, userID).
			Scan(&nextID, &nextPath)
		switch {
		case err == sql.ErrNoRows:
			nextPath = ""
		case err != nil:
			return "", err
		default:
			if _, err := tx.Exec(`UPDATE user_photos SET is_current = 1 WHERE id = ?`, nextID); err != nil {
				return "", err
			}
		}
		if _, err := tx.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, nextPath, userID); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return filePath, nil
}
