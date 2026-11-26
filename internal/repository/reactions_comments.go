package repository

import (
	"forum/internal/models"
)

// maxReactionNames is how many names a reaction tooltip lists.
const maxReactionNames = 8

// ToggleCommentReaction adds the reaction, or removes it if the user already
// has it. Each kind counts once per user.
func (r *ReactionRepository) ToggleCommentReaction(userID, commentID int, kind string) error {
	res, err := r.DB.Exec(`INSERT OR IGNORE INTO comment_emoji_reactions (comment_id, user_id, kind) VALUES (?, ?, ?)`,
		commentID, userID, kind)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}
	_, err = r.DB.Exec(`DELETE FROM comment_emoji_reactions WHERE comment_id = ? AND user_id = ? AND kind = ?`,
		commentID, userID, kind)
	return err
}

// CommentReactions summarises the reactions on the given comments for a
// viewer, keyed by comment id, in the fixed reaction order.
func (r *ReactionRepository) CommentReactions(commentIDs []int, viewerID int) (map[int][]models.ReactionSummary, error) {
	out := map[int][]models.ReactionSummary{}
	if len(commentIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(commentIDs))
	for i, id := range commentIDs {
		args[i] = id
	}
	rows, err := r.DB.Query(`
SELECT cr.comment_id, cr.kind, cr.user_id, u.username
FROM comment_emoji_reactions cr JOIN users u ON u.id = cr.user_id
WHERE cr.comment_id IN (`+placeholders(len(commentIDs))+`)
ORDER BY cr.created_at, u.username`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct {
		comment int
		kind    string
	}
	agg := map[key]*models.ReactionSummary{}
	for rows.Next() {
		var cid, uid int
		var kind, name string
		if err := rows.Scan(&cid, &kind, &uid, &name); err != nil {
			return nil, err
		}
		k := key{cid, kind}
		s, ok := agg[k]
		if !ok {
			s = &models.ReactionSummary{Kind: kind}
			agg[k] = s
		}
		s.Count++
		if uid == viewerID {
			s.Mine = true
		}
		if len(s.Users) < maxReactionNames {
			s.Users = append(s.Users, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, cid := range commentIDs {
		for _, k := range models.CommentReactionKinds {
			if s, ok := agg[key{cid, k.Kind}]; ok {
				s.Label = k.Label
				out[cid] = append(out[cid], *s)
			}
		}
	}
	return out, nil
}

// placeholders returns "?, ?, ?" for n arguments.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, n*3)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',', ' ')
		}
		b = append(b, '?')
	}
	return string(b)
}
