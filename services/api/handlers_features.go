package main

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// fanoutToMembers pushes a JSON payload to all members of a conversation.
func (a *App) fanoutToMembers(ctx context.Context, convID string, payload []byte, exceptUser string) {
	rows, err := a.db.Query(ctx,
		`SELECT user_id FROM conversation_members WHERE conversation_id = $1`, convID)
	if err != nil {
		return
	}
	defer rows.Close()
	var members []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err == nil && uid != exceptUser {
			members = append(members, uid)
		}
	}
	for _, uid := range members {
		a.hub.sendTo(uid, payload)
	}
	publishRelay(members, payload)
}

func (a *App) messageConv(ctx context.Context, msgID string) (convID, senderID string, ok bool) {
	err := a.db.QueryRow(ctx,
		`SELECT conversation_id, sender_id FROM messages WHERE id=$1 AND deleted_at IS NULL`, msgID).
		Scan(&convID, &senderID)
	return convID, senderID, err == nil
}

// ---- Message edit / delete ----

func (a *App) handleEditMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, senderID, ok := a.messageConv(r.Context(), msgID)
	if !ok || senderID != uid {
		writeErr(w, http.StatusForbidden, "cannot edit this message")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE messages SET body=$1, edited_at=now() WHERE id=$2`, req.Body, msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to edit message")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message_edited", "conversation_id": convID, "id": msgID, "body": req.Body,
	})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "edited"})
}

func (a *App) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, senderID, ok := a.messageConv(r.Context(), msgID)
	if !ok {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	// Sender, or owner/admin of the conversation, may delete.
	if senderID != uid {
		var role string
		_ = a.db.QueryRow(r.Context(),
			`SELECT role FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`,
			convID, uid).Scan(&role)
		if role != "owner" && role != "admin" {
			writeErr(w, http.StatusForbidden, "cannot delete this message")
			return
		}
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE messages SET deleted_at=now() WHERE id=$1`, msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to delete message")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message_deleted", "conversation_id": convID, "id": msgID,
	})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- Message reactions ----

var emojiRe = regexp.MustCompile(`^.{1,8}$`)

func (a *App) handleReactMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Emoji string `json:"emoji"`
	}
	if !decodeJSON(w, r, &req) || !emojiRe.MatchString(req.Emoji) {
		writeErr(w, http.StatusBadRequest, "valid emoji required")
		return
	}
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, _, ok := a.messageConv(r.Context(), msgID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		msgID, uid, req.Emoji); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to react")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "reaction", "conversation_id": convID, "message_id": msgID,
		"user_id": uid, "emoji": req.Emoji, "action": "add",
	})
	a.fanoutToMembers(r.Context(), convID, payload, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "reacted"})
}

func (a *App) handleUnreactMessage(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	emoji := r.URL.Query().Get("emoji")
	uid := userIDFrom(r)
	convID, _, ok := a.messageConv(r.Context(), msgID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM message_reactions WHERE message_id=$1 AND user_id=$2 AND emoji=$3`, msgID, uid, emoji)
	payload, _ := json.Marshal(map[string]any{
		"type": "reaction", "conversation_id": convID, "message_id": msgID,
		"user_id": uid, "emoji": emoji, "action": "remove",
	})
	a.fanoutToMembers(r.Context(), convID, payload, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ---- Read receipts ----

func (a *App) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO conversation_reads (conversation_id, user_id, last_read_at) VALUES ($1,$2,now())
		 ON CONFLICT (conversation_id, user_id) DO UPDATE SET last_read_at=now()`, convID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to mark read")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "read", "conversation_id": convID, "user_id": uid, "at": time.Now().UTC(),
	})
	a.fanoutToMembers(r.Context(), convID, payload, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleReadState(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT cr.user_id, u.display_name, cr.last_read_at
		 FROM conversation_reads cr JOIN users u ON u.id = cr.user_id
		 WHERE cr.conversation_id = $1`, convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load read state")
		return
	}
	defer rows.Close()
	type readState struct {
		UserID string    `json:"user_id"`
		Name   string    `json:"name"`
		At     time.Time `json:"at"`
	}
	out := []readState{}
	for rows.Next() {
		var s readState
		if err := rows.Scan(&s.UserID, &s.Name, &s.At); err == nil {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reads": out})
}

// ---- Presence ----

func (a *App) handlePresence(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("id")
	a.hub.mu.RLock()
	online := len(a.hub.clients[target]) > 0
	a.hub.mu.RUnlock()
	var lastSeen *time.Time
	var showActive bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT last_seen_at, show_active_status FROM users WHERE id=$1`, target).Scan(&lastSeen, &showActive)
	if !showActive {
		online = false
		lastSeen = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"online": online, "last_seen": lastSeen})
}

// ---- Group & channel management ----

func (a *App) handleAddMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	var role string
	var isGroup bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT m.role, c.is_group FROM conversation_members m
		 JOIN conversations c ON c.id = m.conversation_id
		 WHERE m.conversation_id=$1 AND m.user_id=$2`, convID, uid).Scan(&role, &isGroup); err != nil {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	if !isGroup || (role != "owner" && role != "admin") {
		writeErr(w, http.StatusForbidden, "only group admins can add members")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1,$2,'member') ON CONFLICT DO NOTHING`,
		convID, req.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

func (a *App) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	target := r.PathValue("uid")
	uid := userIDFrom(r)
	var role string
	if err := a.db.QueryRow(r.Context(),
		`SELECT role FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`,
		convID, uid).Scan(&role); err != nil {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	// Members may leave themselves; removing others needs owner/admin.
	if target != uid && role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "only admins can remove members")
		return
	}
	var targetRole string
	_ = a.db.QueryRow(r.Context(),
		`SELECT role FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`,
		convID, target).Scan(&targetRole)
	if targetRole == "owner" {
		writeErr(w, http.StatusForbidden, "the owner cannot be removed")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`, convID, target)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// Channels: anyone can subscribe; only owner/admin can post (enforced in the
// WS message path).
func (a *App) handleChannelSubscribe(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	var isChannel bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT is_channel FROM conversations WHERE id=$1`, convID).Scan(&isChannel); err != nil || !isChannel {
		writeErr(w, http.StatusNotFound, "channel not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO conversation_members (conversation_id, user_id, role) VALUES ($1,$2,'member') ON CONFLICT DO NOTHING`,
		convID, userIDFrom(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to subscribe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "subscribed"})
}

func (a *App) handleChannelUnsubscribe(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	var role string
	_ = a.db.QueryRow(r.Context(),
		`SELECT role FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`, convID, uid).Scan(&role)
	if role == "owner" {
		writeErr(w, http.StatusForbidden, "the owner cannot unsubscribe")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`, convID, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

func (a *App) handleListChannels(w http.ResponseWriter, r *http.Request) {
	q := "%" + strings.ToLower(r.URL.Query().Get("q")) + "%"
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id, c.title, c.description,
		        (SELECT count(*) FROM conversation_members m WHERE m.conversation_id = c.id) AS members,
		        EXISTS(SELECT 1 FROM conversation_members m WHERE m.conversation_id = c.id AND m.user_id = $2)
		 FROM conversations c
		 WHERE c.is_channel = true AND (lower(c.title) LIKE $1 OR lower(c.description) LIKE $1)
		 ORDER BY members DESC LIMIT 50`, q, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	defer rows.Close()
	type ch struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Members     int64  `json:"members"`
		Joined      bool   `json:"joined"`
	}
	out := []ch{}
	for rows.Next() {
		var c ch
		if err := rows.Scan(&c.ID, &c.Title, &c.Description, &c.Members, &c.Joined); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

// ---- Polls ----

func (a *App) handleVotePoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OptionID string `json:"option_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	postID := r.PathValue("id")
	uid := userIDFrom(r)
	var valid bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM poll_options WHERE id=$1 AND post_id=$2)`, req.OptionID, postID).Scan(&valid)
	if !valid {
		writeErr(w, http.StatusBadRequest, "invalid option")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO poll_votes (option_id, user_id, post_id) VALUES ($1,$2,$3)
		 ON CONFLICT (post_id, user_id) DO UPDATE SET option_id = EXCLUDED.option_id, created_at = now()`,
		req.OptionID, uid, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to vote")
		return
	}
	a.respondPoll(w, r, postID, uid)
}

func (a *App) handleGetPoll(w http.ResponseWriter, r *http.Request) {
	a.respondPoll(w, r, r.PathValue("id"), userIDFrom(r))
}

func (a *App) respondPoll(w http.ResponseWriter, r *http.Request, postID, uid string) {
	rows, err := a.db.Query(r.Context(),
		`SELECT o.id, o.label,
		        (SELECT count(*) FROM poll_votes v WHERE v.option_id = o.id),
		        EXISTS(SELECT 1 FROM poll_votes v WHERE v.option_id = o.id AND v.user_id = $2)
		 FROM poll_options o WHERE o.post_id = $1 ORDER BY o.idx`, postID, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load poll")
		return
	}
	defer rows.Close()
	type opt struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Votes int64  `json:"votes"`
		Voted bool   `json:"voted_by_me"`
	}
	out := []opt{}
	var total int64
	for rows.Next() {
		var o opt
		if err := rows.Scan(&o.ID, &o.Label, &o.Votes, &o.Voted); err == nil {
			total += o.Votes
			out = append(out, o)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"options": out, "total_votes": total})
}

// ---- Hashtags & trending ----

var hashtagRe = regexp.MustCompile(`#([\p{L}\p{N}_]{2,50})`)

func extractHashtags(body string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range hashtagRe.FindAllStringSubmatch(body, -1) {
		tag := strings.ToLower(m[1])
		if !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

func (a *App) handleTrending(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT tag, use_count FROM hashtags
		 WHERE last_used > now() - interval '7 days'
		 ORDER BY use_count DESC LIMIT 20`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load trending")
		return
	}
	defer rows.Close()
	type tag struct {
		Tag   string `json:"tag"`
		Count int64  `json:"count"`
	}
	out := []tag{}
	for rows.Next() {
		var t tag
		if err := rows.Scan(&t.Tag, &t.Count); err == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"trending": out})
}

func (a *App) handleHashtagPosts(w http.ResponseWriter, r *http.Request) {
	tag := strings.ToLower(r.PathValue("tag"))
	limit, offset := pageParams(r)
	posts, err := a.scanPosts(r.Context(), postSelect+`
		JOIN post_hashtags ph ON ph.post_id = p.id
		WHERE ph.tag = $2 AND p.visibility = 'public' AND (p.expires_at IS NULL OR p.expires_at > now())
		ORDER BY p.created_at DESC LIMIT $3 OFFSET $4`, userIDFrom(r), tag, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load posts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// ---- Bookmarks ----

func (a *App) handleBookmark(w http.ResponseWriter, r *http.Request) {
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO bookmarks (user_id, post_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		userIDFrom(r), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to bookmark")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "bookmarked"})
}

func (a *App) handleUnbookmark(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM bookmarks WHERE user_id=$1 AND post_id=$2`, userIDFrom(r), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (a *App) handleListBookmarks(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	posts, err := a.scanPosts(r.Context(), postSelect+`
		JOIN bookmarks b ON b.post_id = p.id AND b.user_id = $1
		ORDER BY b.created_at DESC LIMIT $2 OFFSET $3`, userIDFrom(r), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load bookmarks")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// ---- Reposts (X-style) ----

func (a *App) handleRepost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quote string `json:"quote"`
	}
	_ = decodeJSON(w, r, &req) // quote optional
	origID := r.PathValue("id")
	uid := userIDFrom(r)
	var exists bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND visibility='public')`, origID).Scan(&exists)
	if !exists {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO posts (author_id, type, body, visibility, repost_of)
		 VALUES ($1,'post',$2,'public',$3) RETURNING id`, uid, req.Quote, origID).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to repost")
		return
	}
	_, _ = a.db.Exec(r.Context(), `UPDATE posts SET share_count = share_count + 1 WHERE id=$1`, origID)
	var authorID string
	if err := a.db.QueryRow(r.Context(), `SELECT author_id FROM posts WHERE id=$1`, origID).Scan(&authorID); err == nil && authorID != uid {
		payload, _ := json.Marshal(map[string]string{"post_id": origID, "actor_id": uid})
		_, _ = a.db.Exec(r.Context(),
			`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'repost',$2)`, authorID, payload)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// ---- Privacy: blocks ----

func (a *App) handleBlock(w http.ResponseWriter, r *http.Request) {
	uid, target := userIDFrom(r), r.PathValue("id")
	if uid == target {
		writeErr(w, http.StatusBadRequest, "cannot block yourself")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to block")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, target); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to block")
		return
	}
	// Blocking severs the follow relationship in both directions.
	_, _ = tx.Exec(r.Context(),
		`DELETE FROM follows WHERE (follower_id=$1 AND followee_id=$2) OR (follower_id=$2 AND followee_id=$1)`, uid, target)
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to block")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "blocked"})
}

func (a *App) handleUnblock(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM user_blocks WHERE blocker_id=$1 AND blocked_id=$2`, userIDFrom(r), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "unblocked"})
}

func (a *App) isBlockedEither(ctx context.Context, aID, bID string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM user_blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`,
		aID, bID).Scan(&ok)
	return ok
}

// ---- Creator monetization ----

// creatorBalance computes lifetime earnings from content views at the
// configured RPM (revenue per 1000 views), minus approved/paid payouts.
func (a *App) creatorBalance(ctx context.Context, creatorID string) (earned, paidOut, available float64, err error) {
	err = a.db.QueryRow(ctx,
		`SELECT COALESCE(sum(view_count),0) * $2 / 1000.0 FROM posts WHERE author_id=$1 AND type IN ('post','reel')`,
		creatorID, a.cfg.CreatorRPM).Scan(&earned)
	if err != nil {
		return
	}
	err = a.db.QueryRow(ctx,
		`SELECT COALESCE(sum(amount),0) FROM payout_requests WHERE creator_id=$1 AND status IN ('approved','paid')`,
		creatorID).Scan(&paidOut)
	available = earned - paidOut
	if available < 0 {
		available = 0
	}
	return
}

func (a *App) handleCreatorEarnings(w http.ResponseWriter, r *http.Request) {
	earned, paidOut, available, err := a.creatorBalance(r.Context(), userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to compute earnings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"earned": earned, "paid_out": paidOut, "available": available, "currency": "USD",
	})
}

func (a *App) handleCreatorPayout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount      float64 `json:"amount"`
		Asset       string  `json:"asset"`
		Destination string  `json:"destination"`
	}
	if !decodeJSON(w, r, &req) || req.Amount <= 0 {
		writeErr(w, http.StatusBadRequest, "positive amount required")
		return
	}
	if req.Asset == "" {
		req.Asset = "USD"
	}
	uid := userIDFrom(r)
	var kyc string
	_ = a.db.QueryRow(r.Context(), `SELECT kyc_status FROM users WHERE id=$1`, uid).Scan(&kyc)
	if kyc != "verified" {
		writeErr(w, http.StatusForbidden, "KYC verification required for payouts")
		return
	}
	_, _, available, err := a.creatorBalance(r.Context(), uid)
	if err != nil || req.Amount > available {
		writeErr(w, http.StatusBadRequest, "amount exceeds available balance")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO payout_requests (creator_id, amount, asset, destination) VALUES ($1,$2,$3,$4) RETURNING id`,
		uid, req.Amount, req.Asset, req.Destination).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to request payout")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleCreatorPayouts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, amount, asset, status, destination, created_at FROM payout_requests
		 WHERE creator_id=$1 ORDER BY created_at DESC LIMIT 50`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load payouts")
		return
	}
	defer rows.Close()
	type payout struct {
		ID          string    `json:"id"`
		Amount      float64   `json:"amount"`
		Asset       string    `json:"asset"`
		Status      string    `json:"status"`
		Destination string    `json:"destination"`
		CreatedAt   time.Time `json:"created_at"`
	}
	out := []payout{}
	for rows.Next() {
		var p payout
		if err := rows.Scan(&p.ID, &p.Amount, &p.Asset, &p.Status, &p.Destination, &p.CreatedAt); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"payouts": out})
}

func (a *App) handleAdminListPayouts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, u.username, p.amount, p.asset, p.destination, p.created_at
		 FROM payout_requests p JOIN users u ON u.id = p.creator_id
		 WHERE p.status='pending' ORDER BY p.created_at LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load payouts")
		return
	}
	defer rows.Close()
	type payout struct {
		ID          string    `json:"id"`
		Creator     string    `json:"creator"`
		Amount      float64   `json:"amount"`
		Asset       string    `json:"asset"`
		Destination string    `json:"destination"`
		CreatedAt   time.Time `json:"created_at"`
	}
	out := []payout{}
	for rows.Next() {
		var p payout
		if err := rows.Scan(&p.ID, &p.Creator, &p.Amount, &p.Asset, &p.Destination, &p.CreatedAt); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"payouts": out})
}

func (a *App) handleAdminReviewPayout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"` // approved | paid | rejected
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Decision != "approved" && req.Decision != "paid" && req.Decision != "rejected" {
		writeErr(w, http.StatusBadRequest, "decision must be approved, paid or rejected")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE payout_requests SET status=$1, reviewed_by=$2, reviewed_at=now() WHERE id=$3 AND status IN ('pending','approved')`,
		req.Decision, userIDFrom(r), r.PathValue("id"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "payout not found or already finalized")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "payout_review:"+req.Decision, r.PathValue("id"), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Decision})
}
