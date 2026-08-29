package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// ---- Pinned messages (Telegram/FB Messenger) ----

// POST /api/chats/{id}/pins/{messageId}
func (a *App) handlePinMessage(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID, msgID := r.PathValue("id"), r.PathValue("messageId")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	srcConv, _, found := a.messageConv(r.Context(), msgID)
	if !found || srcConv != convID {
		writeErr(w, http.StatusNotFound, "message not in this conversation")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO message_pins (conversation_id, message_id, pinned_by) VALUES ($1,$2,$3)
		 ON CONFLICT DO NOTHING`, convID, msgID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "pin failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "pin", "conversation_id": convID, "message_id": msgID, "pinned_by": uid,
	})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/chats/{id}/pins/{messageId}
func (a *App) handleUnpinMessage(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID, msgID := r.PathValue("id"), r.PathValue("messageId")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`DELETE FROM message_pins WHERE conversation_id=$1 AND message_id=$2`, convID, msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "unpin failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "unpin", "conversation_id": convID, "message_id": msgID,
	})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/chats/{id}/pins
func (a *App) handleListPins(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT m.id, m.sender_id, u.display_name, m.body, m.media_url, m.created_at, p.pinned_at
		 FROM message_pins p
		 JOIN messages m ON m.id = p.message_id AND m.deleted_at IS NULL
		 JOIN users u ON u.id = m.sender_id
		 WHERE p.conversation_id=$1 ORDER BY p.pinned_at DESC LIMIT 50`, convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load pins")
		return
	}
	defer rows.Close()
	type pin struct {
		ID        string    `json:"id"`
		SenderID  string    `json:"sender_id"`
		Sender    string    `json:"sender_name"`
		Body      string    `json:"body"`
		MediaURL  string    `json:"media_url"`
		CreatedAt time.Time `json:"created_at"`
		PinnedAt  time.Time `json:"pinned_at"`
	}
	out := []pin{}
	for rows.Next() {
		var p pin
		if err := rows.Scan(&p.ID, &p.SenderID, &p.Sender, &p.Body, &p.MediaURL, &p.CreatedAt, &p.PinnedAt); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pins": out})
}

// ---- Forward with attribution (Telegram) ----

// POST /api/messages/{id}/forward — {conversation_id}
func (a *App) handleForwardMessage(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	msgID := r.PathValue("id")
	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ConversationID == "" {
		writeErr(w, http.StatusBadRequest, "conversation_id required")
		return
	}
	srcConv, senderID, ok := a.messageConv(r.Context(), msgID)
	if !ok {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	// must be able to read the source and write to the target
	if !a.isMember(r.Context(), srcConv, uid) || !a.isMember(r.Context(), req.ConversationID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	_ = senderID
	var body, mediaURL string
	var isEncrypted bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT body, media_url, is_encrypted FROM messages WHERE id=$1 AND deleted_at IS NULL`,
		msgID).Scan(&body, &mediaURL, &isEncrypted); err != nil {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	if isEncrypted {
		writeErr(w, http.StatusForbidden, "encrypted messages cannot be forwarded")
		return
	}
	var newID string
	var createdAt time.Time
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, media_url, forwarded_from)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id, created_at`,
		req.ConversationID, uid, body, mediaURL, msgID).Scan(&newID, &createdAt); err != nil {
		writeErr(w, http.StatusInternalServerError, "forward failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "id": newID, "conversation_id": req.ConversationID,
		"sender_id": uid, "body": body, "media_url": mediaURL,
		"forwarded_from": msgID, "created_at": createdAt,
	})
	a.fanoutToMembers(r.Context(), req.ConversationID, payload, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message_id": newID})
}

// ---- Saved Messages (Telegram self-chat) ----

// POST /api/chats/saved — get-or-create the caller's Saved Messages chat.
func (a *App) handleSavedMessages(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var convID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM conversations WHERE is_saved AND created_by=$1`, uid).Scan(&convID)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"conversation_id": convID})
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed")
		return
	}
	defer tx.Rollback(r.Context())
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO conversations (created_by, title, is_saved)
		 VALUES ($1,'Saved Messages', true)
		 ON CONFLICT DO NOTHING RETURNING id`, uid).Scan(&convID); err != nil {
		// lost the race — read the winner
		if err2 := tx.QueryRow(r.Context(),
			`SELECT id FROM conversations WHERE is_saved AND created_by=$1`, uid).Scan(&convID); err2 != nil {
			writeErr(w, http.StatusInternalServerError, "failed")
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO conversation_members (conversation_id, user_id, role)
		 VALUES ($1,$2,'owner') ON CONFLICT DO NOTHING`, convID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversation_id": convID})
}
