package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ---- Realtime hub (in-process; shard with Redis pub/sub for multi-node) ----

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*wsClient]bool // userID -> connections
}

type wsClient struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte
}

func newHub() *Hub {
	return &Hub{clients: map[string]map[*wsClient]bool{}}
}

func (h *Hub) add(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.userID] == nil {
		h.clients[c.userID] = map[*wsClient]bool{}
	}
	h.clients[c.userID][c] = true
}

func (h *Hub) remove(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[c.userID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.userID)
		}
	}
}

func (h *Hub) sendTo(userID string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients[userID] {
		select {
		case c.send <- payload:
		default:
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // tighten to app origins in production
}

func (a *App) handleWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	claims, err := parseJWT(a.cfg.JWTSecret, token)
	if err != nil || claims.Type != "access" {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsClient{userID: claims.Sub, conn: conn, send: make(chan []byte, 64)}
	a.hub.add(c)
	defer a.hub.remove(c)

	go func() {
		for msg := range c.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	for {
		var evt struct {
			Type           string          `json:"type"` // message | signal
			ConversationID string          `json:"conversation_id"`
			Body           string          `json:"body"`
			MediaURL       string          `json:"media_url"`
			Signal         json.RawMessage `json:"signal"` // WebRTC SDP/ICE payload
		}
		if err := conn.ReadJSON(&evt); err != nil {
			return
		}
		switch evt.Type {
		case "message":
			a.persistAndFanout(r.Context(), c.userID, evt.ConversationID, evt.Body, evt.MediaURL)
		case "signal":
			// WebRTC call signaling: forward SDP offers/answers and ICE
			// candidates to conversation members. Peer connections are
			// established directly between clients (mesh); large meetings
			// should front this with an SFU such as LiveKit.
			a.fanoutSignal(r.Context(), c.userID, evt.ConversationID, evt.Signal)
		}
	}
}

func (a *App) fanoutSignal(ctx context.Context, senderID, convID string, signal json.RawMessage) {
	if len(signal) == 0 || !a.isMember(ctx, convID, senderID) {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type":            "signal",
		"conversation_id": convID,
		"sender_id":       senderID,
		"signal":          signal,
	})
	if err != nil {
		return
	}
	rows, err := a.db.Query(ctx,
		`SELECT user_id FROM conversation_members WHERE conversation_id = $1 AND user_id <> $2`, convID, senderID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err == nil {
			a.hub.sendTo(uid, payload)
		}
	}
}

func (a *App) persistAndFanout(ctx context.Context, senderID, convID, body, mediaURL string) {
	if strings.TrimSpace(body) == "" && mediaURL == "" {
		return
	}
	if !a.isMember(ctx, convID, senderID) {
		return
	}
	var msgID string
	var createdAt time.Time
	err := a.db.QueryRow(ctx,
		`INSERT INTO messages (conversation_id, sender_id, body, media_url)
		 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
		convID, senderID, body, mediaURL).Scan(&msgID, &createdAt)
	if err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type":            "message",
		"id":              msgID,
		"conversation_id": convID,
		"sender_id":       senderID,
		"body":            body,
		"media_url":       mediaURL,
		"created_at":      createdAt,
	})
	rows, err := a.db.Query(ctx,
		`SELECT user_id FROM conversation_members WHERE conversation_id = $1`, convID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err == nil {
			a.hub.sendTo(uid, payload)
		}
	}
}

func (a *App) isMember(ctx context.Context, convID, userID string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id=$1 AND user_id=$2)`,
		convID, userID).Scan(&ok)
	return ok
}

// ---- REST chat endpoints ----

func (a *App) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IsGroup   bool     `json:"is_group"`
		Title     string   `json:"title"`
		MemberIDs []string `json:"member_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if !req.IsGroup {
		if len(req.MemberIDs) != 1 {
			writeErr(w, http.StatusBadRequest, "direct conversation needs exactly one other member")
			return
		}
		// reuse existing direct conversation if present
		var existing string
		err := a.db.QueryRow(r.Context(),
			`SELECT c.id FROM conversations c
			 WHERE c.is_group = false
			   AND EXISTS(SELECT 1 FROM conversation_members m WHERE m.conversation_id=c.id AND m.user_id=$1)
			   AND EXISTS(SELECT 1 FROM conversation_members m WHERE m.conversation_id=c.id AND m.user_id=$2)
			 LIMIT 1`, uid, req.MemberIDs[0]).Scan(&existing)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]string{"id": existing})
			return
		}
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}
	defer tx.Rollback(r.Context())
	var convID string
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO conversations (is_group, title, created_by) VALUES ($1,$2,$3) RETURNING id`,
		req.IsGroup, req.Title, uid).Scan(&convID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}
	members := append([]string{uid}, req.MemberIDs...)
	for i, m := range members {
		role := "member"
		if i == 0 {
			role = "owner"
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
			convID, m, role); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to add members")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": convID})
}

func (a *App) handleListConversations(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id, c.is_group, c.title, c.created_at,
		        (SELECT body FROM messages msg WHERE msg.conversation_id = c.id AND msg.deleted_at IS NULL
		         ORDER BY msg.created_at DESC LIMIT 1) AS last_message
		 FROM conversations c
		 JOIN conversation_members m ON m.conversation_id = c.id AND m.user_id = $1
		 ORDER BY c.created_at DESC LIMIT 100`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load conversations")
		return
	}
	defer rows.Close()
	type conv struct {
		ID          string    `json:"id"`
		IsGroup     bool      `json:"is_group"`
		Title       string    `json:"title"`
		CreatedAt   time.Time `json:"created_at"`
		LastMessage *string   `json:"last_message"`
	}
	out := []conv{}
	for rows.Next() {
		var c conv
		if err := rows.Scan(&c.ID, &c.IsGroup, &c.Title, &c.CreatedAt, &c.LastMessage); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

func (a *App) handleListMessages(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, userIDFrom(r)) {
		writeErr(w, http.StatusForbidden, "not a member of this conversation")
		return
	}
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT m.id, m.sender_id, u.display_name, m.body, m.media_url, m.created_at
		 FROM messages m JOIN users u ON u.id = m.sender_id
		 WHERE m.conversation_id = $1 AND m.deleted_at IS NULL
		 ORDER BY m.created_at DESC LIMIT $2 OFFSET $3`, convID, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load messages")
		return
	}
	defer rows.Close()
	type msg struct {
		ID        string    `json:"id"`
		SenderID  string    `json:"sender_id"`
		Sender    string    `json:"sender_name"`
		Body      string    `json:"body"`
		MediaURL  string    `json:"media_url"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []msg{}
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Sender, &m.Body, &m.MediaURL, &m.CreatedAt); err == nil {
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}
