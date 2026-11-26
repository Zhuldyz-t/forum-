package repository

import "database/sql"

type ReactionRepository struct{ DB *sql.DB }

func (r *ReactionRepository) TogglePost(userID, postID, value int) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current int
	err = tx.QueryRow(`SELECT value FROM post_reactions WHERE user_id = ? AND post_id = ?`, userID, postID).Scan(&current)
	if err == sql.ErrNoRows {
		if _, err = tx.Exec(`INSERT INTO post_reactions (user_id, post_id, value, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`, userID, postID, value); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if current == value {
		_, err = tx.Exec(`DELETE FROM post_reactions WHERE user_id = ? AND post_id = ?`, userID, postID)
	} else {
		_, err = tx.Exec(`UPDATE post_reactions SET value = ?, created_at = CURRENT_TIMESTAMP WHERE user_id = ? AND post_id = ?`, value, userID, postID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ReactionRepository) ToggleComment(userID, commentID, value int) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current int
	err = tx.QueryRow(`SELECT value FROM comment_reactions WHERE user_id = ? AND comment_id = ?`, userID, commentID).Scan(&current)
	if err == sql.ErrNoRows {
		if _, err = tx.Exec(`INSERT INTO comment_reactions (user_id, comment_id, value) VALUES (?, ?, ?)`, userID, commentID, value); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if current == value {
		_, err = tx.Exec(`DELETE FROM comment_reactions WHERE user_id = ? AND comment_id = ?`, userID, commentID)
	} else {
		_, err = tx.Exec(`UPDATE comment_reactions SET value = ? WHERE user_id = ? AND comment_id = ?`, value, userID, commentID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
