package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// Creator Studio additions: dashboard, post-level analytics, audience
// demographics, scheduled content management, and drafts.
//
// These close the gap flagged in ChatApp_Competitor_Comparison.md:
// "TikTok parity needs … creator analytics/rewards" and
// "Facebook parity needs … creator distribution."

// ---- Creator dashboard --------------------------------------------------------

// GET /api/creator/dashboard — 30‑day aggregate across all posts.
func (a *App) handleCreatorDashboard(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	cutoff := time.Now().AddDate(0, 0, -30)

	var d struct {
		Views           int64   `json:"total_views"`
		Likes           int64   `json:"total_likes"`
		Comments        int64   `json:"total_comments"`
		Shares          int64   `json:"total_shares"`
		FollowersGained int64   `json:"followers_gained"`
		PostsCreated    int64   `json:"posts_created"`
		EstEarnings     float64 `json:"estimated_earnings_usd"`
	}
	a.db.QueryRow(r.Context(), `
		SELECT
			COALESCE(SUM(p.view_count), 0),
			COALESCE(SUM(p.like_count), 0),
			COALESCE(SUM(p.comment_count), 0),
			COALESCE(SUM(p.share_count), 0)
		FROM posts p
		WHERE p.author_id=$1 AND p.created_at >= $2 AND p.deleted_at IS NULL`,
		uid, cutoff).Scan(&d.Views, &d.Likes, &d.Comments, &d.Shares)

	a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM followers WHERE followed_id=$1 AND created_at >= $2`,
		uid, cutoff).Scan(&d.FollowersGained)

	a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM posts WHERE author_id=$1 AND created_at >= $2 AND deleted_at IS NULL`,
		uid, cutoff).Scan(&d.PostsCreated)

	// CPM-based earnings estimate: $0.50 per 1000 qualified views
	d.EstEarnings = float64(d.Views) * 0.0005

	writeJSON(w, http.StatusOK, d)
}

// ---- Post-level analytics -----------------------------------------------------

// GET /api/creator/posts/{id}/analytics — single‑post performance breakdown.
func (a *App) handleCreatorPostAnalytics(w http.ResponseWriter, r *http.Request) {
	uid, postID := userIDFrom(r), r.PathValue("id")

	var owns bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL)`,
		postID, uid).Scan(&owns); err != nil || !owns {
		writeErr(w, http.StatusNotFound, "post not found or not yours")
		return
	}

	type hourlyBucket struct {
		Hour  int   `json:"hour"`
		Views int64 `json:"views"`
	}
	out := struct {
		PostID   string         `json:"post_id"`
		Views    int64          `json:"views"`
		Likes    int64          `json:"likes"`
		Comments int64          `json:"comments"`
		Shares   int64          `json:"shares"`
		Hourly   []hourlyBucket `json:"hourly_views"`
	}{
		PostID: postID,
	}
	a.db.QueryRow(r.Context(),
		`SELECT view_count, like_count, comment_count, share_count FROM posts WHERE id=$1`, postID).
		Scan(&out.Views, &out.Likes, &out.Comments, &out.Shares)

	rows, _ := a.db.Query(r.Context(), `
		SELECT EXTRACT(HOUR FROM created_at)::int, COUNT(*)
		FROM post_views WHERE post_id=$1 AND created_at >= NOW() - INTERVAL '48 hours'
		GROUP BY 1 ORDER BY 1`, postID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var b hourlyBucket
			if rows.Scan(&b.Hour, &b.Views) == nil {
				out.Hourly = append(out.Hourly, b)
			}
		}
	}
	if out.Hourly == nil {
		out.Hourly = []hourlyBucket{}
	}

	writeJSON(w, http.StatusOK, out)
}

// ---- Audience insights --------------------------------------------------------

// GET /api/creator/audience — follower demographics by country and growth.
func (a *App) handleCreatorAudience(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)

	type countryBucket struct {
		Country string `json:"country"`
		Count   int64  `json:"count"`
	}
	out := struct {
		TotalFollowers    int64           `json:"total_followers"`
		ByCountry         []countryBucket `json:"by_country"`
		Growth7D          int64           `json:"growth_7d"`
		Growth30D         int64           `json:"growth_30d"`
		AvgEngagementRate float64         `json:"avg_engagement_rate"`
	}{}

	a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM followers WHERE followed_id=$1`, uid).Scan(&out.TotalFollowers)

	rows, _ := a.db.Query(r.Context(), `
		SELECT COALESCE(u.country, 'unknown'), COUNT(*)
		FROM followers f JOIN users u ON u.id = f.follower_id
		WHERE f.followed_id=$1
		GROUP BY 1 ORDER BY 2 DESC LIMIT 20`, uid)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var b countryBucket
			if rows.Scan(&b.Country, &b.Count) == nil {
				out.ByCountry = append(out.ByCountry, b)
			}
		}
	}
	if out.ByCountry == nil {
		out.ByCountry = []countryBucket{}
	}

	cutoff7 := time.Now().AddDate(0, 0, -7)
	cutoff30 := time.Now().AddDate(0, 0, -30)
	a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM followers WHERE followed_id=$1 AND created_at >= $2`, uid, cutoff7).Scan(&out.Growth7D)
	a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM followers WHERE followed_id=$1 AND created_at >= $2`, uid, cutoff30).Scan(&out.Growth30D)

	if out.TotalFollowers > 0 {
		var recentPosts int64
		var totalEngagement int64
		a.db.QueryRow(r.Context(),
			`SELECT COUNT(*), COALESCE(SUM(like_count)+SUM(comment_count)+SUM(share_count),0)
			 FROM posts WHERE author_id=$1 AND created_at >= $2 AND deleted_at IS NULL`,
			uid, cutoff30).Scan(&recentPosts, &totalEngagement)
		if recentPosts > 0 {
			out.AvgEngagementRate = float64(totalEngagement) / float64(recentPosts) / float64(out.TotalFollowers) * 100
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// ---- Scheduled posts ----------------------------------------------------------

// GET /api/creator/scheduled — list scheduled posts for the creator.
func (a *App) handleCreatorScheduled(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id, body, scheduled_for, created_at
		FROM scheduled_posts WHERE author_id=$1 AND published=false
		ORDER BY scheduled_for LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load scheduled posts")
		return
	}
	defer rows.Close()
	type sched struct {
		ID           string    `json:"id"`
		Body         string    `json:"body"`
		ScheduledFor time.Time `json:"scheduled_for"`
		CreatedAt    time.Time `json:"created_at"`
	}
	var out []sched
	for rows.Next() {
		var s sched
		if rows.Scan(&s.ID, &s.Body, &s.ScheduledFor, &s.CreatedAt) == nil {
			out = append(out, s)
		}
	}
	if out == nil {
		out = []sched{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scheduled": out})
}

// ---- Content drafts -----------------------------------------------------------

// POST /api/creator/drafts — save a draft post.
func (a *App) handleCreatorSaveDraft(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Body       string          `json:"body"`
		Media      json.RawMessage `json:"media"`
		Visibility string          `json:"visibility"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var draftID string
	if err := a.db.QueryRow(r.Context(), `
		INSERT INTO post_drafts (author_id, body, media, visibility)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		uid, req.Body, req.Media, req.Visibility).Scan(&draftID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save draft")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": draftID, "status": "saved"})
}

// GET /api/creator/drafts — list drafts.
func (a *App) handleCreatorDrafts(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id, body, media, visibility, updated_at
		FROM post_drafts WHERE author_id=$1 ORDER BY updated_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load drafts")
		return
	}
	defer rows.Close()
	type draft struct {
		ID         string          `json:"id"`
		Body       string          `json:"body"`
		Media      json.RawMessage `json:"media"`
		Visibility string          `json:"visibility"`
		UpdatedAt  time.Time       `json:"updated_at"`
	}
	var out []draft
	for rows.Next() {
		var d draft
		if rows.Scan(&d.ID, &d.Body, &d.Media, &d.Visibility, &d.UpdatedAt) == nil {
			out = append(out, d)
		}
	}
	if out == nil {
		out = []draft{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": out})
}
