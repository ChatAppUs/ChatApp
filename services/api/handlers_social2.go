package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ---- Unrepost (complements existing handleRepost using posts.repost_of) ----

// DELETE /api/posts/{id}/repost — remove my repost of this post.
func (a *App) handleUnrepost(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	origID := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unrepost failed")
		return
	}
	defer tx.Rollback(r.Context())
	res, err := tx.Exec(r.Context(),
		`DELETE FROM posts WHERE repost_of=$1 AND author_id=$2`, origID, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unrepost failed")
		return
	}
	if res.RowsAffected() > 0 {
		_, _ = tx.Exec(r.Context(),
			`UPDATE posts SET share_count = GREATEST(share_count-1, 0) WHERE id=$1`, origID)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "unrepost failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": res.RowsAffected() > 0})
}

// ---- Post editing & threads ----

// PATCH /api/posts/{id} — edit body (author only).
func (a *App) handleEditPost(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	postID := r.PathValue("id")
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body := capRunes(strings.TrimSpace(req.Body), 5000)
	if body == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE posts SET body=$1, edited_at=now()
		 WHERE id=$2 AND author_id=$3 AND deleted_at IS NULL`, body, postID, uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "post not found or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/posts/{id}/thread — posts in the same thread (X-style).
func (a *App) handleThread(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	postID := r.PathValue("id")
	posts, err := a.scanPosts(r.Context(), postSelect+`
		WHERE p.deleted_at IS NULL AND (p.id = $1 OR p.thread_parent_id = $1)
		ORDER BY p.created_at ASC LIMIT 100`, postID)
	_ = uid
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load thread")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// ---- Story engagement ----

// POST /api/stories/{id}/view — record a story view (deduped).
func (a *App) handleStoryView(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	storyID := r.PathValue("id")
	res, err := a.db.Exec(r.Context(),
		`INSERT INTO story_views (story_id, viewer_id)
		 SELECT $1, $2 WHERE EXISTS(
		   SELECT 1 FROM posts WHERE id=$1 AND type='story' AND deleted_at IS NULL AND expires_at > now())
		 ON CONFLICT DO NOTHING`, storyID, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "view failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recorded": res.RowsAffected() > 0})
}

// GET /api/stories/{id}/viewers — owner only.
func (a *App) handleStoryViewers(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	storyID := r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT author_id FROM posts WHERE id=$1 AND type='story'`, storyID).Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, "story not found")
		return
	}
	if owner != uid {
		writeErr(w, http.StatusForbidden, "only the author can see viewers")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username, u.display_name, COALESCE(u.avatar_url,''), v.viewed_at
		 FROM story_views v JOIN users u ON u.id = v.viewer_id
		 WHERE v.story_id=$1 ORDER BY v.viewed_at DESC LIMIT 200`, storyID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load viewers")
		return
	}
	defer rows.Close()
	type viewer struct {
		ID       string    `json:"id"`
		Username string    `json:"username"`
		Name     string    `json:"display_name"`
		Avatar   string    `json:"avatar_url"`
		ViewedAt time.Time `json:"viewed_at"`
	}
	out := []viewer{}
	for rows.Next() {
		var v viewer
		if err := rows.Scan(&v.ID, &v.Username, &v.Name, &v.Avatar, &v.ViewedAt); err == nil {
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"viewers": out})
}

// POST /api/stories/{id}/react — {emoji}
func (a *App) handleStoryReact(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	storyID := r.PathValue("id")
	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	emoji := capRunes(strings.TrimSpace(req.Emoji), 16)
	if emoji == "" {
		writeErr(w, http.StatusBadRequest, "emoji required")
		return
	}
	var authorID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT author_id FROM posts
		 WHERE id=$1 AND type='story' AND deleted_at IS NULL AND expires_at > now()`,
		storyID).Scan(&authorID); err != nil {
		writeErr(w, http.StatusNotFound, "story not found or expired")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO story_reactions (story_id, user_id, emoji) VALUES ($1,$2,$3)
		 ON CONFLICT (story_id, user_id) DO UPDATE SET emoji=$3, created_at=now()`,
		storyID, uid, emoji); err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	if authorID != uid {
		payload, _ := json.Marshal(map[string]string{"story_id": storyID, "actor_id": uid, "emoji": emoji})
		_, _ = a.db.Exec(r.Context(),
			`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'story_reaction',$2)`, authorID, payload)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/stories/{id}/reply — {body}: DM the author with story reference.
func (a *App) handleStoryReply(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	storyID := r.PathValue("id")
	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body := capRunes(strings.TrimSpace(req.Body), 5000)
	if body == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	var authorID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT author_id FROM posts
		 WHERE id=$1 AND type='story' AND deleted_at IS NULL AND expires_at > now()`,
		storyID).Scan(&authorID); err != nil {
		writeErr(w, http.StatusNotFound, "story not found or expired")
		return
	}
	if authorID == uid {
		writeErr(w, http.StatusBadRequest, "cannot reply to your own story")
		return
	}
	if a.isBlockedEither(r.Context(), uid, authorID) {
		writeErr(w, http.StatusForbidden, "cannot message this user")
		return
	}
	convID, err := a.getOrCreateDM(r.Context(), uid, authorID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "conversation failed")
		return
	}
	var msgID string
	var createdAt time.Time
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, story_id)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		convID, uid, body, storyID).Scan(&msgID, &createdAt); err != nil {
		writeErr(w, http.StatusInternalServerError, "reply failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "id": msgID, "conversation_id": convID,
		"sender_id": uid, "body": body, "story_id": storyID, "created_at": createdAt,
	})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "conversation_id": convID, "message_id": msgID})
}

// getOrCreateDM returns the existing 1:1 conversation or creates one.
func (a *App) getOrCreateDM(ctx context.Context, aID, bID string) (string, error) {
	var convID string
	err := a.db.QueryRow(ctx,
		`SELECT m1.conversation_id FROM conversation_members m1
		 JOIN conversation_members m2 ON m2.conversation_id = m1.conversation_id AND m2.user_id = $2
		 JOIN conversations c ON c.id = m1.conversation_id
		 WHERE m1.user_id = $1 AND NOT c.is_group AND NOT c.is_channel AND NOT c.is_saved
		 LIMIT 1`, aID, bID).Scan(&convID)
	if err == nil {
		return convID, nil
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx,
		`INSERT INTO conversations (created_by) VALUES ($1) RETURNING id`, aID).Scan(&convID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO conversation_members (conversation_id, user_id, role)
		 VALUES ($1,$2,'owner'),($1,$3,'member')`, convID, aID, bID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return convID, nil
}

// capRunes truncates s to at most n runes.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
