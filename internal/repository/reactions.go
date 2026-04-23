package repository

import (
	"database/sql"
	"errors"
)

type ReactionRepository struct{ DB *sql.DB }

func (r *ReactionRepository) TogglePost(userID, postID, value int) error {
	var current int
	err := r.DB.QueryRow(`SELECT value FROM post_reactions WHERE user_id = ? AND post_id = ?`, userID, postID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = r.DB.Exec(`INSERT INTO post_reactions (user_id, post_id, value) VALUES (?, ?, ?)`, userID, postID, value)
		return err
	}
	if err != nil {
		return err
	}
	if current == value {
		_, err = r.DB.Exec(`DELETE FROM post_reactions WHERE user_id = ? AND post_id = ?`, userID, postID)
		return err
	}
	_, err = r.DB.Exec(`UPDATE post_reactions SET value = ? WHERE user_id = ? AND post_id = ?`, value, userID, postID)
	return err
}

func (r *ReactionRepository) ToggleComment(userID, commentID, value int) error {
	var current int
	err := r.DB.QueryRow(`SELECT value FROM comment_reactions WHERE user_id = ? AND comment_id = ?`, userID, commentID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = r.DB.Exec(`INSERT INTO comment_reactions (user_id, comment_id, value) VALUES (?, ?, ?)`, userID, commentID, value)
		return err
	}
	if err != nil {
		return err
	}
	if current == value {
		_, err = r.DB.Exec(`DELETE FROM comment_reactions WHERE user_id = ? AND comment_id = ?`, userID, commentID)
		return err
	}
	_, err = r.DB.Exec(`UPDATE comment_reactions SET value = ? WHERE user_id = ? AND comment_id = ?`, value, userID, commentID)
	return err
}
