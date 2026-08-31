package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Gap pack 4: closes the remaining implementable competitor gaps.
// X: drafts, topics, verified organizations, who-to-follow, audio rooms
// (scheduled/ticketed/recorded), premium plans, GIF catalog.
// Telegram: message entities (spoiler/bold/italic/mono/link), contact cards,
// channel discussion groups + stats, anonymous admins, bot invoices, inline
// bots, mini apps, people nearby.
// TikTok: sounds, share counter, paywalled series, restricted mode, content
// ratings, family pairing, interest vector, live-gift leaderboard, creator
// marketplace. Facebook: marketplace, fundraisers, professional dashboard.
// imo: XP/levels, room directory, chat export, screenshot alerts.

// ---------- Drafts (X/TikTok) ----------

// POST /api/me/drafts — save a server-side draft.
func (a *App) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type  string    `json:"type"`
		Body  string    `json:"body"`
		Media []mediaIn `json:"media"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	media, _ := json.Marshal(req.Media)
	if len(req.Media) == 0 {
		media = []byte("[]")
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO post_drafts (user_id, type, body, media)
		 VALUES ($1, COALESCE(NULLIF($2,''),'post'), $3, $4)
		 RETURNING id`, userIDFrom(r), req.Type, req.Body, media).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save draft")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/me/drafts — list your drafts, newest first.
func (a *App) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, type, body, media, updated_at FROM post_drafts
		 WHERE user_id=$1 ORDER BY updated_at DESC LIMIT 100`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load drafts")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, typ, body, media string
		var updated time.Time
		if err := rows.Scan(&id, &typ, &body, &media, &updated); err == nil {
			out = append(out, map[string]any{"id": id, "type": typ, "body": body,
				"media": json.RawMessage(media), "updated_at": updated})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": out})
}

// DELETE /api/me/drafts/{id}
func (a *App) handleDeleteDraft(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM post_drafts WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "draft not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- Topics / interests (X) ----------

// GET /api/topics — directory of interest topics with follower counts.
func (a *App) handleListInterestTopics(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT t.id, t.name,
		        (SELECT count(*) FROM topic_follows f WHERE f.topic_id=t.id),
		        EXISTS(SELECT 1 FROM topic_follows f WHERE f.topic_id=t.id AND f.user_id=$1)
		 FROM topics t ORDER BY t.name LIMIT 200`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load topics")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var followers int64
		var following bool
		if err := rows.Scan(&id, &name, &followers, &following); err == nil {
			out = append(out, map[string]any{"id": id, "name": name,
				"followers": followers, "following": following})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": out})
}

// POST /api/topics — create a topic (idempotent on name).
func (a *App) handleCreateTopic4(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.ToLower(strings.TrimSpace(req.Name))
	if len(name) < 2 || len(name) > 60 {
		writeErr(w, http.StatusBadRequest, "name must be 2-60 chars")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO topics (name) VALUES ($1) ON CONFLICT (name) DO UPDATE SET name=EXCLUDED.name
		 RETURNING id`, name).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create topic")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "name": name})
}

// POST/DELETE /api/topics/{id}/follow — follow/unfollow a topic.
func (a *App) handleFollowTopic(w http.ResponseWriter, r *http.Request) {
	uid, topicID := userIDFrom(r), r.PathValue("id")
	if r.Method == http.MethodDelete {
		a.db.Exec(r.Context(), `DELETE FROM topic_follows WHERE topic_id=$1 AND user_id=$2`, topicID, uid)
		writeJSON(w, http.StatusOK, map[string]bool{"following": false})
		return
	}
	res, err := a.db.Exec(r.Context(),
		`INSERT INTO topic_follows (topic_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, topicID, uid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "topic not found")
		return
	}
	_ = res
	writeJSON(w, http.StatusOK, map[string]bool{"following": true})
}

// ---------- Verified organizations (X) ----------

// POST /api/organizations — register an organization you own.
func (a *App) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Handle string `json:"handle"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(req.Handle), "@"))
	if !handleRe.MatchString(h) || strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "invalid name or handle")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO organizations (name, handle, owner_id) VALUES ($1,$2,$3) RETURNING id`,
		strings.TrimSpace(req.Name), h, userIDFrom(r)).Scan(&id); err != nil {
		writeErr(w, http.StatusConflict, "handle taken")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "handle": h})
}

// POST /api/organizations/{id}/members — the owner affiliates a user (by
// username or user_id); the user's profile gains the org badge
// (users.affiliated_org_id).
func (a *App) handleOrgAddMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
		Title    string `json:"title"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" && req.Username != "" {
		if err := a.db.QueryRow(r.Context(),
			`SELECT id FROM users WHERE username=$1 AND status='active'`,
			strings.ToLower(strings.TrimPrefix(strings.TrimSpace(req.Username), "@"))).Scan(&req.UserID); err != nil {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
	}
	if req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "user_id or username required")
		return
	}
	orgID := r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT owner_id FROM organizations WHERE id=$1`, orgID).Scan(&owner); err != nil || owner != userIDFrom(r) {
		writeErr(w, http.StatusForbidden, "only the org owner can add members")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO organization_members (org_id, user_id, title) VALUES ($1,$2,$3)
		 ON CONFLICT (org_id, user_id) DO UPDATE SET title=EXCLUDED.title`, orgID, req.UserID, req.Title); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add member")
		return
	}
	a.db.Exec(r.Context(), `UPDATE users SET affiliated_org_id=$1 WHERE id=$2`, orgID, req.UserID)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "affiliated"})
}

// DELETE /api/organizations/{id}/members/{uid}
func (a *App) handleOrgRemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID, target := r.PathValue("id"), r.PathValue("uid")
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT owner_id FROM organizations WHERE id=$1`, orgID).Scan(&owner); err != nil || owner != userIDFrom(r) {
		writeErr(w, http.StatusForbidden, "only the org owner can remove members")
		return
	}
	a.db.Exec(r.Context(), `DELETE FROM organization_members WHERE org_id=$1 AND user_id=$2`, orgID, target)
	a.db.Exec(r.Context(), `UPDATE users SET affiliated_org_id=NULL WHERE id=$1 AND affiliated_org_id=$2`, target, orgID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// GET /api/organizations/{id} — public org profile with member count.
func (a *App) handleGetOrg(w http.ResponseWriter, r *http.Request) {
	var id, name, handle string
	var verified bool
	var members int64
	err := a.db.QueryRow(r.Context(),
		`SELECT o.id, o.name, o.handle, o.is_verified,
		        (SELECT count(*) FROM organization_members m WHERE m.org_id=o.id)
		 FROM organizations o WHERE o.id::text=$1 OR o.handle=$1`, r.PathValue("id")).
		Scan(&id, &name, &handle, &verified, &members)
	if err != nil {
		writeErr(w, http.StatusNotFound, "organization not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name, "handle": handle,
		"is_verified": verified, "member_count": members})
}

// POST /api/admin/organizations/{id}/verify — superadmin grants the org badge.
func (a *App) handleAdminVerifyOrg(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE organizations SET is_verified=true WHERE id=$1`, r.PathValue("id"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "organization not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

// ---------- Who-to-follow suggestions (X) ----------

// GET /api/me/suggestions — followers-of-followers you don't follow yet,
// ranked by mutual count, then popular accounts.
func (a *App) handleWhoToFollow(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username::text, u.display_name, COALESCE(u.avatar_url,''), count(*) AS mutuals
		 FROM follows f1
		 JOIN follows f2 ON f2.follower_id = f1.followee_id
		 JOIN users u ON u.id = f2.followee_id
		 WHERE f1.follower_id = $1 AND f2.followee_id <> $1 AND u.status='active'
		   AND NOT EXISTS(SELECT 1 FROM follows f3 WHERE f3.follower_id=$1 AND f3.followee_id=f2.followee_id)
		 GROUP BY u.id, u.username, u.display_name, u.avatar_url
		 ORDER BY mutuals DESC LIMIT 20`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load suggestions")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uname, name, avatar string
		var mutuals int64
		if err := rows.Scan(&id, &uname, &name, &avatar, &mutuals); err == nil {
			out = append(out, map[string]any{"id": id, "username": uname, "display_name": name,
				"avatar_url": avatar, "mutual_follows": mutuals})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out})
}

// ---------- Audio rooms (X Spaces / TG voice chats / imo voice clubs) ----------

// POST /api/audio-rooms — create a room (optionally scheduled + ticketed).
func (a *App) handleCreateAudioRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string     `json:"title"`
		ScheduledAt *time.Time `json:"scheduled_at"`
		TicketPrice float64    `json:"ticket_price_usd"`
		IsPublic    *bool      `json:"is_public"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" {
		writeErr(w, http.StatusBadRequest, "title required")
		return
	}
	isPublic := true
	if req.IsPublic != nil {
		isPublic = *req.IsPublic
	}
	status := "scheduled"
	if req.ScheduledAt == nil || req.ScheduledAt.Before(time.Now()) {
		status = "scheduled"
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO audio_rooms (host_id, title, status, scheduled_at, ticket_price, is_public)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6) RETURNING id`,
		userIDFrom(r), strings.TrimSpace(req.Title), status, req.ScheduledAt,
		formatMoney(req.TicketPrice), isPublic).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create room")
		return
	}
	a.db.Exec(r.Context(),
		`INSERT INTO audio_room_participants (room_id, user_id, role) VALUES ($1,$2,'host')`,
		id, userIDFrom(r))
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": status})
}

// POST /api/audio-rooms/{id}/start — the host goes live.
func (a *App) handleStartAudioRoom(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE audio_rooms SET status='live', started_at=now()
		 WHERE id=$1 AND host_id=$2 AND status IN ('scheduled','ended')`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "room not found or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

// POST /api/audio-rooms/{id}/end — the host ends the room.
func (a *App) handleEndAudioRoom(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE audio_rooms SET status='ended', ended_at=now()
		 WHERE id=$1 AND host_id=$2 AND status='live'`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "room not live or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

// GET /api/audio-rooms/{id} — room detail with participant roster.
func (a *App) handleGetAudioRoom(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	var title, status string
	var host string
	var price float64
	var scheduled, started *time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT title, status, host_id, trim_scale(ticket_price)::float8, scheduled_at, started_at
		 FROM audio_rooms WHERE id=$1`, roomID).Scan(&title, &status, &host, &price, &scheduled, &started)
	if err != nil {
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT p.user_id, u.username::text, p.role, p.hand_raised
		 FROM audio_room_participants p JOIN users u ON u.id=p.user_id
		 WHERE p.room_id=$1 ORDER BY p.joined_at`, roomID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load participants")
		return
	}
	defer rows.Close()
	participants := []map[string]any{}
	for rows.Next() {
		var uid, uname, role string
		var hand bool
		if err := rows.Scan(&uid, &uname, &role, &hand); err == nil {
			participants = append(participants, map[string]any{
				"user_id": uid, "username": uname, "role": role, "hand_raised": hand})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": roomID, "title": title, "status": status, "host_id": host,
		"ticket_price_usd": price, "scheduled_at": scheduled, "started_at": started,
		"participants": participants})
}

// POST /api/audio-rooms/{id}/join — join as listener; ticketed rooms charge
// the joiner via the USD wallet and pay the host.
func (a *App) handleJoinAudioRoom(w http.ResponseWriter, r *http.Request) {
	uid, roomID := userIDFrom(r), r.PathValue("id")
	var host, status string
	var price float64
	if err := a.db.QueryRow(r.Context(),
		`SELECT host_id, status, trim_scale(ticket_price)::float8 FROM audio_rooms WHERE id=$1`,
		roomID).Scan(&host, &status, &price); err != nil {
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}
	if status == "ended" {
		writeErr(w, http.StatusConflict, "room has ended")
		return
	}
	if price > 0 && uid != host {
		var has bool
		_ = a.db.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM audio_room_tickets WHERE room_id=$1 AND user_id=$2)`,
			roomID, uid).Scan(&has)
		if !has {
			txID, err := a.moveUSD(r.Context(), uid, host, formatMoney(price),
				"space_ticket_send", "space_ticket_recv", "audio room ticket")
			if err != nil {
				writeErr(w, http.StatusPaymentRequired, "ticketed space: insufficient USD balance")
				return
			}
			a.db.Exec(r.Context(),
				`INSERT INTO audio_room_tickets (room_id, user_id, amount_usd, tx_id) VALUES ($1,$2,$3,$4)`,
				roomID, uid, price, txID)
		}
	}
	a.db.Exec(r.Context(),
		`INSERT INTO audio_room_participants (room_id, user_id, role) VALUES ($1,$2,'listener')
		 ON CONFLICT (room_id, user_id) DO NOTHING`, roomID, uid)
	ticket, err := a.sfuTicket("audio-"+roomID, uid, "subscriber")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ticket failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id": roomID, "role": "listener", "ticket": ticket,
		"sfu_url": a.cfg.SFUPublicURL, "ice_servers": a.iceServers(uid)})
}

// POST /api/audio-rooms/{id}/hand — raise/lower hand (listeners ask to speak).
func (a *App) handleRoomHand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Raised bool `json:"raised"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE audio_room_participants SET hand_raised=$3
		 WHERE room_id=$1 AND user_id=$2 AND role='listener'`,
		r.PathValue("id"), userIDFrom(r), req.Raised)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "join the room first")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"hand_raised": req.Raised})
}

// PUT /api/audio-rooms/{id}/speakers/{uid} — host promotes a listener to
// speaker or co-host (co-hosting lives); DELETE demotes back to listener.
func (a *App) handleRoomSpeaker(w http.ResponseWriter, r *http.Request) {
	roomID, target := r.PathValue("id"), r.PathValue("uid")
	var host string
	if err := a.db.QueryRow(r.Context(),
		`SELECT host_id FROM audio_rooms WHERE id=$1`, roomID).Scan(&host); err != nil || host != userIDFrom(r) {
		writeErr(w, http.StatusForbidden, "only the host manages speakers")
		return
	}
	if r.Method == http.MethodDelete {
		a.db.Exec(r.Context(),
			`UPDATE audio_room_participants SET role='listener', hand_raised=false
			 WHERE room_id=$1 AND user_id=$2 AND role IN ('speaker','cohost')`, roomID, target)
		writeJSON(w, http.StatusOK, map[string]string{"role": "listener"})
		return
	}
	var req struct {
		Role string `json:"role"` // speaker | cohost
	}
	if !decodeJSON(w, r, &req) || (req.Role != "speaker" && req.Role != "cohost") {
		writeErr(w, http.StatusBadRequest, "role must be speaker or cohost")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE audio_room_participants SET role=$3, hand_raised=false
		 WHERE room_id=$1 AND user_id=$2 AND role <> 'host'`, roomID, target, req.Role)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "participant not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"role": req.Role})
}

// GET /api/audio-rooms — directory of public live/scheduled rooms (imo voice
// club discovery + X Spaces browse).
func (a *App) handleDiscoverAudioRooms(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT r.id, r.title, r.status, r.scheduled_at, u.username::text,
		        (SELECT count(*) FROM audio_room_participants p WHERE p.room_id=r.id)
		 FROM audio_rooms r JOIN users u ON u.id=r.host_id
		 WHERE r.is_public AND r.status IN ('live','scheduled')
		 ORDER BY (r.status='live') DESC, r.scheduled_at NULLS LAST LIMIT 50`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load rooms")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, status, host string
		var scheduled *time.Time
		var count int64
		if err := rows.Scan(&id, &title, &status, &scheduled, &host, &count); err == nil {
			out = append(out, map[string]any{"id": id, "title": title, "status": status,
				"scheduled_at": scheduled, "host_username": host, "participants": count})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": out})
}

// ---------- Premium plans (X Premium / Telegram Premium) ----------

// GET /api/premium/plans — active subscription plans.
func (a *App) handlePremiumPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, name, trim_scale(price_usd)::float8, features FROM premium_plans WHERE active ORDER BY price_usd`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load plans")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, features string
		var price float64
		if err := rows.Scan(&id, &name, &price, &features); err == nil {
			out = append(out, map[string]any{"id": id, "name": name, "price_usd": price,
				"features": json.RawMessage(features)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": out})
}

// POST /api/premium/subscribe — subscribe to a plan with the USD wallet
// (30-day period); sets users.is_premium.
func (a *App) handlePremiumSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlanID string `json:"plan_id"`
	}
	if !decodeJSON(w, r, &req) || req.PlanID == "" {
		writeErr(w, http.StatusBadRequest, "plan_id required")
		return
	}
	uid := userIDFrom(r)
	var price float64
	if err := a.db.QueryRow(r.Context(),
		`SELECT trim_scale(price_usd)::float8 FROM premium_plans WHERE id=$1 AND active`,
		req.PlanID).Scan(&price); err != nil {
		writeErr(w, http.StatusNotFound, "plan not found")
		return
	}
	// Revenue goes to the platform treasury account (user id all-zeros).
	txID, err := a.moveUSD(r.Context(), uid, "00000000-0000-0000-0000-000000000000", formatMoney(price),
		"premium_sub", "premium_rev", "premium subscription")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO premium_subscriptions (user_id, plan_id, expires_at, tx_id)
		 VALUES ($1,$2, now() + interval '30 days', $3) RETURNING id`, uid, req.PlanID, txID).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record subscription")
		return
	}
	a.db.Exec(r.Context(), `UPDATE users SET is_premium=true WHERE id=$1`, uid)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "active"})
}

// ---------- Self-hosted GIF catalog ----------

// POST /api/gifs — upload a GIF into the shared catalog (tags for search).
func (a *App) handleUploadGIF(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string   `json:"title"`
		Tags     []string `json:"tags"`
		MediaURL string   `json:"media_url"`
	}
	if !decodeJSON(w, r, &req) || req.MediaURL == "" {
		writeErr(w, http.StatusBadRequest, "media_url required")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO gif_catalog (uploader_id, title, tags, media_url) VALUES ($1,$2,$3,$4) RETURNING id`,
		userIDFrom(r), req.Title, req.Tags, req.MediaURL).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save gif")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/gifs?q= — tag/title search over the self-hosted catalog.
func (a *App) handleSearchGIFs(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	rows, err := a.db.Query(r.Context(),
		`SELECT id, title, media_url FROM gif_catalog
		 WHERE $1='' OR lower(title) LIKE '%'||$1||'%' OR EXISTS(
		   SELECT 1 FROM unnest(tags) t WHERE lower(t) LIKE '%'||$1||'%')
		 ORDER BY created_at DESC LIMIT 50`, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, url string
		if err := rows.Scan(&id, &title, &url); err == nil {
			out = append(out, map[string]any{"id": id, "title": title, "media_url": url})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"gifs": out})
}

// ---------- Contact-card + GIF messages (Telegram) ----------

// POST /api/conversations/{id}/contact — share a contact card in chat.
func (a *App) handleSendContactCard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID   string `json:"user_id"`
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.UserID == "" && req.Username != "" {
		if err := a.db.QueryRow(r.Context(),
			`SELECT id FROM users WHERE username=$1 AND status='active'`,
			strings.ToLower(strings.TrimPrefix(strings.TrimSpace(req.Username), "@"))).Scan(&req.UserID); err != nil {
			writeErr(w, http.StatusNotFound, "user not found")
			return
		}
	}
	if req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "user_id or username required")
		return
	}
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var uname, name string
	if err := a.db.QueryRow(r.Context(),
		`SELECT username::text, display_name FROM users WHERE id=$1 AND status='active'`,
		req.UserID).Scan(&uname, &name); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	card, _ := json.Marshal(map[string]string{"user_id": req.UserID, "username": uname})
	var msgID string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, kind, entities)
		 VALUES ($1,$2,$3,'contact',$4) RETURNING id`,
		convID, uid, name, card).Scan(&msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to send")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "id": msgID, "conversation_id": convID, "sender_id": uid,
		"kind": "contact", "body": name, "entities": json.RawMessage(card),
		"created_at": time.Now()})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusCreated, map[string]string{"id": msgID, "kind": "contact"})
}

// POST /api/conversations/{id}/gif — send a catalog GIF as a message.
func (a *App) handleSendGIFMessage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GifID string `json:"gif_id"`
	}
	if !decodeJSON(w, r, &req) || req.GifID == "" {
		writeErr(w, http.StatusBadRequest, "gif_id required")
		return
	}
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var url string
	if err := a.db.QueryRow(r.Context(),
		`SELECT media_url FROM gif_catalog WHERE id=$1`, req.GifID).Scan(&url); err != nil {
		writeErr(w, http.StatusNotFound, "gif not found")
		return
	}
	var msgID string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, media_url, kind)
		 VALUES ($1,$2,'',$3,'gif') RETURNING id`, convID, uid, url).Scan(&msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to send")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "id": msgID, "conversation_id": convID, "sender_id": uid,
		"kind": "gif", "media_url": url, "created_at": time.Now()})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusCreated, map[string]string{"id": msgID, "kind": "gif"})
}

// ---------- Channel discussion groups (Telegram) ----------

// PUT /api/channels/{id}/discussion — link a discussion group to a channel.
func (a *App) handleLinkDiscussion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GroupID string `json:"group_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, chanID := userIDFrom(r), r.PathValue("id")
	var role string
	var isChannel bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT c.is_channel, m.role FROM conversations c
		 JOIN conversation_members m ON m.conversation_id=c.id AND m.user_id=$2
		 WHERE c.id=$1`, chanID, uid).Scan(&isChannel, &role); err != nil || !isChannel {
		writeErr(w, http.StatusNotFound, "channel not found")
		return
	}
	if role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "admin required")
		return
	}
	if req.GroupID != "" && !a.isMember(r.Context(), req.GroupID, uid) {
		writeErr(w, http.StatusForbidden, "not a member of the discussion group")
		return
	}
	a.db.Exec(r.Context(), `UPDATE conversations SET discussion_group_id=NULLIF($2,'')::uuid WHERE id=$1`,
		chanID, req.GroupID)
	writeJSON(w, http.StatusOK, map[string]string{"discussion_group_id": req.GroupID})
}

// GET /api/channels/{id}/stats — view/share analytics for a channel.
func (a *App) handleChannelStats(w http.ResponseWriter, r *http.Request) {
	uid, chanID := userIDFrom(r), r.PathValue("id")
	var role string
	if err := a.db.QueryRow(r.Context(),
		`SELECT m.role FROM conversation_members m WHERE m.conversation_id=$1 AND m.user_id=$2`,
		chanID, uid).Scan(&role); err != nil || (role != "owner" && role != "admin") {
		writeErr(w, http.StatusForbidden, "admin required")
		return
	}
	var members, msgs7d, msgs30d int64
	a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM conversation_members WHERE conversation_id=$1`, chanID).Scan(&members)
	a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM messages WHERE conversation_id=$1 AND created_at > now() - interval '7 days'`,
		chanID).Scan(&msgs7d)
	a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM messages WHERE conversation_id=$1 AND created_at > now() - interval '30 days'`,
		chanID).Scan(&msgs30d)
	writeJSON(w, http.StatusOK, map[string]any{
		"members": members, "messages_7d": msgs7d, "messages_30d": msgs30d})
}

// PUT /api/conversations/{id}/anonymous-admin — group admins can act
// anonymously (name hidden in admin actions).
func (a *App) handleAnonymousAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Anonymous bool `json:"anonymous"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE conversation_members SET is_anonymous=$3
		 WHERE conversation_id=$1 AND user_id=$2 AND role IN ('owner','admin')`,
		r.PathValue("id"), userIDFrom(r), req.Anonymous)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusForbidden, "admin required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"anonymous": req.Anonymous})
}

// ---------- Sounds (TikTok/FB self-hosted sound library) ----------

// POST /api/sounds — publish a sound clip.
func (a *App) handleCreateSound(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string `json:"title"`
		Artist    string `json:"artist"`
		MediaURL  string `json:"media_url"`
		DurationS int    `json:"duration_s"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" || req.MediaURL == "" {
		writeErr(w, http.StatusBadRequest, "title and media_url required")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO sounds (uploader_id, title, artist, media_url, duration_s)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		userIDFrom(r), req.Title, req.Artist, req.MediaURL, req.DurationS).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to publish sound")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/sounds?q= — search the sound library, most-used first.
func (a *App) handleSearchSounds(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	rows, err := a.db.Query(r.Context(),
		`SELECT id, title, artist, media_url, duration_s, use_count FROM sounds
		 WHERE $1='' OR lower(title) LIKE '%'||$1||'%' OR lower(artist) LIKE '%'||$1||'%'
		 ORDER BY use_count DESC, created_at DESC LIMIT 50`, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, artist, url string
		var dur int
		var uses int64
		if err := rows.Scan(&id, &title, &artist, &url, &dur, &uses); err == nil {
			out = append(out, map[string]any{"id": id, "title": title, "artist": artist,
				"media_url": url, "duration_s": dur, "use_count": uses})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sounds": out})
}

// ---------- Post shares with counter (TikTok) ----------
// Shares are recorded by handleSharePostToChat (handlers_social3.go): it
// inserts a shares row and bumps posts.share_count on every share-to-chat.

// ---------- Paywalled series (TikTok Series) ----------

// PUT /api/posts/{id}/price — the author gates a post behind a USD price
// (0 = free).
func (a *App) handleSetPostPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PriceUSD float64 `json:"price_usd"`
	}
	if !decodeJSON(w, r, &req) || req.PriceUSD < 0 {
		writeErr(w, http.StatusBadRequest, "price_usd must be >= 0")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE posts SET price_usd=$2::numeric WHERE id=$1 AND author_id=$3 AND deleted_at IS NULL`,
		r.PathValue("id"), formatMoney(req.PriceUSD), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "post not found or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"price_usd": req.PriceUSD})
}

// POST /api/posts/{id}/purchase — buy access to a paywalled post; funds go
// to the author via the USD wallet.
func (a *App) handlePurchasePost(w http.ResponseWriter, r *http.Request) {
	uid, postID := userIDFrom(r), r.PathValue("id")
	var author string
	var price float64
	if err := a.db.QueryRow(r.Context(),
		`SELECT author_id, trim_scale(price_usd)::float8 FROM posts WHERE id=$1 AND deleted_at IS NULL`,
		postID).Scan(&author, &price); err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	if price <= 0 {
		writeErr(w, http.StatusBadRequest, "post is not paywalled")
		return
	}
	if uid == author {
		writeErr(w, http.StatusBadRequest, "you own this post")
		return
	}
	var already bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM content_purchases WHERE post_id=$1 AND user_id=$2)`,
		postID, uid).Scan(&already); err == nil && already {
		writeErr(w, http.StatusConflict, "already purchased")
		return
	}
	txID, err := a.moveUSD(r.Context(), uid, author, formatMoney(price),
		"content_purchase", "content_sale", "paid content")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO content_purchases (post_id, user_id, amount_usd, tx_id) VALUES ($1,$2,$3,$4)`,
		postID, uid, price, txID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record purchase")
		return
	}
	a.creditCreatorEarnings(r.Context(), author, "paid_content", formatMoney(price), postID)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "purchased", "tx_id": txID})
}

// ---------- Marketplace (Facebook) ----------

// POST /api/marketplace — create a listing.
func (a *App) handleCreateListing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string    `json:"title"`
		Description string    `json:"description"`
		PriceUSD    float64   `json:"price_usd"`
		Category    string    `json:"category"`
		Media       []mediaIn `json:"media"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" || req.PriceUSD < 0 {
		writeErr(w, http.StatusBadRequest, "title and price_usd required")
		return
	}
	media, _ := json.Marshal(req.Media)
	if len(req.Media) == 0 {
		media = []byte("[]")
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO marketplace_listings (seller_id, title, description, price_usd, category, media)
		 VALUES ($1,$2,$3,$4::numeric,COALESCE(NULLIF($5,''),'general'),$6) RETURNING id`,
		userIDFrom(r), req.Title, req.Description, formatMoney(req.PriceUSD), req.Category, media).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create listing")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/marketplace?q=&category= — browse active listings.
func (a *App) handleListListings(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	cat := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("category")))
	rows, err := a.db.Query(r.Context(),
		`SELECT l.id, l.title, l.description, trim_scale(l.price_usd)::float8, l.category, l.media,
		        u.username::text, l.created_at
		 FROM marketplace_listings l JOIN users u ON u.id=l.seller_id
		 WHERE l.status='active'
		   AND ($1='' OR lower(l.title) LIKE '%'||$1||'%' OR lower(l.description) LIKE '%'||$1||'%')
		   AND ($2='' OR l.category=$2)
		 ORDER BY l.created_at DESC LIMIT 60`, q, cat)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load listings")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, desc, cat2, media, seller string
		var price float64
		var created time.Time
		if err := rows.Scan(&id, &title, &desc, &price, &cat2, &media, &seller, &created); err == nil {
			out = append(out, map[string]any{"id": id, "title": title, "description": desc,
				"price_usd": price, "category": cat2, "media": json.RawMessage(media),
				"seller": seller, "created_at": created})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"listings": out})
}

// PUT /api/marketplace/{id}/status — seller marks sold/removed/active.
func (a *App) handleListingStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Status != "active" && req.Status != "sold" && req.Status != "removed" {
		writeErr(w, http.StatusBadRequest, "status must be active|sold|removed")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE marketplace_listings SET status=$3 WHERE id=$1 AND seller_id=$2`,
		r.PathValue("id"), userIDFrom(r), req.Status)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "listing not found or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// ---------- Fundraisers (Facebook) ----------

// POST /api/fundraisers — start a fundraiser.
func (a *App) handleCreateFundraiser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string  `json:"title"`
		Description string  `json:"description"`
		GoalUSD     float64 `json:"goal_usd"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" || req.GoalUSD <= 0 {
		writeErr(w, http.StatusBadRequest, "title and goal_usd required")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO fundraisers (creator_id, title, description, goal_usd)
		 VALUES ($1,$2,$3,$4::numeric) RETURNING id`,
		userIDFrom(r), req.Title, req.Description, formatMoney(req.GoalUSD)).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create fundraiser")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// POST /api/fundraisers/{id}/donate — donate via the USD wallet; updates the
// raised total and credits the organizer.
func (a *App) handleDonate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AmountUSD float64 `json:"amount_usd"`
	}
	if !decodeJSON(w, r, &req) || req.AmountUSD <= 0 {
		writeErr(w, http.StatusBadRequest, "amount_usd must be > 0")
		return
	}
	uid, fid := userIDFrom(r), r.PathValue("id")
	var creator, status string
	if err := a.db.QueryRow(r.Context(),
		`SELECT creator_id, status FROM fundraisers WHERE id=$1`, fid).Scan(&creator, &status); err != nil {
		writeErr(w, http.StatusNotFound, "fundraiser not found")
		return
	}
	if status != "active" {
		writeErr(w, http.StatusConflict, "fundraiser is closed")
		return
	}
	txID, err := a.moveUSD(r.Context(), uid, creator, formatMoney(req.AmountUSD),
		"donation_send", "donation_recv", "fundraiser donation")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	a.db.Exec(r.Context(),
		`INSERT INTO fundraiser_donations (fundraiser_id, user_id, amount_usd, tx_id) VALUES ($1,$2,$3,$4)`,
		fid, uid, req.AmountUSD, txID)
	a.db.Exec(r.Context(), `UPDATE fundraisers SET raised_usd = raised_usd + $2::numeric WHERE id=$1`,
		fid, formatMoney(req.AmountUSD))
	a.notify(r.Context(), creator, "donation", "New donation",
		"A donation arrived on your fundraiser", map[string]any{"from": uid, "amount_usd": req.AmountUSD})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "donated", "tx_id": txID})
}

// GET /api/fundraisers/{id} — progress + donor count.
func (a *App) handleGetFundraiser(w http.ResponseWriter, r *http.Request) {
	var id, title, desc, status string
	var goal, raised float64
	var donors int64
	err := a.db.QueryRow(r.Context(),
		`SELECT f.id, f.title, f.description, trim_scale(f.goal_usd)::float8,
		        trim_scale(f.raised_usd)::float8, f.status,
		        (SELECT count(*) FROM fundraiser_donations d WHERE d.fundraiser_id=f.id)
		 FROM fundraisers f WHERE f.id=$1`, r.PathValue("id")).
		Scan(&id, &title, &desc, &goal, &raised, &status, &donors)
	if err != nil {
		writeErr(w, http.StatusNotFound, "fundraiser not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "title": title, "description": desc,
		"goal_usd": goal, "raised_usd": raised, "status": status, "donor_count": donors})
}

// ---------- Restricted mode / content ratings / family pairing (TikTok) ----------

// PUT /api/me/restricted-mode — account-level filter for mature-rated posts.
func (a *App) handleRestrictedMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	// Guardians lock the setting: an actively-paired child cannot disable it.
	if !req.Enabled {
		var locked bool
		_ = a.db.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM family_links WHERE child_id=$1 AND status='active')`, uid).Scan(&locked)
		if locked {
			writeErr(w, http.StatusForbidden, "restricted mode is managed by your guardian")
			return
		}
	}
	a.db.Exec(r.Context(), `UPDATE users SET restricted_mode=$2 WHERE id=$1`, uid, req.Enabled)
	writeJSON(w, http.StatusOK, map[string]bool{"restricted_mode": req.Enabled})
}

// PUT /api/posts/{id}/rating — author sets content_rating everyone|mature.
func (a *App) handleSetContentRating(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rating string `json:"rating"`
	}
	if !decodeJSON(w, r, &req) || (req.Rating != "everyone" && req.Rating != "mature") {
		writeErr(w, http.StatusBadRequest, "rating must be everyone|mature")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE posts SET content_rating=$3 WHERE id=$1 AND author_id=$2`,
		r.PathValue("id"), userIDFrom(r), req.Rating)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "post not found or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content_rating": req.Rating})
}

// POST /api/family/link — guardian invites a child account by username.
func (a *App) handleFamilyLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChildUsername string `json:"child_username"`
	}
	if !decodeJSON(w, r, &req) || req.ChildUsername == "" {
		writeErr(w, http.StatusBadRequest, "child_username required")
		return
	}
	uid := userIDFrom(r)
	var child string
	if err := a.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE username=$1 AND status='active'`,
		strings.ToLower(req.ChildUsername)).Scan(&child); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if child == uid {
		writeErr(w, http.StatusBadRequest, "cannot link yourself")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO family_links (guardian_id, child_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		uid, child); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create link")
		return
	}
	a.notify(r.Context(), child, "family_link", "Family pairing request",
		"A guardian invited you to family pairing", map[string]any{"guardian_id": uid})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "pending"})
}

// POST /api/family/accept — the child accepts a pending link; restricted
// mode is switched on for the child account.
func (a *App) handleFamilyAccept(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	res, err := a.db.Exec(r.Context(),
		`UPDATE family_links SET status='active' WHERE child_id=$1 AND status='pending'`, uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "no pending link")
		return
	}
	a.db.Exec(r.Context(), `UPDATE users SET restricted_mode=true WHERE id=$1`, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// GET /api/family — links where you are guardian or child.
func (a *App) handleFamilyList(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT l.guardian_id, g.username::text, l.child_id, c.username::text, l.status
		 FROM family_links l
		 JOIN users g ON g.id=l.guardian_id JOIN users c ON c.id=l.child_id
		 WHERE l.guardian_id=$1 OR l.child_id=$1`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load links")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var gid, guname, cid, cuname, status string
		if err := rows.Scan(&gid, &guname, &cid, &cuname, &status); err == nil {
			out = append(out, map[string]any{"guardian_id": gid, "guardian_username": guname,
				"child_id": cid, "child_username": cuname, "status": status})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

// ---------- XP / levels (imo gamification) ----------

// awardXP bumps a user's XP for an activity (message, post, call minutes).
// Level thresholds are quadratic: level N requires (N-1)^2 * 100 XP.
func (a *App) awardXP(ctx context.Context, userID string, points int) {
	_, _ = a.db.Exec(ctx, `UPDATE users SET xp = xp + $2 WHERE id=$1`, userID, points)
}

// GET /api/me/level — your XP, level, and XP needed for the next level.
func (a *App) handleMyLevel(w http.ResponseWriter, r *http.Request) {
	var xp int64
	if err := a.db.QueryRow(r.Context(),
		`SELECT xp FROM users WHERE id=$1`, userIDFrom(r)).Scan(&xp); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load xp")
		return
	}
	level := xpLevel(xp)
	writeJSON(w, http.StatusOK, map[string]any{
		"xp": xp, "level": level, "next_level_xp": int64(level*level) * 100})
}

// GET /api/users/{username}/level — public level badge.
func (a *App) handleUserLevel(w http.ResponseWriter, r *http.Request) {
	var xp int64
	if err := a.db.QueryRow(r.Context(),
		`SELECT xp FROM users WHERE username=$1 AND status='active'`,
		strings.ToLower(r.PathValue("username"))).Scan(&xp); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"xp": xp, "level": xpLevel(xp)})
}

func xpLevel(xp int64) int {
	lvl := 1
	for int64(lvl*lvl)*100 <= xp {
		lvl++
	}
	return lvl
}

// ---------- People nearby + group discovery (Telegram/imo) ----------

// PUT /api/me/discoverable — opt in/out of people-nearby discovery.
func (a *App) handleDiscoverable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	a.db.Exec(r.Context(), `UPDATE users SET discoverable=$2 WHERE id=$1`, userIDFrom(r), req.Enabled)
	writeJSON(w, http.StatusOK, map[string]bool{"discoverable": req.Enabled})
}

// GET /api/nearby — users currently broadcasting live location who opted in
// to discovery, within ~5km of your own live location.
func (a *App) handlePeopleNearby(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var myLat, myLng float64
	var ok bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT true, lat, lng FROM live_locations
		 WHERE user_id=$1 AND expires_at > now() ORDER BY updated_at DESC LIMIT 1`, uid).
		Scan(&ok, &myLat, &myLng)
	if !ok {
		writeErr(w, http.StatusBadRequest, "start live location first")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT u.username::text, u.display_name,
		        6371 * 2 * asin(sqrt(
		          power(sin(radians(l.lat - $2) / 2), 2) +
		          cos(radians($2)) * cos(radians(l.lat)) *
		          power(sin(radians(l.lng - $3) / 2), 2))) AS km
		 FROM (SELECT DISTINCT ON (user_id) user_id, lat, lng FROM live_locations
		       WHERE expires_at > now() ORDER BY user_id, updated_at DESC) l
		 JOIN users u ON u.id = l.user_id
		 WHERE l.user_id <> $1 AND u.discoverable
		 ORDER BY km LIMIT 50`, uid, myLat, myLng)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load nearby")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var uname, name string
		var km float64
		if err := rows.Scan(&uname, &name, &km); err == nil && km <= 5 {
			out = append(out, map[string]any{"username": uname, "display_name": name,
				"distance_km": float64(int(km*100)) / 100})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out})
}

// GET /api/discover/groups?q=&category= — public groups directory.
func (a *App) handleDiscoverGroups(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	cat := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("category")))
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id, c.title, c.handle, c.category,
		        (SELECT count(*) FROM conversation_members m WHERE m.conversation_id=c.id)
		 FROM conversations c
		 WHERE c.handle IS NOT NULL AND c.is_group AND NOT c.is_channel
		   AND ($1='' OR lower(c.title) LIKE '%'||$1||'%' OR c.handle LIKE '%'||$1||'%')
		   AND ($2='' OR c.category=$2)
		 ORDER BY 5 DESC LIMIT 50`, q, cat)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load groups")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, handle, category string
		var members int64
		if err := rows.Scan(&id, &title, &handle, &category, &members); err == nil {
			out = append(out, map[string]any{"id": id, "title": title, "handle": handle,
				"category": category, "member_count": members})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": out})
}

// PUT /api/conversations/{id}/category — group owner sets a discovery
// category (imo big-groups directory).
func (a *App) handleSetGroupCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Category string `json:"category"`
	}
	if !decodeJSON(w, r, &req) || len(req.Category) > 40 {
		writeErr(w, http.StatusBadRequest, "category must be <= 40 chars")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE conversations SET category=$3 WHERE id=$1 AND EXISTS(
		  SELECT 1 FROM conversation_members m
		  WHERE m.conversation_id=$1 AND m.user_id=$2 AND m.role='owner')`,
		r.PathValue("id"), userIDFrom(r), strings.ToLower(strings.TrimSpace(req.Category)))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusForbidden, "owner required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"category": req.Category})
}

// ---------- Chat backup/restore (imo) ----------

// GET /api/me/export — full JSON export of your conversations' messages
// (data portability / chat backup).
func (a *App) handleExportData(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT m.conversation_id, m.sender_id, u.username::text, m.body,
		        COALESCE(m.media_url,''), m.kind, m.created_at
		 FROM messages m JOIN users u ON u.id=m.sender_id
		 WHERE m.conversation_id IN
		   (SELECT conversation_id FROM conversation_members WHERE user_id=$1)
		 ORDER BY m.conversation_id, m.created_at`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "export failed")
		return
	}
	defer rows.Close()
	type msg struct {
		ConversationID string    `json:"conversation_id"`
		SenderID       string    `json:"sender_id"`
		SenderUsername string    `json:"sender_username"`
		Body           string    `json:"body"`
		MediaURL       string    `json:"media_url"`
		Kind           string    `json:"kind"`
		CreatedAt      time.Time `json:"created_at"`
	}
	msgs := []msg{}
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.ConversationID, &m.SenderID, &m.SenderUsername,
			&m.Body, &m.MediaURL, &m.Kind, &m.CreatedAt); err == nil {
			msgs = append(msgs, m)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=chatapp-export.json")
	_ = json.NewEncoder(w).Encode(map[string]any{"messages": msgs, "exported_at": time.Now()})
}

// ---------- Screenshot alerts in secret chats (imo) ----------

// POST /api/conversations/{id}/screenshot — best-effort alert: notifies the
// other members that you took a screenshot (client-triggered).
func (a *App) handleScreenshotAlert(w http.ResponseWriter, r *http.Request) {
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var uname string
	a.db.QueryRow(r.Context(), `SELECT username::text FROM users WHERE id=$1`, uid).Scan(&uname)
	payload, _ := json.Marshal(map[string]any{
		"type": "screenshot_alert", "conversation_id": convID, "user_id": uid,
		"username": uname, "created_at": time.Now()})
	a.fanoutToMembers(r.Context(), convID, payload, uid)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "alerted"})
}

// ---------- Bot payments + inline bots (Telegram) ----------
// Mini apps ship via POST /api/bots/{id}/mini-app (bots.mini_app_url).

// POST /api/bot/{token}/createInvoice — a bot issues a payable invoice to a
// user (Telegram bot payments; token-authed like the other bot endpoints).
func (a *App) handleBotCreateInvoice(w http.ResponseWriter, r *http.Request) {
	_, botUserID, ok := a.botFromToken(r.Context(), r.PathValue("token"))
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid bot token")
		return
	}
	var req struct {
		UserID    string  `json:"user_id"`
		Title     string  `json:"title"`
		AmountUSD float64 `json:"amount_usd"`
		Payload   string  `json:"payload"`
	}
	if !decodeJSON(w, r, &req) || req.UserID == "" || strings.TrimSpace(req.Title) == "" || req.AmountUSD <= 0 {
		writeErr(w, http.StatusBadRequest, "user_id, title and amount_usd required")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO bot_invoices (bot_id, user_id, title, amount_usd, payload)
		 VALUES ($1,$2,$3,$4::numeric,$5) RETURNING id`,
		botUserID, req.UserID, req.Title, formatMoney(req.AmountUSD), req.Payload).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create invoice")
		return
	}
	a.notify(r.Context(), req.UserID, "invoice", "Payment request",
		req.Title, map[string]any{"invoice_id": id, "bot_id": botUserID})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// POST /api/bots/invoices/{id}/pay — the invoiced user pays from USD wallet.
func (a *App) handleBotPayInvoice(w http.ResponseWriter, r *http.Request) {
	uid, invID := userIDFrom(r), r.PathValue("id")
	var botID, title, status string
	var amount float64
	if err := a.db.QueryRow(r.Context(),
		`SELECT bot_id, title, status, trim_scale(amount_usd)::float8
		 FROM bot_invoices WHERE id=$1 AND user_id=$2`, invID, uid).
		Scan(&botID, &title, &status, &amount); err != nil {
		writeErr(w, http.StatusNotFound, "invoice not found")
		return
	}
	if status != "pending" {
		writeErr(w, http.StatusConflict, "invoice already "+status)
		return
	}
	txID, err := a.moveUSD(r.Context(), uid, botID, formatMoney(amount),
		"bot_payment", "bot_revenue", title)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	a.db.Exec(r.Context(),
		`UPDATE bot_invoices SET status='paid', paid_at=now(), tx_id=$2 WHERE id=$1`, invID, txID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "paid", "tx_id": txID})
}

// GET /api/bots/inline?q=&bot=<username> — inline bot query: returns the
// bot's searchable results (its registered mini app and its recent public
// posts matching the query).
func (a *App) handleInlineQuery(w http.ResponseWriter, r *http.Request) {
	botName := strings.ToLower(strings.TrimPrefix(r.URL.Query().Get("bot"), "@"))
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var botUserID, miniAppURL string
	if err := a.db.QueryRow(r.Context(),
		`SELECT b.user_id, b.mini_app_url FROM bots b
		 JOIN users u ON u.id=b.user_id
		 WHERE u.username=$1 AND b.active AND u.status='active'`,
		botName).Scan(&botUserID, &miniAppURL); err != nil {
		writeErr(w, http.StatusNotFound, "bot not found")
		return
	}
	out := []map[string]any{}
	if miniAppURL != "" && (q == "" || strings.Contains(strings.ToLower(botName), q)) {
		out = append(out, map[string]any{"type": "mini_app", "title": botName, "ref": miniAppURL})
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT left(body, 80), id::text FROM posts
		 WHERE author_id=$1 AND deleted_at IS NULL
		   AND ($2='' OR lower(body) LIKE '%'||$2||'%')
		 ORDER BY created_at DESC LIMIT 20`, botUserID, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "inline query failed")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var title, ref string
		if err := rows.Scan(&title, &ref); err == nil {
			out = append(out, map[string]any{"type": "post", "title": title, "ref": ref})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// ---------- Live gifts + leaderboard (TikTok/imo) ----------

// POST /api/live/{roomId}/gifts — send a wallet-backed gift during a live
// room; the host receives the USD.
func (a *App) handleSendLiveGift(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GiftID string `json:"gift_id"`
		ToUser string `json:"to_user"`
	}
	if !decodeJSON(w, r, &req) || req.GiftID == "" || req.ToUser == "" {
		writeErr(w, http.StatusBadRequest, "gift_id and to_user required")
		return
	}
	uid, roomID := userIDFrom(r), r.PathValue("roomId")
	if req.ToUser == uid {
		writeErr(w, http.StatusBadRequest, "cannot gift yourself")
		return
	}
	var name, emoji string
	var price float64
	if err := a.db.QueryRow(r.Context(),
		`SELECT name, emoji, price_usd FROM gift_catalog WHERE id=$1 AND active`,
		req.GiftID).Scan(&name, &emoji, &price); err != nil {
		writeErr(w, http.StatusNotFound, "gift not found")
		return
	}
	txID, err := a.moveUSD(r.Context(), uid, req.ToUser, formatMoney(price),
		"live_gift_send", "live_gift_recv", name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	a.db.Exec(r.Context(),
		`INSERT INTO live_gifts (room_id, gift_id, from_user, to_user, amount_usd, tx_id)
		 VALUES ($1,$2,$3,$4,$5,$6)`, roomID, req.GiftID, uid, req.ToUser, price, txID)
	a.notify(r.Context(), req.ToUser, "live_gift", "Live gift",
		emoji+" "+name, map[string]any{"from": uid, "room_id": roomID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "sent", "tx_id": txID})
}

// GET /api/live/{roomId}/leaderboard — top gifters in a live room.
func (a *App) handleLiveLeaderboard(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT u.username::text, count(*), trim_scale(sum(l.amount_usd))::float8
		 FROM live_gifts l JOIN users u ON u.id=l.from_user
		 WHERE l.room_id=$1 GROUP BY u.username
		 ORDER BY 3 DESC, 2 DESC LIMIT 20`, r.PathValue("roomId"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load leaderboard")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var uname string
		var gifts int64
		var total float64
		if err := rows.Scan(&uname, &gifts, &total); err == nil {
			out = append(out, map[string]any{"username": uname, "gifts": gifts, "total_usd": total})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"leaderboard": out})
}

// ---------- Creator marketplace (TikTok brand deals) ----------

// POST /api/brand-deals — a brand posts a campaign brief.
func (a *App) handleCreateBrandDeal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title     string  `json:"title"`
		Brief     string  `json:"brief"`
		BudgetUSD float64 `json:"budget_usd"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" || req.BudgetUSD <= 0 {
		writeErr(w, http.StatusBadRequest, "title and budget_usd required")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO brand_deals (brand_id, title, brief, budget_usd)
		 VALUES ($1,$2,$3,$4::numeric) RETURNING id`,
		userIDFrom(r), req.Title, req.Brief, formatMoney(req.BudgetUSD)).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create deal")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/brand-deals — open briefs for creators to browse.
func (a *App) handleListBrandDeals(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT d.id, d.title, d.brief, trim_scale(d.budget_usd)::float8, u.username::text, d.created_at
		 FROM brand_deals d JOIN users u ON u.id=d.brand_id
		 WHERE d.status='open' ORDER BY d.created_at DESC LIMIT 50`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load deals")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, brief, brand string
		var budget float64
		var created time.Time
		if err := rows.Scan(&id, &title, &brief, &budget, &brand, &created); err == nil {
			out = append(out, map[string]any{"id": id, "title": title, "brief": brief,
				"budget_usd": budget, "brand": brand, "created_at": created})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"deals": out})
}

// POST /api/brand-deals/{id}/accept — a creator accepts the brief.
func (a *App) handleAcceptBrandDeal(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE brand_deals SET creator_id=$2, status='accepted'
		 WHERE id=$1 AND status='open' AND brand_id<>$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "deal not available")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

// ---------- Professional dashboard (Facebook) ----------

// GET /api/me/analytics — per-account rollup for the professional dashboard.
func (a *App) handleProDashboard(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var posts, followers int64
	var likes, comments, views, shares7d int64
	var earnings float64
	a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM posts WHERE author_id=$1 AND deleted_at IS NULL`, uid).Scan(&posts)
	a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM follows WHERE followee_id=$1`, uid).Scan(&followers)
	a.db.QueryRow(r.Context(),
		`SELECT COALESCE(sum(like_count),0), COALESCE(sum(comment_count),0), COALESCE(sum(view_count),0)
		 FROM posts WHERE author_id=$1 AND deleted_at IS NULL`, uid).Scan(&likes, &comments, &views)
	a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM shares s JOIN posts p ON p.id=s.post_id
		 WHERE p.author_id=$1 AND s.created_at > now() - interval '7 days'`, uid).Scan(&shares7d)
	a.db.QueryRow(r.Context(),
		`SELECT COALESCE(trim_scale(sum(amount))::float8,0) FROM ledger_entries le
		 JOIN wallet_accounts wa ON wa.id=le.account_id
		 WHERE wa.user_id=$1 AND le.kind LIKE '%recv%'`, uid).Scan(&earnings)
	writeJSON(w, http.StatusOK, map[string]any{
		"posts": posts, "followers": followers, "total_likes": likes,
		"total_comments": comments, "total_views": views, "shares_7d": shares7d,
		"earnings_usd": earnings})
}

// ---------- Premium expiry worker ----------

func (a *App) startPremiumWorker() {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			a.expirePremium()
		}
	}()
}

func (a *App) expirePremium() {
	ctx := context.Background()
	rows, err := a.db.Query(ctx,
		`UPDATE premium_subscriptions SET status='expired'
		 WHERE status='active' AND expires_at < now() RETURNING user_id`)
	if err != nil {
		return
	}
	var uids []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err == nil {
			uids = append(uids, uid)
		}
	}
	rows.Close()
	for _, uid := range uids {
		a.db.Exec(ctx,
			`UPDATE users SET is_premium=false WHERE id=$1 AND NOT EXISTS(
			  SELECT 1 FROM premium_subscriptions WHERE user_id=$1 AND status='active')`, uid)
	}
}

// sanitizeEntities keeps only well-formed formatting spans
// (Telegram-style entities: bold/italic/mono/spoiler/link) so arbitrary
// client payloads cannot inject junk into messages.entities.
func sanitizeEntities(raw json.RawMessage) json.RawMessage {
	type entity struct {
		Type   string `json:"type"`
		Offset int    `json:"offset"`
		Length int    `json:"length"`
		URL    string `json:"url,omitempty"`
	}
	var in []entity
	if len(raw) == 0 || json.Unmarshal(raw, &in) != nil {
		return json.RawMessage("[]")
	}
	allowed := map[string]bool{"bold": true, "italic": true, "mono": true,
		"spoiler": true, "link": true, "underline": true, "strike": true}
	out := []entity{}
	for _, e := range in {
		if !allowed[e.Type] || e.Offset < 0 || e.Length <= 0 || e.Length > 4096 || e.Offset > 65536 {
			continue
		}
		if e.Type != "link" {
			e.URL = ""
		} else if !strings.HasPrefix(e.URL, "https://") && !strings.HasPrefix(e.URL, "http://") {
			continue
		}
		out = append(out, e)
	}
	b, _ := json.Marshal(out)
	return b
}
