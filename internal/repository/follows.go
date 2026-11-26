package repository

import (
	"database/sql"
	"forum/internal/models"
)

type FollowRepository struct{ DB *sql.DB }

// Follow subscribes follower to following. It reports whether a new
// subscription was created (false if it already existed).
func (r *FollowRepository) Follow(followerID, followingID int) (bool, error) {
	res, err := r.DB.Exec(`INSERT OR IGNORE INTO follows (follower_id, following_id) VALUES (?, ?)`, followerID, followingID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (r *FollowRepository) Unfollow(followerID, followingID int) error {
	_, err := r.DB.Exec(`DELETE FROM follows WHERE follower_id = ? AND following_id = ?`, followerID, followingID)
	return err
}

// Counts returns how many followers the user has and how many users they follow.
func (r *FollowRepository) Counts(userID int) (followers, following int, err error) {
	err = r.DB.QueryRow(`SELECT
		(SELECT COUNT(*) FROM follows WHERE following_id = ?),
		(SELECT COUNT(*) FROM follows WHERE follower_id = ?)`, userID, userID).Scan(&followers, &following)
	return
}

// FollowingSet returns the IDs of users that userID follows.
func (r *FollowRepository) FollowingSet(userID int) (map[int]bool, error) {
	set := map[int]bool{}
	if userID == 0 {
		return set, nil
	}
	rows, err := r.DB.Query(`SELECT following_id FROM follows WHERE follower_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}

// List returns the user's followers (followers=true) or the users they follow.
func (r *FollowRepository) List(userID int, followers bool, limit int) ([]models.FollowUser, error) {
	query := `SELECT u.id, u.username, u.avatar, u.bio FROM follows f JOIN users u ON u.id = f.follower_id
		WHERE f.following_id = ? ORDER BY f.created_at DESC LIMIT ?`
	if !followers {
		query = `SELECT u.id, u.username, u.avatar, u.bio FROM follows f JOIN users u ON u.id = f.following_id
		WHERE f.follower_id = ? ORDER BY f.created_at DESC LIMIT ?`
	}
	rows, err := r.DB.Query(query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.FollowUser
	for rows.Next() {
		var u models.FollowUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Avatar, &u.Bio); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
