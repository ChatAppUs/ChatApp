package main

import (
	"net/http"
	"time"
)

// Reel analytics + community notes.

// GET /api/reels/{id}/analytics — per-reel stats, author-only. Aggregates
// views, unique viewers, watch completion, rewatches, likes, comments,
// shares and remix count from the real signal tables.
func (a *App) handleReelAnalytics(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	reelID := r.PathValue("id")
	var authorID, typ string
	var created time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT author_id, type, created_at FROM posts WHERE id=$1 AND deleted_at IS NULL`,
		reelID).Scan(&authorID, &typ, &created)
	if err != nil {
		writeErr(w, http.StatusNotFound, "reel not found")
		return
	}
	if typ != "reel" {
		writeErr(w, http.StatusBadRequest, "not a reel")
		return
	}
	if authorID != uid {
		writeErr(w, http.StatusForbidden, "only the author can view reel analytics")
		return
	}
	var views, uniqueViewers, likes, comments, shares, remixes int
	var avgCompletion, rewatchRate float64
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*), COUNT(DISTINCT user_id) FROM post_views WHERE post_id=$1`,
		reelID).Scan(&views, &uniqueViewers)
	_ = a.db.QueryRow(r.Context(),
		`SELECT COALESCE(AVG(LEAST(watched_ms::float / NULLIF(duration_ms,0), 1.0)),0),
                        COALESCE(AVG(CASE WHEN rewatched THEN 1.0 ELSE 0.0 END),0)
                 FROM reel_watch_events WHERE post_id=$1 AND duration_ms > 0`,
		reelID).Scan(&avgCompletion, &rewatchRate)
	_ = a.db.QueryRow(r.Context(),
		`SELECT like_count, comment_count, share_count FROM posts WHERE id=$1`,
		reelID).Scan(&likes, &comments, &shares)
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM posts WHERE remix_of=$1 AND deleted_at IS NULL`,
		reelID).Scan(&remixes)
	writeJSON(w, http.StatusOK, map[string]any{
		"reel_id": reelID, "views": views, "unique_viewers": uniqueViewers,
		"avg_completion": avgCompletion, "rewatch_rate": rewatchRate,
		"likes": likes, "comments": comments, "shares": shares,
		"remixes": remixes, "created_at": created,
	})
}

// GET /api/reels/{id}/remixes — public list of remixes of a reel.
func (a *App) handleReelRemixes(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, u.username, u.display_name, p.created_at, p.like_count, p.view_count,
		        COALESCE(p.remix_mode, '')
                 FROM posts p JOIN users u ON u.id = p.author_id
                 WHERE p.remix_of=$1 AND p.deleted_at IS NULL AND p.visibility='public'
                 ORDER BY p.created_at DESC LIMIT $2 OFFSET $3`,
		r.PathValue("id"), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load remixes")
		return
	}
	defer rows.Close()
	type remix struct {
		ID        string    `json:"id"`
		Username  string    `json:"username"`
		Author    string    `json:"author"`
		Likes     int       `json:"likes"`
		Views     int       `json:"views"`
		Created   time.Time `json:"created_at"`
		RemixMode string    `json:"remix_mode,omitempty"`
	}
	out := []remix{}
	for rows.Next() {
		var m remix
		if err := rows.Scan(&m.ID, &m.Username, &m.Author, &m.Created, &m.Likes, &m.Views, &m.RemixMode); err == nil {
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"remixes": out})
}

// ---------- Community notes ----------

// POST /api/posts/{id}/notes — propose a community note on a post.
func (a *App) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Body) < 20 || len(req.Body) > 1000 {
		writeErr(w, http.StatusBadRequest, "note must be 20-1000 characters")
		return
	}
	uid := userIDFrom(r)
	postID := r.PathValue("id")
	var authorID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT author_id FROM posts WHERE id=$1 AND deleted_at IS NULL`, postID).Scan(&authorID); err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	if authorID == uid {
		writeErr(w, http.StatusBadRequest, "cannot write a note on your own post")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO community_notes (post_id, author_id, body) VALUES ($1,$2,$3) RETURNING id`,
		postID, uid, req.Body).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "you already wrote a note on this post")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// POST /api/notes/{id}/vote — rate a note helpful/not helpful.
func (a *App) handleVoteNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Helpful bool `json:"helpful"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	noteID := r.PathValue("id")
	var authorID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT author_id FROM community_notes WHERE id=$1`, noteID).Scan(&authorID); err != nil {
		writeErr(w, http.StatusNotFound, "note not found")
		return
	}
	if authorID == uid {
		writeErr(w, http.StatusBadRequest, "cannot vote on your own note")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO community_note_votes (note_id, user_id, helpful) VALUES ($1,$2,$3)
                 ON CONFLICT (note_id, user_id) DO UPDATE SET helpful=$3`,
		noteID, uid, req.Helpful); err != nil {
		writeErr(w, http.StatusInternalServerError, "vote failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "voted"})
}

// GET /api/posts/{id}/notes — notes with votes; the "shown" note is the one
// with the highest helpful ratio (>= 5 votes and >= 60% helpful).
func (a *App) handleListNotes(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT n.id, n.author_id, u.username, n.body, n.created_at,
                        (SELECT count(*) FROM community_note_votes v WHERE v.note_id=n.id AND v.helpful),
                        (SELECT count(*) FROM community_note_votes v WHERE v.note_id=n.id AND NOT v.helpful),
                        COALESCE((SELECT v.helpful FROM community_note_votes v WHERE v.note_id=n.id AND v.user_id=$2)::text, '')
                 FROM community_notes n JOIN users u ON u.id = n.author_id
                 WHERE n.post_id=$1 ORDER BY n.created_at DESC`, r.PathValue("id"), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load notes")
		return
	}
	defer rows.Close()
	type note struct {
		ID         string    `json:"id"`
		AuthorID   string    `json:"author_id"`
		Username   string    `json:"username"`
		Body       string    `json:"body"`
		Helpful    int       `json:"helpful"`
		NotHelpful int       `json:"not_helpful"`
		MyVote     string    `json:"my_vote"`
		Shown      bool      `json:"shown"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := []note{}
	best, bestScore := -1, -1.0
	for rows.Next() {
		var n note
		if err := rows.Scan(&n.ID, &n.AuthorID, &n.Username, &n.Body, &n.CreatedAt,
			&n.Helpful, &n.NotHelpful, &n.MyVote); err == nil {
			total := n.Helpful + n.NotHelpful
			if total >= 5 {
				ratio := float64(n.Helpful) / float64(total)
				if ratio >= 0.6 && ratio > bestScore {
					best, bestScore = len(out), ratio
				}
			}
			out = append(out, n)
		}
	}
	if best >= 0 {
		out[best].Shown = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": out})
}

func (a *App) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM community_notes WHERE id=$1 AND author_id=$2`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "note not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
