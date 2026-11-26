package repository

import (
	"database/sql"
	"errors"
	"forum/internal/models"
	"time"
)

type PollRepository struct{ DB *sql.DB }

// Vote errors the handler turns into messages.
var (
	ErrAlreadyVoted  = errors.New("already voted")
	ErrPollEnded     = errors.New("poll ended")
	ErrSingleChoice  = errors.New("single choice poll")
	ErrBadPollOption = errors.New("bad poll option")
)

// Create adds a poll to a post.
func (r *PollRepository) Create(postID int, question string, multiple bool, endsAt *time.Time, options []string) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var ends any
	if endsAt != nil {
		ends = endsAt.UTC().Format("2006-01-02 15:04:05")
	}
	res, err := tx.Exec(`INSERT INTO polls (post_id, question, multiple, ends_at) VALUES (?, ?, ?, ?)`,
		postID, question, multiple, ends)
	if err != nil {
		return err
	}
	pollID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for i, text := range options {
		if _, err := tx.Exec(`INSERT INTO poll_options (poll_id, text, position) VALUES (?, ?, ?)`, pollID, text, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ByPost loads a post's poll with results for the viewer; nil if none.
func (r *PollRepository) ByPost(postID, viewerID int) (*models.Poll, error) {
	p := &models.Poll{PostID: postID}
	var ends sql.NullTime
	err := r.DB.QueryRow(`SELECT id, question, multiple, ends_at FROM polls WHERE post_id = ?`, postID).
		Scan(&p.ID, &p.Question, &p.Multiple, &ends)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ends.Valid {
		p.EndsAt = &ends.Time
		p.Ended = time.Now().After(ends.Time)
	}
	return p, r.fill(p, viewerID)
}

// ByID loads a poll by its id (see ByPost).
func (r *PollRepository) ByID(pollID, viewerID int) (*models.Poll, error) {
	var postID int
	if err := r.DB.QueryRow(`SELECT post_id FROM polls WHERE id = ?`, pollID).Scan(&postID); err != nil {
		return nil, err
	}
	return r.ByPost(postID, viewerID)
}

func (r *PollRepository) fill(p *models.Poll, viewerID int) error {
	rows, err := r.DB.Query(`
SELECT o.id, o.text,
       (SELECT COUNT(*) FROM poll_votes v WHERE v.option_id = o.id),
       EXISTS (SELECT 1 FROM poll_votes v WHERE v.option_id = o.id AND v.user_id = ?)
FROM poll_options o WHERE o.poll_id = ? ORDER BY o.position`, viewerID, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var o models.PollOption
		if err := rows.Scan(&o.ID, &o.Text, &o.Votes, &o.Mine); err != nil {
			return err
		}
		if o.Mine {
			p.Voted = true
		}
		p.Options = append(p.Options, o)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := r.DB.QueryRow(`SELECT COUNT(DISTINCT user_id) FROM poll_votes WHERE poll_id = ?`, p.ID).Scan(&p.Voters); err != nil {
		return err
	}
	// Share of voters who picked each option (sums to 100% for single choice).
	for i := range p.Options {
		if p.Voters > 0 {
			p.Options[i].Percent = (p.Options[i].Votes*100 + p.Voters/2) / p.Voters
		}
	}
	return nil
}

// Vote records the user's choice. Each user votes once per poll; options
// must belong to the poll, and only one unless the poll allows several.
func (r *PollRepository) Vote(pollID, userID int, optionIDs []int) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var multiple bool
	var ends sql.NullTime
	if err := tx.QueryRow(`SELECT multiple, ends_at FROM polls WHERE id = ?`, pollID).Scan(&multiple, &ends); err != nil {
		return err
	}
	if ends.Valid && time.Now().After(ends.Time) {
		return ErrPollEnded
	}
	if len(optionIDs) == 0 {
		return ErrBadPollOption
	}
	if !multiple && len(optionIDs) > 1 {
		return ErrSingleChoice
	}
	var voted int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM poll_votes WHERE poll_id = ? AND user_id = ?`, pollID, userID).Scan(&voted); err != nil {
		return err
	}
	if voted > 0 {
		return ErrAlreadyVoted
	}
	seen := map[int]bool{}
	for _, id := range optionIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var ok int
		if err := tx.QueryRow(`SELECT 1 FROM poll_options WHERE id = ? AND poll_id = ?`, id, pollID).Scan(&ok); err != nil {
			return ErrBadPollOption
		}
		if _, err := tx.Exec(`INSERT INTO poll_votes (poll_id, option_id, user_id) VALUES (?, ?, ?)`, pollID, id, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
