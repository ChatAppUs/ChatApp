package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
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

// connCount reports the live WebSocket connection count (cluster load metric).
func (h *Hub) connCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, conns := range h.clients {
		n += len(conns)
	}
	return n
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

// isOnline reports whether the user has at least one live WS connection.
func (h *Hub) isOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

// upgrader.CheckOrigin is installed in main() from cfg.AllowedOrigins:
// browser cross-site connections are restricted to our own app origins;
// native clients (no Origin header) are always allowed. Token auth in the
// query string is the primary defense; this blocks CSWSH from web pages.
var upgrader = websocket.Upgrader{}

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
	// Presence: mark online now, stamp last seen on disconnect.
	_, _ = a.db.Exec(r.Context(), `UPDATE users SET last_seen_at=now() WHERE id=$1`, c.userID)
	defer func() {
		_, _ = a.db.Exec(context.Background(), `UPDATE users SET last_seen_at=now() WHERE id=$1`, c.userID)
	}()

	go func() {
		for msg := range c.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	for {
		var evt struct {
			Type           string          `json:"type"` // message | signal | typing
			ConversationID string          `json:"conversation_id"`
			Body           string          `json:"body"`
			MediaURL       string          `json:"media_url"`
			IsEncrypted    bool            `json:"is_encrypted"`
			Silent         bool            `json:"silent"`
			TopicID        string          `json:"topic_id"`
			ReplyTo        string          `json:"reply_to"`
			Signal         json.RawMessage `json:"signal"` // WebRTC SDP/ICE payload
		}
		if err := conn.ReadJSON(&evt); err != nil {
			return
		}
		switch evt.Type {
		case "message":
			a.persistMessage(r.Context(), c.userID, evt.ConversationID, evt.Body, evt.MediaURL, evt.IsEncrypted, evt.Silent, evt.TopicID, evt.ReplyTo)
		case "signal":
			// WebRTC call signaling: forward SDP offers/answers and ICE
			// candidates to conversation members. Peer connections are
			// established directly between clients (mesh); large meetings
			// should front this with an SFU such as LiveKit.
			a.fanoutSignal(r.Context(), c.userID, evt.ConversationID, evt.Signal)
		case "typing":
			// Ephemeral typing indicator; never persisted.
			if a.isMember(r.Context(), evt.ConversationID, c.userID) {
				payload, _ := json.Marshal(map[string]any{
					"type": "typing", "conversation_id": evt.ConversationID, "user_id": c.userID,
				})
				a.fanoutToMembers(r.Context(), evt.ConversationID, payload, c.userID)
			}
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

// persistAndFanout keeps the original 7-arg call shape used by older
// callers (bots, story replies) and delegates to persistMessage.
func (a *App) persistAndFanout(ctx context.Context, senderID, convID, body, mediaURL string, isEncrypted bool, replyTo string) {
	a.persistMessage(ctx, senderID, convID, body, mediaURL, isEncrypted, false, "", replyTo)
}

// persistMessage is the single message-write path: membership + channel +
// slow-mode checks, insert with TTL/topic/silent, realtime fanout, bot
// update enqueue, message-request creation for first-time DMs, and push
// for offline members.
func (a *App) persistMessage(ctx context.Context, senderID, convID, body, mediaURL string, isEncrypted, silent bool, topicID, replyTo string) {
	if strings.TrimSpace(body) == "" && mediaURL == "" {
		return
	}
	if !a.isMember(ctx, convID, senderID) {
		return
	}
	// Broadcast channels: only owner/admin may post.
	var isChannel bool
	var role string
	var ttl, slowMode int
	_ = a.db.QueryRow(ctx,
		`SELECT c.is_channel, c.message_ttl_seconds, c.slow_mode_seconds, m.role FROM conversations c
		 JOIN conversation_members m ON m.conversation_id = c.id AND m.user_id = $2
		 WHERE c.id = $1`, convID, senderID).Scan(&isChannel, &ttl, &slowMode, &role)
	if isChannel && role != "owner" && role != "admin" {
		return
	}
	// Slow mode: owner/admin are exempt; others wait slow_mode_seconds
	// between messages (Telegram parity).
	if slowMode > 0 && role != "owner" && role != "admin" {
		// Advance the mark only when the cooldown elapsed; if the returned
		// timestamp is too recent the mark was not advanced: reject.
		var lastSent time.Time
		err := a.db.QueryRow(ctx,
			`INSERT INTO message_rate_marks (conversation_id, user_id, last_sent_at)
			 VALUES ($1,$2,now())
			 ON CONFLICT (conversation_id, user_id) DO UPDATE
			 SET last_sent_at = CASE
			   WHEN message_rate_marks.last_sent_at + make_interval(secs => $3) <= now() THEN now()
			   ELSE message_rate_marks.last_sent_at END
			 RETURNING last_sent_at`, convID, senderID, slowMode).Scan(&lastSent)
		if err != nil || time.Since(lastSent) < time.Duration(slowMode)*time.Second-time.Second {
			return
		}
	}
	var msgID string
	var createdAt time.Time
	var expiresAt *time.Time
	err := a.db.QueryRow(ctx,
		`INSERT INTO messages (conversation_id, sender_id, body, media_url, is_encrypted, reply_to_id, expires_at, is_silent, topic_id)
		 VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,
		         CASE WHEN $7 > 0 THEN now() + make_interval(secs => $7) END,
		         $8, NULLIF($9,'')::uuid)
		 RETURNING id, created_at, expires_at`,
		convID, senderID, body, mediaURL, isEncrypted, replyTo, ttl, silent, topicID).Scan(&msgID, &createdAt, &expiresAt)
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
		"is_encrypted":    isEncrypted,
		"is_silent":       silent,
		"topic_id":        topicID,
		"reply_to":        replyTo,
		"created_at":      createdAt,
		"expires_at":      expiresAt,
	})
	a.fanoutConv(ctx, convID, payload)
	a.enqueueBotUpdates(ctx, convID, senderID, msgID, body, mediaURL)
	a.afterMessageNotify(ctx, senderID, convID, body, silent)
	if previewURL := firstURL(body); previewURL != "" {
		go a.attachLinkPreview(context.Background(), msgID, convID, previewURL)
	}
}

// afterMessageNotify creates the message-request row for first-time DMs and
// queues push notifications for members without a live connection.
func (a *App) afterMessageNotify(ctx context.Context, senderID, convID, body string, silent bool) {
	rows, err := a.db.Query(ctx,
		`SELECT user_id FROM conversation_members WHERE conversation_id=$1 AND user_id<>$2`, convID, senderID)
	if err != nil {
		return
	}
	var recipients []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err == nil {
			recipients = append(recipients, uid)
		}
	}
	rows.Close()
	var isGroup bool
	_ = a.db.QueryRow(ctx, `SELECT is_group OR is_channel FROM conversations WHERE id=$1`, convID).Scan(&isGroup)
	var senderName string
	_ = a.db.QueryRow(ctx, `SELECT display_name FROM users WHERE id=$1`, senderID).Scan(&senderName)
	for _, uid := range recipients {
		if !isGroup {
			// First-time DM from someone the recipient does not follow and who
			// does not follow them becomes a message request.
			var isReq bool
			_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM message_requests WHERE conversation_id=$1 AND recipient_id=$2)`, convID, uid).Scan(&isReq)
			if !isReq {
				var related bool
				_ = a.db.QueryRow(ctx,
					`SELECT EXISTS(SELECT 1 FROM follows WHERE (follower_id=$1 AND followee_id=$2) OR (follower_id=$2 AND followee_id=$1))`,
					uid, senderID).Scan(&related)
				if !related {
					_, _ = a.db.Exec(ctx,
						`INSERT INTO message_requests (conversation_id, recipient_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
						convID, uid)
				}
			}
		}
		if silent {
			continue // silent messages never push
		}
		if a.hub.isOnline(uid) {
			continue // already delivered over the live socket
		}
		preview := body
		if len(preview) > 120 {
			preview = preview[:120]
		}
		title := senderName
		if title == "" {
			title = "New message"
		}
		a.queuePush(ctx, uid, "message", title, preview,
			map[string]any{"conversation_id": convID, "sender_id": senderID})
	}
}

// firstURL extracts the first http(s) URL in a message body.
func firstURL(body string) string {
	for _, field := range strings.Fields(body) {
		if strings.HasPrefix(field, "https://") || strings.HasPrefix(field, "http://") {
			return strings.TrimRight(field, ".,;)!]>}")
		}
	}
	return ""
}

// attachLinkPreview resolves the first link in a message asynchronously and
// fans the preview out to the conversation when found.
func (a *App) attachLinkPreview(ctx context.Context, msgID, convID, rawURL string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "ChatApp-LinkPreview/1.0")
	req.Header.Set("Accept", "text/html")
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		return
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return
	}
	html := string(page)
	pick := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(html); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
		return ""
	}
	title := pick(ogTitleRe)
	if title == "" {
		title = pick(titleTagRe)
	}
	if title == "" {
		return
	}
	preview := map[string]string{
		"url": rawURL, "title": title, "description": pick(ogDescRe), "image": pick(ogImageRe),
	}
	pj, _ := json.Marshal(preview)
	if _, err := a.db.Exec(ctx, `UPDATE messages SET link_preview=$2 WHERE id=$1`, msgID, pj); err != nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "link_preview", "conversation_id": convID, "message_id": msgID, "preview": preview,
	})
	a.fanoutConv(ctx, convID, payload)
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
		IsGroup     bool     `json:"is_group"`
		IsChannel   bool     `json:"is_channel"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		MemberIDs   []string `json:"member_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if req.IsChannel {
		req.IsGroup = false
	}
	if !req.IsGroup && !req.IsChannel {
		if len(req.MemberIDs) != 1 {
			writeErr(w, http.StatusBadRequest, "direct conversation needs exactly one other member")
			return
		}
		if a.isBlockedEither(r.Context(), uid, req.MemberIDs[0]) {
			writeErr(w, http.StatusForbidden, "cannot message this user")
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
		`INSERT INTO conversations (is_group, is_channel, title, description, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		req.IsGroup, req.IsChannel, req.Title, req.Description, uid).Scan(&convID); err != nil {
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
		`SELECT c.id, c.is_group, c.is_channel, c.title, c.created_at, c.theme,
		        (SELECT body FROM messages msg WHERE msg.conversation_id = c.id AND msg.deleted_at IS NULL
		         ORDER BY msg.created_at DESC LIMIT 1) AS last_message,
		        (SELECT count(*) FROM messages msg WHERE msg.conversation_id = c.id AND msg.deleted_at IS NULL
		         AND msg.sender_id <> $1
		         AND msg.created_at > COALESCE((SELECT last_read_at FROM conversation_reads r
		          WHERE r.conversation_id = c.id AND r.user_id = $1), 'epoch')) AS unread
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
		IsChannel   bool      `json:"is_channel"`
		Title       string    `json:"title"`
		CreatedAt   time.Time `json:"created_at"`
		Theme       string    `json:"theme"`
		LastMessage *string   `json:"last_message"`
		Unread      int64     `json:"unread"`
	}
	out := []conv{}
	for rows.Next() {
		var c conv
		if err := rows.Scan(&c.ID, &c.IsGroup, &c.IsChannel, &c.Title, &c.CreatedAt, &c.Theme, &c.LastMessage, &c.Unread); err == nil {
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
		`SELECT m.id, m.sender_id, u.display_name, m.body, m.media_url, m.is_encrypted,
		        COALESCE(m.reply_to_id::text,''), m.created_at, m.edited_at,
	 COALESCE(m.forwarded_from::text,''), COALESCE(m.story_id::text,''),
	 EXISTS(SELECT 1 FROM message_pins mp WHERE mp.message_id = m.id),
		        COALESCE((SELECT json_object_agg(emoji, cnt) FROM (
		          SELECT emoji, count(*) AS cnt FROM message_reactions WHERE message_id = m.id GROUP BY emoji
		        ) r), '{}'::json), m.expires_at
		 FROM messages m JOIN users u ON u.id = m.sender_id
		 WHERE m.conversation_id = $1 AND m.deleted_at IS NULL
		   AND (m.expires_at IS NULL OR m.expires_at > now())
		   AND NOT EXISTS(SELECT 1 FROM message_hidden h WHERE h.message_id = m.id AND h.user_id = $4)
		 ORDER BY m.created_at DESC LIMIT $2 OFFSET $3`, convID, limit, offset, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load messages")
		return
	}
	defer rows.Close()
	type msg struct {
		ID            string           `json:"id"`
		SenderID      string           `json:"sender_id"`
		Sender        string           `json:"sender_name"`
		Body          string           `json:"body"`
		MediaURL      string           `json:"media_url"`
		IsEncrypted   bool             `json:"is_encrypted"`
		ReplyTo       string           `json:"reply_to"`
		ForwardedFrom string           `json:"forwarded_from"`
		StoryID       string           `json:"story_id"`
		Pinned        bool             `json:"pinned"`
		CreatedAt     time.Time        `json:"created_at"`
		EditedAt      *time.Time       `json:"edited_at"`
		ExpiresAt     *time.Time       `json:"expires_at"`
		Reactions     map[string]int64 `json:"reactions"`
	}
	out := []msg{}
	for rows.Next() {
		var m msg
		var reactions []byte
		if err := rows.Scan(&m.ID, &m.SenderID, &m.Sender, &m.Body, &m.MediaURL, &m.IsEncrypted,
			&m.ReplyTo, &m.CreatedAt, &m.EditedAt, &m.ForwardedFrom, &m.StoryID, &m.Pinned,
			&reactions, &m.ExpiresAt); err == nil {
			m.Reactions = map[string]int64{}
			_ = json.Unmarshal(reactions, &m.Reactions)
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}
