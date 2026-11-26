package repository

// ReputationSQL computes users.reputation for the row being updated:
// +5 per post, +2 per comment, +3 per like received (posts and «нравится» on
// comments), -2 per post dislike received, +15 per best answer, +1 per
// follower. Reactions to your own content don't count.
const ReputationSQL = `(
	5 * (SELECT COUNT(*) FROM posts WHERE user_id = users.id)
	+ 2 * (SELECT COUNT(*) FROM comments WHERE user_id = users.id AND is_deleted = 0)
	+ 3 * (SELECT COUNT(*) FROM post_reactions pr JOIN posts p ON p.id = pr.post_id
	       WHERE p.user_id = users.id AND pr.value = 1 AND pr.user_id != users.id)
	+ 3 * (SELECT COUNT(*) FROM comment_emoji_reactions cr JOIN comments c ON c.id = cr.comment_id
	       WHERE c.user_id = users.id AND cr.kind = 'like' AND cr.user_id != users.id)
	- 2 * (SELECT COUNT(*) FROM post_reactions pr JOIN posts p ON p.id = pr.post_id
	       WHERE p.user_id = users.id AND pr.value = -1 AND pr.user_id != users.id)
	+ 15 * (SELECT COUNT(*) FROM posts bp JOIN comments bc ON bc.id = bp.best_comment_id
	        WHERE bc.user_id = users.id AND bp.user_id != users.id)
	+ (SELECT COUNT(*) FROM follows WHERE following_id = users.id)
)`
