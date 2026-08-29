package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ---- Comment likes ----

// POST /api/comments/{id}/like
func (a *App) handleLikeComment(w http.ResponseWriter, r *http.Request) {
	uid, commentID := userIDFrom(r), r.PathValue("id")
	var postID, authorID string
	err := a.db.QueryRow(r.Context(),
		`SELECT post_id, author_id FROM comments WHERE id=$1 AND deleted_at IS NULL`,
		commentID).Scan(&postID, &authorID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "comment not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO comment_likes (comment_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		commentID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "like failed")
		return
	}
	if authorID != uid {
		payload, _ := json.Marshal(map[string]string{
			"comment_id": commentID, "post_id": postID, "actor_id": uid,
		})
		_, _ = a.db.Exec(r.Context(),
			`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'comment_like',$2)`,
			authorID, payload)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/comments/{id}/like
func (a *App) handleUnlikeComment(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM comment_likes WHERE comment_id=$1 AND user_id=$2`,
		r.PathValue("id"), userIDFrom(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- Notifications ----

// POST /api/notifications/read — mark all notifications read.
func (a *App) handleMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`UPDATE notifications SET read_at = now() WHERE user_id=$1 AND read_at IS NULL`,
		userIDFrom(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- Message search within a conversation ----

// GET /api/conversations/{id}/search?q=...
func (a *App) handleSearchMessages(w http.ResponseWriter, r *http.Request) {
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 || len(q) > 200 {
		writeErr(w, http.StatusBadRequest, "query must be 2-200 chars")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT m.id, m.sender_id, u.display_name, m.body, m.media_url, m.created_at
		 FROM messages m JOIN users u ON u.id = m.sender_id
		 WHERE m.conversation_id=$1 AND m.deleted_at IS NULL
		   AND (m.expires_at IS NULL OR m.expires_at > now())
		   AND m.body ILIKE '%' || $2 || '%'
		 ORDER BY m.created_at DESC LIMIT 50`, convID, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()
	type hit struct {
		ID        string    `json:"id"`
		SenderID  string    `json:"sender_id"`
		Name      string    `json:"sender_name"`
		Body      string    `json:"body"`
		MediaURL  string    `json:"media_url"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []hit{}
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.ID, &h.SenderID, &h.Name, &h.Body, &h.MediaURL, &h.CreatedAt); err == nil {
			out = append(out, h)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

// ---- Share post to DM ----

// POST /api/posts/{id}/share — {conversation_id}: sends the post into a chat.
func (a *App) handleSharePostToChat(w http.ResponseWriter, r *http.Request) {
	uid, postID := userIDFrom(r), r.PathValue("id")
	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !a.isMember(r.Context(), req.ConversationID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var body, authorName string
	err := a.db.QueryRow(r.Context(),
		`SELECT p.body, u.display_name FROM posts p JOIN users u ON u.id=p.author_id
		 WHERE p.id=$1 AND p.deleted_at IS NULL`, postID).Scan(&body, &authorName)
	if err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	// Server-side share: plaintext (like forwards) so every member can read it.
	text := "📤 Shared post by " + authorName + " (post:" + postID + ")\n" + capRunes(body, 500)
	a.persistAndFanout(r.Context(), uid, req.ConversationID, text, "", false, "")
	if _, err := a.db.Exec(r.Context(),
		`UPDATE posts SET share_count = share_count + 1 WHERE id=$1`, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, "share failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
