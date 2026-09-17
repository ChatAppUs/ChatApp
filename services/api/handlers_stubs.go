package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// handlers_stubs.go — fills every verified 1-2 line stub found in the
// deep audit of main vs ChatApp_Competitor_Comparison.md (Sep 2026).
// 15 stubs → 15 real implementations.

// ---- Duet (TikTok row: "duet/stitch/remix") ----

func (a *App) handleDuet(w http.ResponseWriter, r *http.Request) {
	uid, reelID := userIDFrom(r), r.PathValue("id")
	var req struct {
		Side  string `json:"side"` // left | right
		Body  string `json:"body"`
		Media string `json:"media_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Side != "left" && req.Side != "right" {
		req.Side = "right"
	}
	var duetID string
	if err := a.db.QueryRow(r.Context(), `
		INSERT INTO posts (author_id, body, media_ids, kind, parent_reel_id, duet_side)
		VALUES ($1,$2,$3,'reel',$4,$5) RETURNING id`,
		uid, req.Body, jsonArray(req.Media), reelID, req.Side).Scan(&duetID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create duet")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": duetID, "status": "created"})
}

// ---- Stitch (TikTok row: "duet/stitch/remix") ----

func (a *App) handleStitch(w http.ResponseWriter, r *http.Request) {
	uid, reelID := userIDFrom(r), r.PathValue("id")
	var req struct {
		Body      string  `json:"body"`
		Media     string  `json:"media_id"`
		ClipStart float64 `json:"clip_start_seconds"`
		ClipEnd   float64 `json:"clip_end_seconds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ClipStart < 0 {
		req.ClipStart = 0
	}
	if req.ClipEnd <= req.ClipStart {
		req.ClipEnd = req.ClipStart + 15
	}
	var stitchID string
	if err := a.db.QueryRow(r.Context(), `
		INSERT INTO posts (author_id, body, media_ids, kind, stitch_source_id, stitch_start, stitch_end)
		VALUES ($1,$2,$3,'reel',$4,$5,$6) RETURNING id`,
		uid, req.Body, jsonArray(req.Media), reelID, req.ClipStart, req.ClipEnd).Scan(&stitchID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create stitch")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": stitchID, "status": "created"})
}

// ---- Playlists (TikTok row: playlist management) ----

func (a *App) handleMyPlaylists(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT p.id, p.title, p.description, COUNT(pi.post_id), p.cover_post_id, p.created_at
		FROM playlists p
		LEFT JOIN playlist_items pi ON pi.playlist_id = p.id
		WHERE p.owner_id=$1 AND p.deleted_at IS NULL
		GROUP BY p.id ORDER BY p.updated_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load playlists")
		return
	}
	defer rows.Close()
	type playlist struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		ItemCount   int64     `json:"item_count"`
		CoverPostID string    `json:"cover_post_id"`
		CreatedAt   time.Time `json:"created_at"`
	}
	var out []playlist
	for rows.Next() {
		var p playlist
		if rows.Scan(&p.ID, &p.Title, &p.Description, &p.ItemCount, &p.CoverPostID, &p.CreatedAt) == nil {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []playlist{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": out})
}

func (a *App) handleUserPlaylists(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("id")
	rows, err := a.db.Query(r.Context(), `
		SELECT p.id, p.title, p.description, COUNT(pi.post_id), p.cover_post_id, p.created_at
		FROM playlists p
		LEFT JOIN playlist_items pi ON pi.playlist_id = p.id
		WHERE p.owner_id=$1 AND p.deleted_at IS NULL AND p.is_public=true
		GROUP BY p.id ORDER BY p.updated_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load playlists")
		return
	}
	defer rows.Close()
	type playlist struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		ItemCount   int64     `json:"item_count"`
		CoverPostID string    `json:"cover_post_id"`
		CreatedAt   time.Time `json:"created_at"`
	}
	var out []playlist
	for rows.Next() {
		var p playlist
		if rows.Scan(&p.ID, &p.Title, &p.Description, &p.ItemCount, &p.CoverPostID, &p.CreatedAt) == nil {
			out = append(out, p)
		}
	}
	if out == nil {
		out = []playlist{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": out})
}

// ---- Tiers — monetization stubs (listed on creator profile/subscription) ----

func (a *App) handleListMyTiers(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id, name, price_cents, description, benefits, subscriber_count
		FROM creator_tiers WHERE creator_id=$1 AND deleted_at IS NULL
		ORDER BY price_cents`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tiers")
		return
	}
	defer rows.Close()
	type tier struct {
		ID              string          `json:"id"`
		Name            string          `json:"name"`
		PriceCents      int64           `json:"price_cents"`
		Description     string          `json:"description"`
		Benefits        json.RawMessage `json:"benefits"`
		SubscriberCount int64           `json:"subscriber_count"`
	}
	var out []tier
	for rows.Next() {
		var t tier
		if rows.Scan(&t.ID, &t.Name, &t.PriceCents, &t.Description, &t.Benefits, &t.SubscriberCount) == nil {
			out = append(out, t)
		}
	}
	if out == nil {
		out = []tier{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tiers": out})
}

func (a *App) handleListCreatorTiers(w http.ResponseWriter, r *http.Request) {
	creatorID := r.PathValue("id")
	rows, err := a.db.Query(r.Context(), `
		SELECT id, name, price_cents, description, benefits, subscriber_count
		FROM creator_tiers WHERE creator_id=$1 AND deleted_at IS NULL
		ORDER BY price_cents`, creatorID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tiers")
		return
	}
	defer rows.Close()
	type tier struct {
		ID              string          `json:"id"`
		Name            string          `json:"name"`
		PriceCents      int64           `json:"price_cents"`
		Description     string          `json:"description"`
		Benefits        json.RawMessage `json:"benefits"`
		SubscriberCount int64           `json:"subscriber_count"`
	}
	var out []tier
	for rows.Next() {
		var t tier
		if rows.Scan(&t.ID, &t.Name, &t.PriceCents, &t.Description, &t.Benefits, &t.SubscriberCount) == nil {
			out = append(out, t)
		}
	}
	if out == nil {
		out = []tier{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tiers": out})
}

// ---- Forums: topic pin/lock (Facebook/X group parity) ----

func (a *App) handleForumTopicPin(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		GroupID string `json:"-"`
		TopicID string `json:"-"`
		Pinned  bool   `json:"pinned"`
	}
	req.GroupID = r.PathValue("id")
	req.TopicID = r.PathValue("topicId")
	decodeJSON(w, r, &req)
	tag, err := a.db.Exec(r.Context(), `
		UPDATE forum_topics SET is_pinned=$1, updated_at=NOW()
		WHERE id=$2 AND group_id=$3 AND deleted_at IS NULL`, req.Pinned, req.TopicID, req.GroupID)
	if err != nil || rowsAffected(tag) == 0 {
		writeErr(w, http.StatusNotFound, "topic not found")
		return
	}
	_ = uid
	writeJSON(w, http.StatusOK, map[string]bool{"pinned": req.Pinned})
}

func (a *App) handleForumTopicLock(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		GroupID string `json:"-"`
		TopicID string `json:"-"`
		Locked  bool   `json:"locked"`
	}
	req.GroupID = r.PathValue("id")
	req.TopicID = r.PathValue("topicId")
	decodeJSON(w, r, &req)
	tag, err := a.db.Exec(r.Context(), `
		UPDATE forum_topics SET is_locked=$1, updated_at=NOW()
		WHERE id=$2 AND group_id=$3 AND deleted_at IS NULL`, req.Locked, req.TopicID, req.GroupID)
	if err != nil || rowsAffected(tag) == 0 {
		writeErr(w, http.StatusNotFound, "topic not found")
		return
	}
	_ = uid
	writeJSON(w, http.StatusOK, map[string]bool{"locked": req.Locked})
}

// ---- P2P trade operations ----

func (a *App) handleP2PTradeRelease(w http.ResponseWriter, r *http.Request) {
	uid, tradeID := userIDFrom(r), r.PathValue("id")
	tag, err := a.db.Exec(r.Context(), `
		UPDATE p2p_trades SET status='completed', released_at=NOW(), released_by=$1
		WHERE id=$2 AND seller_id=$1 AND status='paid'`, uid, tradeID)
	if err != nil || rowsAffected(tag) == 0 {
		writeErr(w, http.StatusBadRequest, "trade not found or not in paid state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "completed"})
}

func (a *App) handleP2PTradeCancel(w http.ResponseWriter, r *http.Request) {
	uid, tradeID := userIDFrom(r), r.PathValue("id")
	tag, err := a.db.Exec(r.Context(), `
		UPDATE p2p_trades SET status='cancelled', cancelled_at=NOW(), cancelled_by=$1
		WHERE id=$2 AND (buyer_id=$1 OR seller_id=$1) AND status IN ('open','paid')`,
		uid, tradeID)
	if err != nil || rowsAffected(tag) == 0 {
		writeErr(w, http.StatusBadRequest, "trade not found or cannot be cancelled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ---- Albums (Facebook parity row) ----

func (a *App) handleMyAlbums(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT a.id, a.title, a.description, a.cover_post_id, COUNT(ai.post_id), a.created_at
		FROM albums a
		LEFT JOIN album_items ai ON ai.album_id = a.id
		WHERE a.owner_id=$1 AND a.deleted_at IS NULL
		GROUP BY a.id ORDER BY a.updated_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load albums")
		return
	}
	defer rows.Close()
	type album struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		CoverPostID string    `json:"cover_post_id"`
		ItemCount   int64     `json:"item_count"`
		CreatedAt   time.Time `json:"created_at"`
	}
	var out []album
	for rows.Next() {
		var al album
		if rows.Scan(&al.ID, &al.Title, &al.Description, &al.CoverPostID, &al.ItemCount, &al.CreatedAt) == nil {
			out = append(out, al)
		}
	}
	if out == nil {
		out = []album{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"albums": out})
}

func (a *App) handleUserAlbums(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("id")
	rows, err := a.db.Query(r.Context(), `
		SELECT a.id, a.title, a.description, a.cover_post_id, COUNT(ai.post_id), a.created_at
		FROM albums a
		LEFT JOIN album_items ai ON ai.album_id = a.id
		WHERE a.owner_id=$1 AND a.deleted_at IS NULL AND a.is_public=true
		GROUP BY a.id ORDER BY a.updated_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load albums")
		return
	}
	defer rows.Close()
	type album struct {
		ID          string    `json:"id"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		CoverPostID string    `json:"cover_post_id"`
		ItemCount   int64     `json:"item_count"`
		CreatedAt   time.Time `json:"created_at"`
	}
	var out []album
	for rows.Next() {
		var al album
		if rows.Scan(&al.ID, &al.Title, &al.Description, &al.CoverPostID, &al.ItemCount, &al.CreatedAt) == nil {
			out = append(out, al)
		}
	}
	if out == nil {
		out = []album{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"albums": out})
}

// ---- Story highlights ----

func (a *App) handleMyHighlights(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT h.id, h.title, h.cover_story_id, COUNT(hi.story_id), h.created_at
		FROM story_highlights h
		LEFT JOIN highlight_items hi ON hi.highlight_id = h.id
		WHERE h.owner_id=$1 AND h.deleted_at IS NULL
		GROUP BY h.id ORDER BY h.updated_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load highlights")
		return
	}
	defer rows.Close()
	type highlight struct {
		ID           string    `json:"id"`
		Title        string    `json:"title"`
		CoverStoryID string    `json:"cover_story_id"`
		ItemCount    int64     `json:"item_count"`
		CreatedAt    time.Time `json:"created_at"`
	}
	var out []highlight
	for rows.Next() {
		var h highlight
		if rows.Scan(&h.ID, &h.Title, &h.CoverStoryID, &h.ItemCount, &h.CreatedAt) == nil {
			out = append(out, h)
		}
	}
	if out == nil {
		out = []highlight{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"highlights": out})
}

// ---- Poll ----

func (a *App) handleGetPoll(w http.ResponseWriter, r *http.Request) {
	postID := r.PathValue("id")
	var poll struct {
		PostID     string          `json:"post_id"`
		Question   string          `json:"question"`
		Options    json.RawMessage `json:"options"`
		TotalVotes int64           `json:"total_votes"`
		MyVote     string          `json:"my_vote"`
		IsClosed   bool            `json:"is_closed"`
	}
	uid := userIDFrom(r)
	if err := a.db.QueryRow(r.Context(), `
		SELECT p.id, p.body, p.poll_options,
			COALESCE(p.poll_total_votes, 0), p.poll_closed
		FROM posts p WHERE p.id=$1 AND p.kind='poll' AND p.deleted_at IS NULL`,
		postID).Scan(&poll.PostID, &poll.Question, &poll.Options, &poll.TotalVotes, &poll.IsClosed); err != nil {
		writeErr(w, http.StatusNotFound, "poll not found")
		return
	}
	var myVote string
	_ = a.db.QueryRow(r.Context(),
		`SELECT option_key FROM poll_votes WHERE post_id=$1 AND user_id=$2`,
		postID, uid).Scan(&myVote)
	poll.MyVote = myVote
	writeJSON(w, http.StatusOK, poll)
}

// ---- Admin staking queue ----

func (a *App) handleAdminStakingQueue(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
		SELECT s.id, s.user_id, s.asset, s.amount, s.status, s.created_at
		FROM staking_positions s
		WHERE s.status IN ('pending_unlock','pending_stake')
		ORDER BY s.created_at LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load staking queue")
		return
	}
	defer rows.Close()
	type stakingEntry struct {
		ID        string    `json:"id"`
		UserID    string    `json:"user_id"`
		Asset     string    `json:"asset"`
		Amount    string    `json:"amount"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	var out []stakingEntry
	for rows.Next() {
		var e stakingEntry
		if rows.Scan(&e.ID, &e.UserID, &e.Asset, &e.Amount, &e.Status, &e.CreatedAt) == nil {
			out = append(out, e)
		}
	}
	if out == nil {
		out = []stakingEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": out})
}

// rowsAffected is a helper that extracts the RowsAffected count from a sql.Result,
// ignoring errors. Use only when the preceding Exec already succeeded.
func rowsAffected(tag interface{ RowsAffected() (int64, error) }) int64 {
	n, _ := tag.RowsAffected()
	return n
}