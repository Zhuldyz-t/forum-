package repository

import (
	"database/sql"
	"forum/internal/models"
	"time"
)

type AchievementRepository struct{ DB *sql.DB }

// ForUser lists the whole catalogue with what the user has earned.
func (r *AchievementRepository) ForUser(userID int) ([]models.UserAchievement, int, error) {
	earned, err := r.earned(userID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]models.UserAchievement, 0, len(models.Achievements))
	for _, a := range models.Achievements {
		ua := models.UserAchievement{Achievement: a}
		if t, ok := earned[a.Code]; ok {
			t := t
			ua.Earned, ua.EarnedAt = true, &t
		}
		out = append(out, ua)
	}
	return out, len(earned), nil
}

func (r *AchievementRepository) earned(userID int) (map[string]time.Time, error) {
	rows, err := r.DB.Query(`SELECT achievement_code, earned_at FROM user_achievements WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var code string
		var at time.Time
		if err := rows.Scan(&code, &at); err != nil {
			return nil, err
		}
		out[code] = at
	}
	return out, rows.Err()
}

// Check awards every achievement the user now qualifies for and returns the
// new ones. Each new one also gets a notification (its toast is pending).
// Achievements are never taken away.
func (r *AchievementRepository) Check(userID int) ([]models.Achievement, error) {
	return r.check(userID, true)
}

// Backfill silently awards achievements every user already qualifies for
// (no notifications or toasts), e.g. for activity before achievements existed.
func (r *AchievementRepository) Backfill() error {
	rows, err := r.DB.Query(`SELECT id FROM users`)
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if _, err := r.check(id, false); err != nil {
			return err
		}
	}
	return nil
}

func (r *AchievementRepository) check(userID int, notify bool) ([]models.Achievement, error) {
	have, err := r.earned(userID)
	if err != nil {
		return nil, err
	}
	if len(have) == len(models.Achievements) {
		return nil, nil
	}

	var posts, comments, followers, likes, best int
	var created time.Time
	err = r.DB.QueryRow(`SELECT
		(SELECT COUNT(*) FROM posts WHERE user_id = ?),
		(SELECT COUNT(*) FROM comments WHERE user_id = ? AND is_deleted = 0),
		(SELECT COUNT(*) FROM follows WHERE following_id = ?),
		(SELECT COUNT(*) FROM post_reactions pr JOIN posts p ON p.id = pr.post_id
		 WHERE p.user_id = ? AND pr.value = 1 AND pr.user_id != ?)
		+ (SELECT COUNT(*) FROM comment_emoji_reactions cr JOIN comments c ON c.id = cr.comment_id
		 WHERE c.user_id = ? AND cr.kind = 'like' AND cr.user_id != ?),
		(SELECT COUNT(*) FROM posts bp JOIN comments bc ON bc.id = bp.best_comment_id
		 WHERE bc.user_id = ? AND bp.user_id != ?),
		created_at
		FROM users WHERE id = ?`,
		userID, userID, userID, userID, userID, userID, userID, userID, userID, userID).
		Scan(&posts, &comments, &followers, &likes, &best, &created)
	if err != nil {
		return nil, err
	}
	nightOwl, err := r.postedAtNight(userID)
	if err != nil {
		return nil, err
	}

	qualifies := map[string]bool{
		"first_post":    posts >= 1,
		"first_comment": comments >= 1,
		"posts_10":      posts >= 10,
		"comments_50":   comments >= 50,
		"followers_10":  followers >= 10,
		"likes_100":     likes >= 100,
		"best_answer":   best >= 1,
		"week":          time.Since(created) >= 7*24*time.Hour,
		"night_owl":     nightOwl,
	}

	var fresh []models.Achievement
	for _, a := range models.Achievements {
		if _, ok := have[a.Code]; ok || !qualifies[a.Code] {
			continue
		}
		res, err := r.DB.Exec(`INSERT OR IGNORE INTO user_achievements (user_id, achievement_code) VALUES (?, ?)`, userID, a.Code)
		if err != nil {
			return fresh, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // another request awarded it first
		}
		if !notify {
			continue
		}
		if _, err := r.DB.Exec(`INSERT INTO notifications (user_id, type, actor_id, achievement) VALUES (?, 'achievement', ?, ?)`,
			userID, userID, a.Code); err != nil {
			return fresh, err
		}
		fresh = append(fresh, a)
	}
	return fresh, nil
}

// postedAtNight reports whether any of the user's posts was published
// between 00:00 and 05:00 server local time.
func (r *AchievementRepository) postedAtNight(userID int) (bool, error) {
	rows, err := r.DB.Query(`SELECT created_at FROM posts WHERE user_id = ?`, userID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return false, err
		}
		if t.Local().Hour() < 5 {
			return true, nil
		}
	}
	return false, rows.Err()
}

// TakeToasts returns achievements whose toast hasn't been shown yet and
// marks them shown.
func (r *AchievementRepository) TakeToasts(userID int) ([]models.Achievement, error) {
	rows, err := r.DB.Query(`SELECT id, achievement FROM notifications
		WHERE user_id = ? AND type = 'achievement' AND toasted = 0 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	var ids []int
	var out []models.Achievement
	for rows.Next() {
		var id int
		var code string
		if err := rows.Scan(&id, &code); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		if a, ok := models.AchievementByCode(code); ok {
			out = append(out, a)
		}
	}
	rows.Close()
	for _, id := range ids {
		if _, err := r.DB.Exec(`UPDATE notifications SET toasted = 1 WHERE id = ?`, id); err != nil {
			return out, err
		}
	}
	return out, nil
}
