package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

var allowedTTLs = map[int]bool{0: true, 60: true, 3600: true, 86400: true, 604800: true}

// PUT /api/conversations/{id}/ttl — {ttl_seconds}: 0 disables disappearing
// messages. In groups/channels only owner/admin may change it.
func (a *App) handleSetTTL(w http.ResponseWriter, r *http.Request) {
	uid, convID := userIDFrom(r), r.PathValue("id")
	var req struct {
		TTLSeconds int `json:"ttl_seconds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !allowedTTLs[req.TTLSeconds] {
		writeErr(w, http.StatusBadRequest, "ttl must be one of 0, 60, 3600, 86400, 604800")
		return
	}
	var isGroup, isChannel bool
	var role string
	err := a.db.QueryRow(r.Context(),
		`SELECT c.is_group, c.is_channel, m.role FROM conversations c
		 JOIN conversation_members m ON m.conversation_id = c.id AND m.user_id = $2
		 WHERE c.id = $1`, convID, uid).Scan(&isGroup, &isChannel, &role)
	if err != nil {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	if (isGroup || isChannel) && role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "only owner/admin can change the timer")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE conversations SET message_ttl_seconds = $2 WHERE id = $1`,
		convID, req.TTLSeconds); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to set timer")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type":            "ttl_changed",
		"conversation_id": convID,
		"ttl_seconds":     req.TTLSeconds,
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusOK, map[string]any{"ttl_seconds": req.TTLSeconds})
}

// GET /api/conversations/{id}/ttl
func (a *App) handleGetTTL(w http.ResponseWriter, r *http.Request) {
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var ttl int
	_ = a.db.QueryRow(r.Context(),
		`SELECT message_ttl_seconds FROM conversations WHERE id=$1`, convID).Scan(&ttl)
	writeJSON(w, http.StatusOK, map[string]any{"ttl_seconds": ttl})
}

// startExpirySweeper permanently deletes expired messages and notifies the
// affected conversations over WS. Runs every 30s; deletions are idempotent so
// concurrent instances are harmless.
func (a *App) startExpirySweeper() {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			a.sweepExpired()
		}
	}()
}

func (a *App) sweepExpired() {
	rows, err := a.db.Query(context.Background(),
		`UPDATE messages SET deleted_at = now()
		 WHERE expires_at IS NOT NULL AND expires_at <= now() AND deleted_at IS NULL
		 RETURNING id, conversation_id`)
	if err != nil {
		return
	}
	defer rows.Close()
	byConv := map[string][]string{}
	for rows.Next() {
		var id, conv string
		if err := rows.Scan(&id, &conv); err == nil {
			byConv[conv] = append(byConv[conv], id)
		}
	}
	for conv, ids := range byConv {
		payload, _ := json.Marshal(map[string]any{
			"type":            "messages_expired",
			"conversation_id": conv,
			"ids":             ids,
		})
		a.fanoutConv(context.Background(), conv, payload)
	}
}

// fanoutConv sends a payload to every member of a conversation.
func (a *App) fanoutConv(ctx context.Context, convID string, payload []byte) {
	a.fanoutToMembers(ctx, convID, payload, "")
}

// GET /api/conversations/{id}/members — member list with roles.
func (a *App) handleListMembers(w http.ResponseWriter, r *http.Request) {
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT m.user_id, u.username, u.display_name, m.role, m.joined_at
		 FROM conversation_members m JOIN users u ON u.id = m.user_id
		 WHERE m.conversation_id = $1 ORDER BY m.joined_at`, convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load members")
		return
	}
	defer rows.Close()
	type member struct {
		ID       string    `json:"id"`
		Username string    `json:"username"`
		Name     string    `json:"display_name"`
		Role     string    `json:"role"`
		JoinedAt time.Time `json:"joined_at"`
	}
	out := []member{}
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.ID, &m.Username, &m.Name, &m.Role, &m.JoinedAt); err == nil {
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}
