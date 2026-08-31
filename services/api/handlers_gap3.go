package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Gap pack 3: privacy suite depth (presence/phone granularity, data saver,
// account self-destruct TTL, safety mode), sessions management, X parity
// (reply policy enforcement, hidden replies, creator comment pinning, lists,
// bookmark folders, paid verification flow), TikTok parity (playlists,
// profile visitors), Telegram parity (sticker packs, chat folders, archive,
// public group handles, granular group-admin permissions).

var handleRe = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

// ---------- Reply policy (X "who can reply") ----------

func (a *App) checkReplyPolicy(ctx context.Context, postID, uid, body string) error {
	var authorID, policy string
	if err := a.db.QueryRow(ctx,
		`SELECT author_id, reply_policy FROM posts WHERE id=$1 AND deleted_at IS NULL`,
		postID).Scan(&authorID, &policy); err != nil {
		return errors.New("post not found")
	}
	if uid == authorID || policy == "everyone" {
		return nil
	}
	switch policy {
	case "nobody":
		return errors.New("replies are disabled for this post")
	case "following":
		var ok bool
		_ = a.db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND followee_id=$2)`,
			authorID, uid).Scan(&ok)
		if ok {
			return nil
		}
	case "mentioned":
		var ok bool
		_ = a.db.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM post_tags WHERE post_id=$1 AND user_id=$2)`,
			postID, uid).Scan(&ok)
		if ok {
			return nil
		}
		for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
			var uname string
			if err := a.db.QueryRow(ctx,
				`SELECT username::text FROM users WHERE id=$1`, authorID).Scan(&uname); err == nil &&
				strings.EqualFold(uname, m[1]) {
				return nil
			}
		}
	}
	return errors.New("the author limited who can reply to this post")
}

// ---------- Privacy settings ----------

// GET /api/me/privacy — presence/phone visibility, data saver, safety mode.
func (a *App) handleGetPrivacy(w http.ResponseWriter, r *http.Request) {
	var lastSeen, phone string
	var dataSaver, safety bool
	var ttl int
	if err := a.db.QueryRow(r.Context(),
		`SELECT last_seen_privacy, phone_privacy, data_saver, safety_mode, account_ttl_days
		 FROM users WHERE id=$1`, userIDFrom(r)).
		Scan(&lastSeen, &phone, &dataSaver, &safety, &ttl); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load privacy settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"last_seen_privacy": lastSeen, "phone_privacy": phone,
		"data_saver": dataSaver, "safety_mode": safety, "account_ttl_days": ttl,
	})
}

// PUT /api/me/privacy — update any subset of the privacy settings.
func (a *App) handleSetPrivacy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LastSeenPrivacy *string `json:"last_seen_privacy"` // everyone|contacts|nobody
		PhonePrivacy    *string `json:"phone_privacy"`     // everyone|contacts|nobody
		DataSaver       *bool   `json:"data_saver"`
		SafetyMode      *bool   `json:"safety_mode"`
		AccountTTLDays  *int    `json:"account_ttl_days"` // 0=never, else 30..730
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	validVis := func(v string) bool { return v == "everyone" || v == "contacts" || v == "nobody" }
	uid := userIDFrom(r)
	if req.LastSeenPrivacy != nil {
		if !validVis(*req.LastSeenPrivacy) {
			writeErr(w, http.StatusBadRequest, "invalid last_seen_privacy")
			return
		}
		a.db.Exec(r.Context(), `UPDATE users SET last_seen_privacy=$1 WHERE id=$2`, *req.LastSeenPrivacy, uid)
	}
	if req.PhonePrivacy != nil {
		if !validVis(*req.PhonePrivacy) {
			writeErr(w, http.StatusBadRequest, "invalid phone_privacy")
			return
		}
		a.db.Exec(r.Context(), `UPDATE users SET phone_privacy=$1 WHERE id=$2`, *req.PhonePrivacy, uid)
	}
	if req.DataSaver != nil {
		a.db.Exec(r.Context(), `UPDATE users SET data_saver=$1 WHERE id=$2`, *req.DataSaver, uid)
	}
	if req.SafetyMode != nil {
		a.db.Exec(r.Context(), `UPDATE users SET safety_mode=$1 WHERE id=$2`, *req.SafetyMode, uid)
	}
	if req.AccountTTLDays != nil {
		v := *req.AccountTTLDays
		if v != 0 && v != 30 && v != 90 && v != 180 && v != 365 && v != 730 {
			writeErr(w, http.StatusBadRequest, "account_ttl_days must be 0, 30, 90, 180, 365 or 730")
			return
		}
		a.db.Exec(r.Context(), `UPDATE users SET account_ttl_days=$1 WHERE id=$2`, v, uid)
	}
	a.handleGetPrivacy(w, r)
}

// startAccountTTLWorker auto-deletes accounts whose owner has been inactive
// longer than their configured TTL (Telegram "delete my account if away").
func (a *App) startAccountTTLWorker() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			_, _ = a.db.Exec(context.Background(),
				`UPDATE users SET status='deleted', updated_at=now()
				 WHERE status='active' AND account_ttl_days > 0
				   AND COALESCE(last_seen_at, created_at) + make_interval(days => account_ttl_days) < now()`)
		}
	}()
}

// ---------- Sessions management ----------

// GET /api/me/sessions — active login sessions (device list).
func (a *App) handleListSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, COALESCE(user_agent,''), COALESCE(ip::text,''), created_at, expires_at
		 FROM sessions WHERE user_id=$1 AND revoked_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load sessions")
		return
	}
	defer rows.Close()
	type sess struct {
		ID        string    `json:"id"`
		UserAgent string    `json:"user_agent"`
		IP        string    `json:"ip"`
		CreatedAt time.Time `json:"created_at"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	out := []sess{}
	for rows.Next() {
		var s sess
		if err := rows.Scan(&s.ID, &s.UserAgent, &s.IP, &s.CreatedAt, &s.ExpiresAt); err == nil {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

// DELETE /api/me/sessions/{id} — revoke one of your own sessions.
func (a *App) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE sessions SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// ---------- Archive chats ----------

// PUT /api/conversations/{id}/archive — archive (or with DELETE unarchive)
// a conversation for yourself only.
func (a *App) handleArchiveConversation(w http.ResponseWriter, r *http.Request) {
	uid, convID := userIDFrom(r), r.PathValue("id")
	res, err := a.db.Exec(r.Context(),
		`UPDATE conversation_members SET archived_at=now() WHERE conversation_id=$1 AND user_id=$2`,
		convID, uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"archived": true})
}

func (a *App) handleUnarchiveConversation(w http.ResponseWriter, r *http.Request) {
	uid, convID := userIDFrom(r), r.PathValue("id")
	res, err := a.db.Exec(r.Context(),
		`UPDATE conversation_members SET archived_at=NULL WHERE conversation_id=$1 AND user_id=$2`,
		convID, uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"archived": false})
}

// ---------- Sticker packs (self-hosted; no third-party catalog) ----------

// POST /api/sticker-packs — create a sticker pack you own.
func (a *App) handleCreateStickerPack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if !handleRe.MatchString(req.Name) {
		writeErr(w, http.StatusBadRequest, "name must be 3-32 chars [a-z0-9_]")
		return
	}
	if strings.TrimSpace(req.Title) == "" || len(req.Title) > 80 {
		writeErr(w, http.StatusBadRequest, "title required (80 chars max)")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO sticker_packs (owner_id, name, title) VALUES ($1,$2,$3) RETURNING id`,
		userIDFrom(r), req.Name, strings.TrimSpace(req.Title)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "pack name taken")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "name": req.Name})
}

// GET /api/sticker-packs — browse packs with sticker counts.
func (a *App) handleListStickerPacks(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.name, p.title, u.username::text,
		        (SELECT count(*) FROM stickers s WHERE s.pack_id = p.id)
		 FROM sticker_packs p JOIN users u ON u.id = p.owner_id
		 ORDER BY p.created_at DESC LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load packs")
		return
	}
	defer rows.Close()
	type pack struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Title    string `json:"title"`
		Owner    string `json:"owner"`
		Stickers int    `json:"sticker_count"`
	}
	out := []pack{}
	for rows.Next() {
		var p pack
		if err := rows.Scan(&p.ID, &p.Name, &p.Title, &p.Owner, &p.Stickers); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"packs": out})
}

// POST /api/sticker-packs/{id}/stickers — add a sticker to your own pack.
func (a *App) handleAddSticker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Emoji    string `json:"emoji"`
		MediaURL string `json:"media_url"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.MediaURL) == "" {
		writeErr(w, http.StatusBadRequest, "media_url required")
		return
	}
	packID := r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT owner_id FROM sticker_packs WHERE id=$1`, packID).Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, "pack not found")
		return
	}
	if owner != userIDFrom(r) {
		writeErr(w, http.StatusForbidden, "only the pack owner can add stickers")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO stickers (pack_id, emoji, media_url, position)
		 VALUES ($1,$2,$3,(SELECT COALESCE(max(position),0)+1 FROM stickers WHERE pack_id=$1))
		 RETURNING id`, packID, req.Emoji, req.MediaURL).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add sticker")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/sticker-packs/{id}/stickers — list a pack's stickers.
func (a *App) handleListStickers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, emoji, media_url, position FROM stickers WHERE pack_id=$1 ORDER BY position`,
		r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load stickers")
		return
	}
	defer rows.Close()
	type sticker struct {
		ID       string `json:"id"`
		Emoji    string `json:"emoji"`
		MediaURL string `json:"media_url"`
		Position int    `json:"position"`
	}
	out := []sticker{}
	for rows.Next() {
		var s sticker
		if err := rows.Scan(&s.ID, &s.Emoji, &s.MediaURL, &s.Position); err == nil {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stickers": out})
}

// POST /api/conversations/{id}/sticker — send a sticker as a chat message.
func (a *App) handleSendSticker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StickerID string `json:"sticker_id"`
	}
	if !decodeJSON(w, r, &req) || req.StickerID == "" {
		writeErr(w, http.StatusBadRequest, "sticker_id required")
		return
	}
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var mediaURL string
	if err := a.db.QueryRow(r.Context(),
		`SELECT media_url FROM stickers WHERE id=$1`, req.StickerID).Scan(&mediaURL); err != nil {
		writeErr(w, http.StatusNotFound, "sticker not found")
		return
	}
	var msgID string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, media_url, kind)
		 VALUES ($1,$2,'',$3,'sticker') RETURNING id`, convID, uid, mediaURL).Scan(&msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to send sticker")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "conversation_id": convID, "message_id": msgID,
		"kind": "sticker", "media_url": mediaURL, "sender_id": uid,
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusCreated, map[string]string{"id": msgID})
}

// ---------- Chat folders ----------

// POST /api/me/chat-folders — create a folder; PUT replaces its members via
// /api/me/chat-folders/{id}/conversations.
func (a *App) handleCreateChatFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Name) == "" || len(req.Name) > 40 {
		writeErr(w, http.StatusBadRequest, "name required (40 chars max)")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO chat_folders (user_id, name) VALUES ($1,$2) RETURNING id`,
		userIDFrom(r), strings.TrimSpace(req.Name)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "folder name exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/me/chat-folders — folders with their conversation ids.
func (a *App) handleListChatFolders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT f.id, f.name,
		        COALESCE(array_agg(i.conversation_id) FILTER (WHERE i.conversation_id IS NOT NULL), '{}')
		 FROM chat_folders f
		 LEFT JOIN chat_folder_items i ON i.folder_id = f.id
		 WHERE f.user_id=$1 GROUP BY f.id ORDER BY f.created_at`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load folders")
		return
	}
	defer rows.Close()
	type folder struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Conversations []string `json:"conversation_ids"`
	}
	out := []folder{}
	for rows.Next() {
		var f folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Conversations); err == nil {
			out = append(out, f)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

// DELETE /api/me/chat-folders/{id}
func (a *App) handleDeleteChatFolder(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM chat_folders WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "folder not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PUT /api/me/chat-folders/{id}/conversations — replace the folder's members.
func (a *App) handleSetChatFolderConversations(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ConversationIDs []string `json:"conversation_ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, folderID := userIDFrom(r), r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT user_id FROM chat_folders WHERE id=$1`, folderID).Scan(&owner); err != nil || owner != uid {
		writeErr(w, http.StatusNotFound, "folder not found")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update folder")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `DELETE FROM chat_folder_items WHERE folder_id=$1`, folderID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update folder")
		return
	}
	for _, cid := range req.ConversationIDs {
		// only conversations the user actually belongs to
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO chat_folder_items (folder_id, conversation_id)
			 SELECT $1, $2::uuid WHERE EXISTS(
			   SELECT 1 FROM conversation_members WHERE conversation_id=$2::uuid AND user_id=$3)
			 ON CONFLICT DO NOTHING`, folderID, cid, uid); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid conversation id")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update folder")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------- Lists (curated user groups with their own feed) ----------

// POST /api/me/lists
func (a *App) handleCreateList(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Name) == "" || len(req.Name) > 60 {
		writeErr(w, http.StatusBadRequest, "name required (60 chars max)")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO user_lists (owner_id, name) VALUES ($1,$2) RETURNING id`,
		userIDFrom(r), strings.TrimSpace(req.Name)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "list name exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/me/lists
func (a *App) handleMyLists(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT l.id, l.name, (SELECT count(*) FROM user_list_members m WHERE m.list_id=l.id)
		 FROM user_lists l WHERE l.owner_id=$1 ORDER BY l.created_at`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load lists")
		return
	}
	defer rows.Close()
	type list struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Members int    `json:"member_count"`
	}
	out := []list{}
	for rows.Next() {
		var l list
		if err := rows.Scan(&l.ID, &l.Name, &l.Members); err == nil {
			out = append(out, l)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": out})
}

// DELETE /api/me/lists/{id}
func (a *App) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM user_lists WHERE id=$1 AND owner_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "list not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *App) ownList(ctx context.Context, listID, uid string) bool {
	var owner string
	if err := a.db.QueryRow(ctx, `SELECT owner_id FROM user_lists WHERE id=$1`, listID).Scan(&owner); err != nil {
		return false
	}
	return owner == uid
}

// PUT /api/lists/{id}/members/{uid} — add a user to your list.
func (a *App) handleListAddMember(w http.ResponseWriter, r *http.Request) {
	listID, uid := r.PathValue("id"), r.PathValue("uid")
	if !a.ownList(r.Context(), listID, userIDFrom(r)) {
		writeErr(w, http.StatusNotFound, "list not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO user_list_members (list_id, user_id) VALUES ($1,$2::uuid) ON CONFLICT DO NOTHING`,
		listID, uid); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// DELETE /api/lists/{id}/members/{uid}
func (a *App) handleListRemoveMember(w http.ResponseWriter, r *http.Request) {
	listID, uid := r.PathValue("id"), r.PathValue("uid")
	if !a.ownList(r.Context(), listID, userIDFrom(r)) {
		writeErr(w, http.StatusNotFound, "list not found")
		return
	}
	a.db.Exec(r.Context(), `DELETE FROM user_list_members WHERE list_id=$1 AND user_id=$2`, listID, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// GET /api/lists/{id}/feed — latest posts from list members.
func (a *App) handleListFeed(w http.ResponseWriter, r *http.Request) {
	uid, listID := userIDFrom(r), r.PathValue("id")
	if !a.ownList(r.Context(), listID, uid) {
		writeErr(w, http.StatusNotFound, "list not found")
		return
	}
	limit, offset := pageParams(r)
	posts, err := a.scanPosts(r.Context(), postSelect+`
		WHERE p.deleted_at IS NULL AND p.publish_at IS NULL
		  AND p.author_id IN (SELECT user_id FROM user_list_members WHERE list_id=$2)
		ORDER BY p.created_at DESC LIMIT $3 OFFSET $4`, uid, listID, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load list feed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// ---------- Bookmark folders ----------

// POST /api/me/bookmark-folders
func (a *App) handleCreateBookmarkFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Name) == "" || len(req.Name) > 60 {
		writeErr(w, http.StatusBadRequest, "name required (60 chars max)")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO bookmark_folders (user_id, name) VALUES ($1,$2) RETURNING id`,
		userIDFrom(r), strings.TrimSpace(req.Name)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "folder name exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/me/bookmark-folders
func (a *App) handleListBookmarkFolders(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT f.id, f.name, (SELECT count(*) FROM bookmarks b WHERE b.folder_id=f.id)
		 FROM bookmark_folders f WHERE f.user_id=$1 ORDER BY f.created_at`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load folders")
		return
	}
	defer rows.Close()
	type folder struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Count int    `json:"bookmark_count"`
	}
	out := []folder{}
	for rows.Next() {
		var f folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Count); err == nil {
			out = append(out, f)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

// DELETE /api/me/bookmark-folders/{id} — bookmarks fall back to unfiled.
func (a *App) handleDeleteBookmarkFolder(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM bookmark_folders WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "folder not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PUT /api/bookmarks/{postId}/folder — file a bookmark into a folder
// (folder_id empty string = unfiled).
func (a *App) handleBookmarkSetFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FolderID string `json:"folder_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if req.FolderID != "" && !a.ownBookmarkFolder(r.Context(), req.FolderID, uid) {
		writeErr(w, http.StatusNotFound, "folder not found")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE bookmarks SET folder_id=NULLIF($1,'')::uuid WHERE user_id=$2 AND post_id=$3`,
		req.FolderID, uid, r.PathValue("postId"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "bookmark not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) ownBookmarkFolder(ctx context.Context, folderID, uid string) bool {
	var owner string
	if err := a.db.QueryRow(ctx,
		`SELECT user_id FROM bookmark_folders WHERE id=$1`, folderID).Scan(&owner); err != nil {
		return false
	}
	return owner == uid
}

// ---------- Profile visitors ("who viewed me") ----------

func (a *App) recordProfileView(ctx context.Context, profileID, viewerID string) {
	if profileID == "" || viewerID == "" || profileID == viewerID {
		return
	}
	_, _ = a.db.Exec(ctx,
		`INSERT INTO profile_views (profile_id, viewer_id, viewed_at) VALUES ($1,$2,now())
		 ON CONFLICT (profile_id, viewer_id) DO UPDATE SET viewed_at=now()`, profileID, viewerID)
}

// GET /api/me/profile-visitors — recent viewers of your profile.
func (a *App) handleProfileVisitors(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username::text, u.display_name, u.avatar_url, v.viewed_at
		 FROM profile_views v JOIN users u ON u.id = v.viewer_id
		 WHERE v.profile_id=$1 ORDER BY v.viewed_at DESC LIMIT 100`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load visitors")
		return
	}
	defer rows.Close()
	type visitor struct {
		ID       string    `json:"id"`
		Username string    `json:"username"`
		Name     string    `json:"display_name"`
		Avatar   string    `json:"avatar_url"`
		ViewedAt time.Time `json:"viewed_at"`
	}
	out := []visitor{}
	for rows.Next() {
		var v visitor
		if err := rows.Scan(&v.ID, &v.Username, &v.Name, &v.Avatar, &v.ViewedAt); err == nil {
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"visitors": out})
}

// ---------- Playlists (creator-curated reel collections) ----------

// POST /api/me/playlists
func (a *App) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" || len(req.Title) > 80 {
		writeErr(w, http.StatusBadRequest, "title required (80 chars max)")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO playlists (owner_id, title) VALUES ($1,$2) RETURNING id`,
		userIDFrom(r), strings.TrimSpace(req.Title)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "playlist title exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/me/playlists — also used for GET /api/users/{id}/playlists via helper.
func (a *App) listPlaylistsFor(w http.ResponseWriter, r *http.Request, ownerID string) {
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.title, (SELECT count(*) FROM playlist_items i WHERE i.playlist_id=p.id)
		 FROM playlists p WHERE p.owner_id=$1 ORDER BY p.created_at`, ownerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load playlists")
		return
	}
	defer rows.Close()
	type pl struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Items int    `json:"item_count"`
	}
	out := []pl{}
	for rows.Next() {
		var p pl
		if err := rows.Scan(&p.ID, &p.Title, &p.Items); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": out})
}

func (a *App) handleMyPlaylists(w http.ResponseWriter, r *http.Request) {
	a.listPlaylistsFor(w, r, userIDFrom(r))
}

func (a *App) handleUserPlaylists(w http.ResponseWriter, r *http.Request) {
	a.listPlaylistsFor(w, r, r.PathValue("id"))
}

// DELETE /api/me/playlists/{id}
func (a *App) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM playlists WHERE id=$1 AND owner_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "playlist not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/playlists/{id}/items — add one of your own posts to your playlist.
func (a *App) handlePlaylistAddItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PostID string `json:"post_id"`
	}
	if !decodeJSON(w, r, &req) || req.PostID == "" {
		writeErr(w, http.StatusBadRequest, "post_id required")
		return
	}
	uid, plID := userIDFrom(r), r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(), `SELECT owner_id FROM playlists WHERE id=$1`, plID).Scan(&owner); err != nil || owner != uid {
		writeErr(w, http.StatusNotFound, "playlist not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO playlist_items (playlist_id, post_id)
		 SELECT $1, $2::uuid WHERE EXISTS(SELECT 1 FROM posts WHERE id=$2::uuid AND author_id=$3)
		 ON CONFLICT DO NOTHING`, plID, req.PostID, uid); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid post id")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// DELETE /api/playlists/{id}/items/{postId}
func (a *App) handlePlaylistRemoveItem(w http.ResponseWriter, r *http.Request) {
	uid, plID := userIDFrom(r), r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(), `SELECT owner_id FROM playlists WHERE id=$1`, plID).Scan(&owner); err != nil || owner != uid {
		writeErr(w, http.StatusNotFound, "playlist not found")
		return
	}
	a.db.Exec(r.Context(), `DELETE FROM playlist_items WHERE playlist_id=$1 AND post_id=$2`,
		plID, r.PathValue("postId"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// GET /api/playlists/{id} — the playlist's posts in order.
func (a *App) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
	uid, plID := userIDFrom(r), r.PathValue("id")
	var title string
	if err := a.db.QueryRow(r.Context(), `SELECT title FROM playlists WHERE id=$1`, plID).Scan(&title); err != nil {
		writeErr(w, http.StatusNotFound, "playlist not found")
		return
	}
	posts, err := a.scanPosts(r.Context(), postSelect+`
		JOIN playlist_items i ON i.post_id = p.id AND i.playlist_id = $2
		WHERE p.deleted_at IS NULL
		ORDER BY i.position, p.created_at DESC LIMIT 50`, uid, plID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load playlist")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": plID, "title": title, "posts": posts})
}

// ---------- Paid verification flow ----------

// POST /api/me/verification-requests — request a verified badge.
func (a *App) handleRequestVerification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tier string `json:"tier"` // blue | org
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Tier == "" {
		req.Tier = "blue"
	}
	if req.Tier != "blue" && req.Tier != "org" {
		writeErr(w, http.StatusBadRequest, "tier must be blue or org")
		return
	}
	uid := userIDFrom(r)
	var pending bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM verification_requests WHERE user_id=$1 AND status='pending')`, uid).
		Scan(&pending)
	if pending {
		writeErr(w, http.StatusConflict, "a verification request is already pending")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO verification_requests (user_id, tier, note) VALUES ($1,$2,$3) RETURNING id`,
		uid, req.Tier, strings.TrimSpace(req.Note)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to request verification")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "pending"})
}

// GET /api/me/verification-requests — your own request history.
func (a *App) handleMyVerificationRequests(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, tier, status, note, created_at, reviewed_at
		 FROM verification_requests WHERE user_id=$1 ORDER BY created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load requests")
		return
	}
	defer rows.Close()
	type reqOut struct {
		ID         string     `json:"id"`
		Tier       string     `json:"tier"`
		Status     string     `json:"status"`
		Note       string     `json:"note"`
		CreatedAt  time.Time  `json:"created_at"`
		ReviewedAt *time.Time `json:"reviewed_at"`
	}
	out := []reqOut{}
	for rows.Next() {
		var v reqOut
		if err := rows.Scan(&v.ID, &v.Tier, &v.Status, &v.Note, &v.CreatedAt, &v.ReviewedAt); err == nil {
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// GET /api/admin/verification-requests?status=pending
func (a *App) handleAdminListVerification(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT v.id, u.username::text, v.tier, v.status, v.note, v.created_at
		 FROM verification_requests v JOIN users u ON u.id = v.user_id
		 WHERE v.status=$1 ORDER BY v.created_at LIMIT 200`, status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load requests")
		return
	}
	defer rows.Close()
	type reqOut struct {
		ID        string    `json:"id"`
		Username  string    `json:"username"`
		Tier      string    `json:"tier"`
		Status    string    `json:"status"`
		Note      string    `json:"note"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []reqOut{}
	for rows.Next() {
		var v reqOut
		if err := rows.Scan(&v.ID, &v.Username, &v.Tier, &v.Status, &v.Note, &v.CreatedAt); err == nil {
			out = append(out, v)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// POST /api/admin/verification-requests/{id}/review — approve sets
// users.is_verified; audit-logged.
func (a *App) handleAdminReviewVerification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"` // approved | rejected
	}
	if !decodeJSON(w, r, &req) || (req.Decision != "approved" && req.Decision != "rejected") {
		writeErr(w, http.StatusBadRequest, "decision must be approved or rejected")
		return
	}
	reqID := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "review failed")
		return
	}
	defer tx.Rollback(r.Context())
	var userID, status string
	if err := tx.QueryRow(r.Context(),
		`SELECT user_id, status FROM verification_requests WHERE id=$1 FOR UPDATE`, reqID).
		Scan(&userID, &status); err != nil {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	if status != "pending" {
		writeErr(w, http.StatusConflict, "request already reviewed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE verification_requests SET status=$1, reviewed_by=$2, reviewed_at=now() WHERE id=$3`,
		req.Decision, userIDFrom(r), reqID); err != nil {
		writeErr(w, http.StatusInternalServerError, "review failed")
		return
	}
	if req.Decision == "approved" {
		if _, err := tx.Exec(r.Context(),
			`UPDATE users SET is_verified=TRUE, updated_at=now() WHERE id=$1`, userID); err != nil {
			writeErr(w, http.StatusInternalServerError, "review failed")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "review failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "verification_"+req.Decision, userID, nil)
	a.notify(r.Context(), userID, "verification",
		"Verification "+req.Decision, "Your verification request was "+req.Decision,
		map[string]any{"request_id": reqID, "decision": req.Decision})
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Decision})
}

// ---------- Hidden replies + creator comment pinning ----------

// POST /api/comments/{id}/hide — the post author (or the comment author)
// hides a reply; hidden replies stay visible to both.
func (a *App) handleHideComment(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	res, err := a.db.Exec(r.Context(),
		`UPDATE comments SET hidden_at=now()
		 WHERE id=$1 AND deleted_at IS NULL
		   AND (author_id=$2 OR EXISTS(SELECT 1 FROM posts p WHERE p.id=comments.post_id AND p.author_id=$2))`,
		r.PathValue("id"), uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusForbidden, "cannot hide this comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "hidden"})
}

// POST /api/comments/{id}/unhide
func (a *App) handleUnhideComment(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	res, err := a.db.Exec(r.Context(),
		`UPDATE comments SET hidden_at=NULL
		 WHERE id=$1 AND (author_id=$2 OR EXISTS(SELECT 1 FROM posts p WHERE p.id=comments.post_id AND p.author_id=$2))`,
		r.PathValue("id"), uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusForbidden, "cannot unhide this comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "visible"})
}

// PUT /api/posts/{id}/pinned-comment — the post author pins one comment to
// the top (TikTok creator pinning); empty comment_id clears the pin.
func (a *App) handlePinComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CommentID string `json:"comment_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, postID := userIDFrom(r), r.PathValue("id")
	var author string
	if err := a.db.QueryRow(r.Context(),
		`SELECT author_id FROM posts WHERE id=$1 AND deleted_at IS NULL`, postID).Scan(&author); err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	if author != uid {
		writeErr(w, http.StatusForbidden, "only the author can pin comments")
		return
	}
	if req.CommentID != "" {
		var ok bool
		_ = a.db.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM comments WHERE id=$1 AND post_id=$2 AND deleted_at IS NULL)`,
			req.CommentID, postID).Scan(&ok)
		if !ok {
			writeErr(w, http.StatusNotFound, "comment not found on this post")
			return
		}
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE posts SET pinned_comment_id=NULLIF($1,'')::uuid WHERE id=$2`, req.CommentID, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to pin comment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "pinned_comment_id": req.CommentID})
}

// ---------- Public group handles (t.me-style) ----------

// PUT /api/conversations/{id}/handle — owner sets a public @handle for a
// group or channel; empty string clears it.
func (a *App) handleSetGroupHandle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle string `json:"handle"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, convID := userIDFrom(r), r.PathValue("id")
	var role string
	var isGroup bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT m.role, c.is_group FROM conversation_members m
		 JOIN conversations c ON c.id = m.conversation_id
		 WHERE m.conversation_id=$1 AND m.user_id=$2`, convID, uid).Scan(&role, &isGroup); err != nil {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	if role != "owner" {
		writeErr(w, http.StatusForbidden, "only the owner can set the handle")
		return
	}
	h := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(req.Handle, "@")))
	if h != "" && !handleRe.MatchString(h) {
		writeErr(w, http.StatusBadRequest, "handle must be 3-32 chars [a-z0-9_]")
		return
	}
	var resErr error
	if h == "" {
		_, resErr = a.db.Exec(r.Context(), `UPDATE conversations SET handle=NULL WHERE id=$1`, convID)
	} else {
		_, resErr = a.db.Exec(r.Context(), `UPDATE conversations SET handle=$1 WHERE id=$2`, h, convID)
	}
	if resErr != nil {
		writeErr(w, http.StatusConflict, "handle taken")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"handle": h})
}

// GET /api/handles/{handle} — public preview of a group/channel by handle.
func (a *App) handleGetByHandle(w http.ResponseWriter, r *http.Request) {
	h := strings.ToLower(strings.TrimPrefix(r.PathValue("handle"), "@"))
	var id, title, desc string
	var isGroup, isChannel bool
	var members int64
	err := a.db.QueryRow(r.Context(),
		`SELECT c.id, c.title, COALESCE(c.description,''), c.is_group, c.is_channel,
		        (SELECT count(*) FROM conversation_members m WHERE m.conversation_id=c.id)
		 FROM conversations c WHERE c.handle=$1`, h).
		Scan(&id, &title, &desc, &isGroup, &isChannel, &members)
	if err != nil {
		writeErr(w, http.StatusNotFound, "handle not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "title": title, "description": desc,
		"is_group": isGroup, "is_channel": isChannel, "member_count": members,
	})
}

// POST /api/handles/{handle}/join — join a public group by its handle.
func (a *App) handleJoinByHandle(w http.ResponseWriter, r *http.Request) {
	h := strings.ToLower(strings.TrimPrefix(r.PathValue("handle"), "@"))
	uid := userIDFrom(r)
	var convID string
	var isChannel bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT id, is_channel FROM conversations WHERE handle=$1`, h).
		Scan(&convID, &isChannel); err != nil {
		writeErr(w, http.StatusNotFound, "handle not found")
		return
	}
	role := "member"
	if isChannel {
		role = "member"
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO conversation_members (conversation_id, user_id, role)
		 VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, convID, uid, role); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to join")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"conversation_id": convID})
}

// ---------- Granular group-admin permissions ----------

// PUT /api/conversations/{id}/members/{uid}/permissions — the owner grants
// per-permission flags to an admin (invite/delete/pin/manage_members).
func (a *App) handleSetMemberPermissions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CanInvite        bool `json:"can_invite"`
		CanDelete        bool `json:"can_delete"`
		CanPin           bool `json:"can_pin"`
		CanManageMembers bool `json:"can_manage_members"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, convID, target := userIDFrom(r), r.PathValue("id"), r.PathValue("uid")
	var role string
	if err := a.db.QueryRow(r.Context(),
		`SELECT role FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`,
		convID, uid).Scan(&role); err != nil || role != "owner" {
		writeErr(w, http.StatusForbidden, "only the owner can set permissions")
		return
	}
	perms, _ := json.Marshal(map[string]bool{
		"can_invite": req.CanInvite, "can_delete": req.CanDelete,
		"can_pin": req.CanPin, "can_manage_members": req.CanManageMembers,
	})
	res, err := a.db.Exec(r.Context(),
		`UPDATE conversation_members SET perms=$1 WHERE conversation_id=$2 AND user_id=$3 AND role='admin'`,
		perms, convID, target)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "admin not found (promote to admin first)")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// hasMemberPerm reports whether uid holds a granular admin permission in a
// conversation. Owners hold every permission implicitly.
func (a *App) hasMemberPerm(ctx context.Context, convID, uid, perm string) bool {
	var role string
	var perms []byte
	if err := a.db.QueryRow(ctx,
		`SELECT role, COALESCE(perms::text,'') FROM conversation_members
		 WHERE conversation_id=$1 AND user_id=$2`, convID, uid).Scan(&role, &perms); err != nil {
		return false
	}
	if role == "owner" {
		return true
	}
	if role != "admin" {
		return false
	}
	var m map[string]bool
	if json.Unmarshal(perms, &m) != nil {
		return false
	}
	return m[perm]
}

// PUT /api/conversations/{id}/members/{uid}/role — the owner promotes a
// member to admin or demotes an admin back to member.
func (a *App) handleSetMemberRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"` // admin | member
	}
	if !decodeJSON(w, r, &req) || (req.Role != "admin" && req.Role != "member") {
		writeErr(w, http.StatusBadRequest, "role must be admin or member")
		return
	}
	uid, convID, target := userIDFrom(r), r.PathValue("id"), r.PathValue("uid")
	var role string
	if err := a.db.QueryRow(r.Context(),
		`SELECT role FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`,
		convID, uid).Scan(&role); err != nil || role != "owner" {
		writeErr(w, http.StatusForbidden, "only the owner can change roles")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE conversation_members SET role=$3 WHERE conversation_id=$1 AND user_id=$2 AND role <> 'owner'`,
		convID, target, req.Role)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "member not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "role": req.Role})
}
