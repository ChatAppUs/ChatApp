package main

// Privacy suite depth: mutes, keyword filters, restricted list, profile lock
// (follow requests), active-status control, and the message-request inbox.

import (
	"net/http"
	"strings"
	"time"
)

// ---- mutes (hide content without unfollow/block) ----

func (a *App) handleMute(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("id")
	uid := userIDFrom(r)
	if target == uid {
		writeErr(w, http.StatusBadRequest, "cannot mute yourself")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO user_mutes (user_id, muted_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		uid, target); err != nil {
		writeErr(w, http.StatusInternalServerError, "mute failed")
		return
	}
	a.invalidateFYP(r.Context(), uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "muted"})
}

func (a *App) handleUnmute(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM user_mutes WHERE user_id=$1 AND muted_id=$2`, userIDFrom(r), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "unmuted"})
}

func (a *App) handleListMutes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username, u.display_name, u.avatar_url, m.created_at
		 FROM user_mutes m JOIN users u ON u.id=m.muted_id
		 WHERE m.user_id=$1 ORDER BY m.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load mutes")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uname, dname, avatar string
		var createdAt time.Time
		if err := rows.Scan(&id, &uname, &dname, &avatar, &createdAt); err == nil {
			out = append(out, map[string]any{
				"id": id, "username": uname, "display_name": dname, "avatar_url": avatar, "muted_at": createdAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mutes": out})
}

// ---- word filters ----

func (a *App) handleAddWordFilter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phrase string `json:"phrase"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Phrase = strings.TrimSpace(req.Phrase)
	if len(req.Phrase) < 2 || len(req.Phrase) > 100 {
		writeErr(w, http.StatusBadRequest, "phrase must be 2-100 characters")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO word_filters (user_id, phrase) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		userIDFrom(r), req.Phrase); err != nil {
		writeErr(w, http.StatusInternalServerError, "filter failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (a *App) handleRemoveWordFilter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phrase string `json:"phrase"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Phrase) == "" {
		writeErr(w, http.StatusBadRequest, "phrase required")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM word_filters WHERE user_id=$1 AND phrase=$2`, userIDFrom(r), req.Phrase)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (a *App) handleListWordFilters(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT phrase, created_at FROM word_filters WHERE user_id=$1 ORDER BY created_at DESC`,
		userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load filters")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var phrase string
		var createdAt time.Time
		if err := rows.Scan(&phrase, &createdAt); err == nil {
			out = append(out, map[string]any{"phrase": phrase, "created_at": createdAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"filters": out})
}

// ---- restricted list ----

func (a *App) handleRestrict(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("id")
	uid := userIDFrom(r)
	if target == uid {
		writeErr(w, http.StatusBadRequest, "cannot restrict yourself")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO restricted_list (user_id, restricted_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		uid, target); err != nil {
		writeErr(w, http.StatusInternalServerError, "restrict failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restricted"})
}

func (a *App) handleUnrestrict(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM restricted_list WHERE user_id=$1 AND restricted_id=$2`,
		userIDFrom(r), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "unrestricted"})
}

func (a *App) handleListRestricted(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username, u.display_name, u.avatar_url, rl.created_at
		 FROM restricted_list rl JOIN users u ON u.id=rl.restricted_id
		 WHERE rl.user_id=$1 ORDER BY rl.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load list")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uname, dname, avatar string
		var createdAt time.Time
		if err := rows.Scan(&id, &uname, &dname, &avatar, &createdAt); err == nil {
			out = append(out, map[string]any{
				"id": id, "username": uname, "display_name": dname, "avatar_url": avatar, "restricted_at": createdAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"restricted": out})
}

// ---- profile lock + active status ----

func (a *App) handleSetProfileLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Locked bool `json:"locked"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET profile_locked=$2 WHERE id=$1`, userIDFrom(r), req.Locked); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"profile_locked": req.Locked})
}

func (a *App) handleSetActiveStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Show bool `json:"show"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET show_active_status=$2 WHERE id=$1`, userIDFrom(r), req.Show); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"show_active_status": req.Show})
}

// ---- follow requests (profile lock) ----

func (a *App) handleFollowRequests(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username, u.display_name, u.avatar_url, fr.created_at
		 FROM follow_requests fr JOIN users u ON u.id=fr.follower_id
		 WHERE fr.followee_id=$1 ORDER BY fr.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load requests")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uname, dname, avatar string
		var createdAt time.Time
		if err := rows.Scan(&id, &uname, &dname, &avatar, &createdAt); err == nil {
			out = append(out, map[string]any{
				"id": id, "username": uname, "display_name": dname, "avatar_url": avatar, "requested_at": createdAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func (a *App) handleAcceptFollowRequest(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	follower := r.PathValue("uid")
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM follow_requests WHERE followee_id=$1 AND follower_id=$2`, uid, follower)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO follows (follower_id, followee_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		follower, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "follow failed")
		return
	}
	a.notify(r.Context(), follower, "follow_accepted", "Follow request accepted",
		"Your follow request was accepted", map[string]any{"by": uid})
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (a *App) handleDeclineFollowRequest(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM follow_requests WHERE followee_id=$1 AND follower_id=$2`,
		userIDFrom(r), r.PathValue("uid"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "declined"})
}

// ---- message requests inbox ----

func (a *App) handleMessageRequests(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT mr.conversation_id, u.username, u.display_name, u.avatar_url, mr.created_at,
		        (SELECT m.body FROM messages m WHERE m.conversation_id=mr.conversation_id
		          AND m.deleted_at IS NULL ORDER BY m.created_at DESC LIMIT 1)
		 FROM message_requests mr
		 JOIN conversation_members cm ON cm.conversation_id=mr.conversation_id AND cm.user_id<>mr.recipient_id
		 JOIN users u ON u.id=cm.user_id
		 WHERE mr.recipient_id=$1 AND mr.status='pending'
		 ORDER BY mr.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load requests")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var convID, uname, dname, avatar string
		var createdAt time.Time
		var preview *string
		if err := rows.Scan(&convID, &uname, &dname, &avatar, &createdAt, &preview); err == nil {
			out = append(out, map[string]any{
				"conversation_id": convID, "username": uname, "display_name": dname,
				"avatar_url": avatar, "requested_at": createdAt, "preview": preview,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func (a *App) handleAcceptMessageRequest(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID := r.PathValue("convId")
	tag, err := a.db.Exec(r.Context(),
		`UPDATE message_requests SET status='accepted', responded_at=now()
		 WHERE conversation_id=$1 AND recipient_id=$2 AND status='pending'`, convID, uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (a *App) handleDeclineMessageRequest(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID := r.PathValue("convId")
	tag, err := a.db.Exec(r.Context(),
		`UPDATE message_requests SET status='declined', responded_at=now()
		 WHERE conversation_id=$1 AND recipient_id=$2 AND status='pending'`, convID, uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	// Declining hides the conversation: remove membership silently.
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`, convID, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "declined"})
}
