package repository

import "database/sql"

type BookmarkRepository struct{ DB *sql.DB }

// Toggle saves the post for the user, or removes it if already saved, and
// reports whether it is saved now.
func (r *BookmarkRepository) Toggle(userID, postID int) (bool, error) {
	res, err := r.DB.Exec(`INSERT OR IGNORE INTO bookmarks (user_id, post_id) VALUES (?, ?)`, userID, postID)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return n > 0, err
	}
	_, err = r.DB.Exec(`DELETE FROM bookmarks WHERE user_id = ? AND post_id = ?`, userID, postID)
	return false, err
}
