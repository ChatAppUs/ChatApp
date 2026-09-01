package main

// Telegram-style bot platform. A bot is a normal user account (it can be
// added to conversations and receive messages) plus a bots row with a token.
// The bot API authenticates by token instead of JWT: getUpdates (long-poll)
// and sendMessage reusing the same message pipeline. Optionally a webhook
// URL receives each update as a signed POST (HMAC with webhook_secret).

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ---- management (JWT-authed, bot owner) ----

func (a *App) handleCreateBot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	if !strings.HasSuffix(req.Username, "bot") || len(req.Username) < 4 || len(req.Username) > 32 {
		writeErr(w, http.StatusBadRequest, "bot username must end in 'bot' (4-32 chars)")
		return
	}
	for _, c := range req.Username {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			writeErr(w, http.StatusBadRequest, "username may contain a-z, 0-9, underscore")
			return
		}
	}
	if req.DisplayName = strings.TrimSpace(req.DisplayName); req.DisplayName == "" {
		req.DisplayName = req.Username
	}
	uid := userIDFrom(r)

	// Bot accounts use an unguessable random password: bots never log in
	// through the user auth plane, only via their API token.
	rawPw, err := a.randomNum(32)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "bot creation failed")
		return
	}
	pwHash, err := a.passwordHash(rawPw)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "bot creation failed")
		return
	}
	var botUserID string
	err = a.db.QueryRow(r.Context(),
		`INSERT INTO users (username, password_hash, display_name) VALUES ($1,$2,$3) RETURNING id`,
		req.Username, pwHash, req.DisplayName).Scan(&botUserID)
	if err != nil {
		writeErr(w, http.StatusConflict, "username already taken")
		return
	}
	token, err := a.randomNum(24)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "bot creation failed")
		return
	}
	token = strconv.FormatInt(time.Now().Unix(), 36) + ":" + token
	var botID string
	err = a.db.QueryRow(r.Context(),
		`INSERT INTO bots (user_id, owner_id, token_hash, description) VALUES ($1,$2,$3,$4) RETURNING id`,
		botUserID, uid, sha256hex(token), req.Description).Scan(&botID)
	if err != nil {
		_, _ = a.db.Exec(r.Context(), `DELETE FROM users WHERE id=$1`, botUserID)
		writeErr(w, http.StatusInternalServerError, "bot creation failed")
		return
	}
	// The token is shown exactly once; only its hash is stored.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": botID, "user_id": botUserID, "username": req.Username, "token": token,
	})
}

func (a *App) handleMyBots(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT b.id, u.username, u.display_name, b.description, b.webhook_url <> '',
		        b.mini_app_url, b.active, b.created_at
		 FROM bots b JOIN users u ON u.id=b.user_id
		 WHERE b.owner_id=$1 ORDER BY b.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load bots")
		return
	}
	defer rows.Close()
	bots := []map[string]any{}
	for rows.Next() {
		var id, uname, dname, desc, miniApp string
		var hasWebhook, active bool
		var createdAt time.Time
		if err := rows.Scan(&id, &uname, &dname, &desc, &hasWebhook, &miniApp, &active, &createdAt); err != nil {
			continue
		}
		bots = append(bots, map[string]any{
			"id": id, "username": uname, "display_name": dname, "description": desc,
			"has_webhook": hasWebhook, "mini_app_url": miniApp, "active": active, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": bots})
}

func (a *App) handleDeleteBot(w http.ResponseWriter, r *http.Request) {
	var botUserID string
	err := a.db.QueryRow(r.Context(),
		`DELETE FROM bots WHERE id=$1 AND owner_id=$2 RETURNING user_id`,
		r.PathValue("id"), userIDFrom(r)).Scan(&botUserID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "bot not found")
		return
	}
	_, _ = a.db.Exec(r.Context(), `DELETE FROM users WHERE id=$1`, botUserID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *App) handleSetWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL    string `json:"url"` // empty clears the webhook (back to long-poll)
		Secret string `json:"secret"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.URL != "" && !strings.HasPrefix(req.URL, "https://") {
		writeErr(w, http.StatusBadRequest, "webhook URL must be https")
		return
	}
	if len(req.URL) > 2048 || len(req.Secret) > 128 {
		writeErr(w, http.StatusBadRequest, "value too long")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE bots SET webhook_url=$3, webhook_secret=$4 WHERE id=$1 AND owner_id=$2`,
		r.PathValue("id"), userIDFrom(r), req.URL, req.Secret)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "bot not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (a *App) handleSetMiniApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Icon  string `json:"icon_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 60 || !strings.HasPrefix(req.URL, "https://") {
		writeErr(w, http.StatusBadRequest, "title required and url must be https")
		return
	}
	botID := r.PathValue("id")
	uid := userIDFrom(r)
	var owns bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM bots WHERE id=$1 AND owner_id=$2)`, botID, uid).Scan(&owns); err != nil || !owns {
		writeErr(w, http.StatusNotFound, "bot not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO mini_apps (bot_id, title, url, icon_url) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (bot_id, title) DO UPDATE SET url=$3, icon_url=$4`,
		botID, req.Title, req.URL, req.Icon); err != nil {
		writeErr(w, http.StatusInternalServerError, "mini-app save failed")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE bots SET mini_app_url=$2 WHERE id=$1`, botID, req.URL); err != nil {
		writeErr(w, http.StatusInternalServerError, "mini-app save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// GET /api/miniapps — public directory of bot-owned mini apps (Telegram Mini
// Apps launcher).
func (a *App) handleListMiniApps(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT ma.id::text, u.username, ma.title, ma.url, ma.icon_url
		 FROM mini_apps ma JOIN bots b ON b.id=ma.bot_id JOIN users u ON u.id=b.user_id
		 WHERE b.active ORDER BY ma.created_at DESC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "list failed")
		return
	}
	defer rows.Close()
	apps := []map[string]any{}
	for rows.Next() {
		var id, bot, title, url, icon string
		if err := rows.Scan(&id, &bot, &title, &url, &icon); err != nil {
			continue
		}
		apps = append(apps, map[string]any{"id": id, "bot": bot, "title": title, "url": url, "icon_url": icon})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
}

// ---- bot API (token-authed) ----

// botFromToken resolves a bot by its API token (hash-compared).
func (a *App) botFromToken(ctx context.Context, token string) (botID, botUserID string, ok bool) {
	err := a.db.QueryRow(ctx,
		`SELECT id, user_id FROM bots WHERE token_hash=$1 AND active`, sha256hex(token)).Scan(&botID, &botUserID)
	return botID, botUserID, err == nil
}

// handleBotGetUpdates long-polls the bot's update inbox. offset = last seen
// update id + 1; waits up to timeout seconds for new updates.
func (a *App) handleBotGetUpdates(w http.ResponseWriter, r *http.Request) {
	botID, _, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	timeout, _ := strconv.Atoi(r.URL.Query().Get("timeout"))
	if timeout < 0 || timeout > 50 {
		timeout = 25
	}
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		rows, err := a.db.Query(r.Context(),
			`SELECT id, kind, payload, created_at FROM bot_updates
			 WHERE bot_id=$1 AND id >= $2 ORDER BY id LIMIT 100`, botID, offset)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to load updates")
			return
		}
		updates := []map[string]any{}
		var maxID int64
		for rows.Next() {
			var id int64
			var kind string
			var payload map[string]any
			var createdAt time.Time
			if err := rows.Scan(&id, &kind, &payload, &createdAt); err != nil {
				continue
			}
			if id > maxID {
				maxID = id
			}
			updates = append(updates, map[string]any{
				"update_id": id, "type": kind, "payload": payload, "created_at": createdAt,
			})
		}
		rows.Close()
		if len(updates) > 0 || time.Now().After(deadline) || r.Context().Err() != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": updates})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// handleBotSendMessage lets a bot post into any conversation it is a member of.
func (a *App) handleBotSendMessage(w http.ResponseWriter, r *http.Request) {
	_, botUserID, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	var req struct {
		ConversationID string `json:"conversation_id"`
		Body           string `json:"body"`
		MediaURL       string `json:"media_url"`
		ReplyTo        string `json:"reply_to"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" && req.MediaURL == "" {
		writeErr(w, http.StatusBadRequest, "body or media_url required")
		return
	}
	if len(req.Body) > 4096 {
		writeErr(w, http.StatusBadRequest, "message too long")
		return
	}
	if !a.isMember(r.Context(), req.ConversationID, botUserID) {
		writeErr(w, http.StatusForbidden, "bot is not a member of this conversation")
		return
	}
	a.persistAndFanout(r.Context(), botUserID, req.ConversationID, req.Body, req.MediaURL, false, req.ReplyTo)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleBotGetMe(w http.ResponseWriter, r *http.Request) {
	_, botUserID, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	var uname, dname string
	if err := a.db.QueryRow(r.Context(),
		`SELECT username, display_name FROM users WHERE id=$1`, botUserID).Scan(&uname, &dname); err != nil {
		writeErr(w, http.StatusNotFound, "bot user missing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "result": map[string]any{"id": botUserID, "username": uname, "display_name": dname},
	})
}

// handleBotEditMessageText edits one of the bot's own messages (Telegram
// editMessageText parity).
func (a *App) handleBotEditMessage(w http.ResponseWriter, r *http.Request) {
	_, botUserID, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	var req struct {
		MessageID string `json:"message_id"`
		Body      string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" || len(req.Body) > 4096 {
		writeErr(w, http.StatusBadRequest, "body required (4096 chars max)")
		return
	}
	var convID string
	err := a.db.QueryRow(r.Context(),
		`SELECT conversation_id FROM messages WHERE id=$1 AND sender_id=$2 AND deleted_at IS NULL`,
		req.MessageID, botUserID).Scan(&convID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "message not found or not sent by this bot")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE messages SET body=$2, edited_at=now() WHERE id=$1`, req.MessageID, req.Body); err != nil {
		writeErr(w, http.StatusInternalServerError, "edit failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message_edited", "id": req.MessageID, "conversation_id": convID,
		"body": req.Body, "edited_at": time.Now(),
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBotDeleteMessage deletes one of the bot's own messages.
func (a *App) handleBotDeleteMessage(w http.ResponseWriter, r *http.Request) {
	_, botUserID, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	var req struct {
		MessageID string `json:"message_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var convID string
	err := a.db.QueryRow(r.Context(),
		`SELECT conversation_id FROM messages WHERE id=$1 AND sender_id=$2 AND deleted_at IS NULL`,
		req.MessageID, botUserID).Scan(&convID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "message not found or not sent by this bot")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE messages SET deleted_at=now() WHERE id=$1`, req.MessageID); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message_deleted", "id": req.MessageID, "conversation_id": convID,
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBotSendPhoto sends a photo/document message with an optional caption
// (Telegram sendPhoto parity; the media is uploaded via the media edge first).
func (a *App) handleBotSendPhoto(w http.ResponseWriter, r *http.Request) {
	_, botUserID, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	var req struct {
		ConversationID string `json:"conversation_id"`
		MediaURL       string `json:"media_url"`
		Caption        string `json:"caption"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.MediaURL = strings.TrimSpace(req.MediaURL)
	if !strings.HasPrefix(req.MediaURL, "/media/") || len(req.MediaURL) > 300 {
		writeErr(w, http.StatusBadRequest, "media_url must be an uploaded /media/ URL")
		return
	}
	if len(req.Caption) > 1024 {
		writeErr(w, http.StatusBadRequest, "caption too long")
		return
	}
	if !a.isMember(r.Context(), req.ConversationID, botUserID) {
		writeErr(w, http.StatusForbidden, "bot is not a member of this conversation")
		return
	}
	a.persistAndFanout(r.Context(), botUserID, req.ConversationID, req.Caption, req.MediaURL, false, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleBotGetChat returns conversation metadata for a conversation the bot
// belongs to (Telegram getChat parity).
func (a *App) handleBotGetChat(w http.ResponseWriter, r *http.Request) {
	_, botUserID, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	convID := r.URL.Query().Get("conversation_id")
	if !a.isMember(r.Context(), convID, botUserID) {
		writeErr(w, http.StatusForbidden, "bot is not a member of this conversation")
		return
	}
	var title string
	var isGroup, isChannel bool
	var members int
	if err := a.db.QueryRow(r.Context(),
		`SELECT COALESCE(c.title,''), c.is_group, c.is_channel,
			(SELECT COUNT(*) FROM conversation_members cm WHERE cm.conversation_id=c.id)
		 FROM conversations c WHERE c.id=$1`, convID).Scan(&title, &isGroup, &isChannel, &members); err != nil {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	kind := "private"
	if isChannel {
		kind = "channel"
	} else if isGroup {
		kind = "group"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": map[string]any{
		"id": convID, "title": title, "type": kind, "member_count": members,
	}})
}

// ---- update enqueue + webhook delivery ----

// enqueueBotUpdates records an update for every bot in the conversation.
// Called from persistAndFanout after a message is persisted.
func (a *App) enqueueBotUpdates(ctx context.Context, convID, senderID, msgID, body, mediaURL string) {
	payload, _ := json.Marshal(map[string]any{
		"message_id": msgID, "conversation_id": convID, "from_user": senderID,
		"body": body, "media_url": mediaURL,
	})
	_, _ = a.db.Exec(ctx,
		`INSERT INTO bot_updates (bot_id, kind, payload)
		 SELECT b.id, 'message', $2::jsonb
		 FROM bots b
		 JOIN conversation_members m ON m.user_id = b.user_id
		 WHERE m.conversation_id = $1 AND b.user_id <> $3 AND b.active`,
		convID, payload, senderID)
}

// startBotWebhookWorker delivers updates to configured webhooks. Long-poll
// bots simply leave rows in bot_updates for getUpdates; webhook bots get a
// signed POST and the row is considered delivered (deleted on 2xx).
func (a *App) startBotWebhookWorker() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.deliverBotWebhooks()
		}
	}()
}

func (a *App) deliverBotWebhooks() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := a.db.Query(ctx,
		`SELECT u.id, u.payload, b.webhook_url, b.webhook_secret
		 FROM bot_updates u JOIN bots b ON b.id = u.bot_id
		 WHERE b.webhook_url <> '' AND b.active
		 ORDER BY u.id LIMIT 200`)
	if err != nil {
		return
	}
	type delivery struct {
		id          int64
		payload     map[string]any
		url, secret string
	}
	var deliveries []delivery
	for rows.Next() {
		var d delivery
		if err := rows.Scan(&d.id, &d.payload, &d.url, &d.secret); err == nil {
			deliveries = append(deliveries, d)
		}
	}
	rows.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	for _, d := range deliveries {
		body, err := json.Marshal(map[string]any{"update_id": d.id, "type": "message", "payload": d.payload})
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if d.secret != "" {
			mac := hmac.New(sha256.New, []byte(d.secret))
			mac.Write(body)
			req.Header.Set("X-ChatApp-Signature", hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := client.Do(req)
		if err != nil {
			continue // stays queued for the next sweep
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_, _ = a.db.Exec(ctx, `DELETE FROM bot_updates WHERE id=$1`, d.id)
		}
	}
}
