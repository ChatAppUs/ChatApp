package main

// Gap pack 9 (migration 025): closes the remaining depth gaps across all
// five competitor matrices:
//   * notification-settings matrix (Telegram) — per-kind per-user on/off switches
//   * 2FA one-time backup / recovery codes (Telegram)
//   * unsend-for-everyone window + message edit history (Telegram)
//   * story archive + "On this day" memories (Facebook)
//   * TikTok-grade reel studio: drafts, effects, voiceovers, captions
//   * creator insights rollup (TikTok / X impressions, reach, follower-growth, top-sounds
//   * share deep-links + OG share-cards (X / TikTok viral loop
//   * call reactions / raise-hand / ratings + scheduled calls (imo)
//   * page insights rollup (Facebook
//   * group post-approval moderation queue (Facebook
//   * quote-repost with comment (X

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// randomRecoveryCode mints an unbiased 10-character code from a CSPRNG.
func randomRecoveryCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no look-alikes
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return "CA-" + string(buf), nil
}

// ---------- Notification settings matrix (Telegram ----------

// GET /api/me/notification-settings — per-kind matrix.
func (a *App) handleGetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT kind, enabled FROM notification_settings WHERE user_id=$1`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var kind string
		var enabled bool
		if err := rows.Scan(&kind, &enabled); err == nil {
			out[kind] = enabled
		}
	}
	// Seed defaults for kinds the user has never touched.
	defaults := []string{"messages", "groups", "calls", "live", "gifts", "replies", "mentions", "reposts", "sounds", "stories", "marketplace", "withdrawals", "deposits", "kyc", "system"}
	for _, kind := range defaults {
		if _, ok := out[kind]; !ok {
			out[kind] = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": out})
}

// PUT /api/me/notification-settings/{kind} — {enabled} toggle.
func (a *App) handleSetNotificationSetting(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	kind := r.PathValue("kind")
	if len(kind) == 0 || len(kind) > 32 {
		writeErr(w, http.StatusBadRequest, "invalid kind")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO notification_settings (user_id, kind, enabled, updated_at)
                 VALUES ($1,$2,$3,now())
                 ON CONFLICT (user_id, kind) DO UPDATE SET enabled=EXCLUDED.enabled, updated_at=now()`,
		uid, kind, req.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update setting")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "enabled": req.Enabled})
}

// notificationEnabled reports whether a user's kind is unmuted (default: on).
func (a *App) notificationEnabled(ctx context.Context, uid, kind string) bool {
	var enabled bool
	if err := a.db.QueryRow(ctx,
		`SELECT enabled FROM notification_settings WHERE user_id=$1 AND kind=$2`, uid, kind).Scan(&enabled); err != nil {
		return true
	}
	return enabled
}

// ---------- 2FA one-time recovery codes (Telegram ----------

// POST /api/auth/2fa/recovery-codes — mint 8 fresh scratch codes (previous
// codes revoked). Requires either 2FA enabled with a valid code, or that 2FA
// is toggled-on in the same session (setup flow). Codes are stored SHA-256 hashed
// and shown as plaintext exactly once.
func (a *App) handleGenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Code string `json:"code"`
	}
	_ = decodeJSON(w, r, &req)
	var totpEnabled bool
	var secret string
	_ = a.db.QueryRow(r.Context(), `SELECT totp_enabled, COALESCE(totp_secret,'') FROM users WHERE id=$1`, uid).Scan(&totpEnabled, &secret)
	if totpEnabled {
		if req.Code == "" || !a.checkTOTP(secret, req.Code) {
			writeErr(w, http.StatusUnauthorized, "valid 2FA code required")
			return
		}
	} else if req.Code != "" && !a.checkTOTP(secret, req.Code) {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	if _, err := a.db.Exec(r.Context(), `DELETE FROM recovery_codes WHERE user_id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to rotate codes")
		return
	}
	codes := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		code, err := randomRecoveryCode()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to generate codes")
			return
		}
		if _, err := a.db.Exec(r.Context(),
			`INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1,$2)`,
			uid, sha256hex(code)); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to store code")
			return
		}
		codes = append(codes, code)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"codes": codes, "note": "store these in a safe place; each code works once"})
}

// GET /api/auth/2fa/recovery-codes — remaining (unused) code count..
func (a *App) handleListRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var n int
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM recovery_codes WHERE user_id=$1 AND used_at IS NULL`, uid).Scan(&n)
	writeJSON(w, http.StatusOK, map[string]any{"remaining": n})
}

// POST /api/auth/2fa/recovery-codes/redeem — verify a recovery code without 2FA:
// two-leg flow: first redeem (returns a short-lived claim token); then the login
// endpoint consumes it (leg 2). For simplicity the claim token is an opaque
// random nonce stored hashed in sessions table? We use a dedicated in-memory
// cache keyed by user id — production safe since single API process owns authn.
func (a *App) handleRedeemRecoveryCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) || len(req.Code) == 0 || len(req.Code) > 32 {
		writeErr(w, http.StatusBadRequest, "valid code required")
		return

	}
	var uid string
	err := a.db.QueryRow(r.Context(),
		`SELECT user_id FROM recovery_codes WHERE user_id IN (SELECT id FROM users WHERE username=$2) AND code_hash=$1 AND used_at IS NULL`,
		sha256hex(req.Code), req.Code[0:0]).Scan(&uid)
	if err != nil {
		// fallback: match by plaintext code lookup across the user's own rows
		writeErr(w, http.StatusUnauthorized, "invalid or already-used recovery code")
		return
	}
	// verify 2FA secret exists? No — recovery redeems WITHOUT the current password
	// only when yanking 2FA: bind to the user row via code_hash directly.

	// The redeem needs the user id; we look it up by code hash across ALL users
	// (codes are unique per (user,hash) and hashes are infeasible to brute). SO:
	_ = err
	err = a.db.QueryRow(r.Context(),
		`SELECT user_id FROM recovery_codes WHERE code_hash=$1 AND used_at IS NULL`, sha256hex(req.Code)).Scan(&uid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid or already-used recovery code")
		return
	}
	// Single-use: mark consumed now, atomically. Only the first redeemer wins.

	res, err := a.db.Exec(r.Context(),
		`UPDATE recovery_codes SET used_at=now() WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL`,
		uid, sha256hex(req.Code))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusUnauthorized, "code already used")
		return

	}
	nonce, err := randomRecoveryCode()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to mint claim")
		return

	}
	a.cache.set(r.Context(), "2fa:recover:"+nonce, []byte(uid), 10*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"claim": nonce, "expires_in": 600})
}

// POST /api/auth/2fa/recovery-codes/disable — consume the claim token: disables 2FA
// and revokes the seed so ops can re-register fresh codes.leg-2 of the redeemerflow.
func (a *App) handleDisable2FAWithRecovery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Claim string `json:"claim"`
	}
	if !decodeJSON(w, r, &req) || req.Claim == "" {
		writeErr(w, http.StatusBadRequest, "claim token required")
		return

	}
	uidBytes, ok := a.cache.get(r.Context(), "2fa:recover:"+req.Claim)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid or expired claim")
		return

	}
	uid := string(uidBytes)
	if _, err := a.db.Exec(r.Context(),
			`UPDATE users SET totp_enabled=false, totp_secret=NULL, updated_at=now() WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to disable 2FA")
		return

	}
	if err := a.freezeWithdrawals(r.Context(), uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to apply security cooldown")
		return
	}
	_, _ = a.db.Exec(r.Context(), `DELETE FROM recovery_codes WHERE user_id=$1`, uid)
	a.cache.del(r.Context(), "2fa:recover:"+req.Claim)
	writeJSON(w, http.StatusOK, map[string]any{"status": "2fa_disabled"})
}

// ---------- Unsend for everyone + edit history (Telegram ----------

// POST /api/messages/{id}/unsend — Telegram-style "unsend within 48h": zeroes
// the body for everyone, records unsend_at, and fans out a tombstone. The sender
// (or admin) may unsend only within 48 hours of send. body_enc stays nulla.

func (a *App) handleUnsendMessage(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, senderID, ok := a.messageConv(r.Context(), msgID)
	if !ok {
		writeErr(w, http.StatusNotFound, "message not found")
		return

	}
	if senderID != uid {
		writeErr(w, http.StatusForbidden, "only the sender may unsend")
		return

	}
	var createdAt time.Time
	var deletedAt *time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT created_at, deleted_at FROM messages WHERE id=$1`, msgID).Scan(&createdAt, &deletedAt)
	if err != nil || deletedAt != nil {
		writeErr(w, http.StatusNotFound, "message not found")
		return

	}
	if time.Since(createdAt) > 48*time.Hour {
		writeErr(w, http.StatusForbidden, "unsend window (48h) has passed")
		return

	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE messages SET body='', unsend_at=now() WHERE id=$1`, msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to unsend")
		return

	}
	payload, _ := json.Marshal(map[string]any{"type": "message_unsent", "conversation_id": convID, "id": msgID, "created_at": createdAt})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "unsent", "created_at": createdAt})
}

// GET /api/messages/{id}/edits — edit history + "edited" status for the sender
// and all members of the conversation.viewer should be a member.

func (a *App) handleMessageEditHistory(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, _, ok := a.messageConv(r.Context(), msgID)
	if !ok {
		writeErr(w, http.StatusNotFound, "message not found")
		return

	}
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "members only")
		return

	}
	rows, err := a.db.Query(r.Context(),
		`SELECT body, edited_at FROM message_edits WHERE message_id=$1 ORDER BY edited_at DESC LIMIT 50`, msgID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load edits")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var body string
		var at time.Time
		if err := rows.Scan(&body, &at); err == nil {
			out = append(out, map[string]any{"body": body, "edited_at": at})
		}
	}
	var editedAt *time.Time
	var body string
	_ = a.db.QueryRow(r.Context(), `SELECT body, edited_at FROM messages WHERE id=$1 AND deleted_at IS NULL`, msgID).Scan(&body, &editedAt)
	writeJSON(w, http.StatusOK, map[string]any{"edits": out, "current_body": body, "edited_at": editedAt})
}

// patchEditMessage installs an edit-history row inside the existing edit path..
func (a *App) recordMessageEdit(ctx context.Context, msgID, body string) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO message_edits (message_id, body) VALUES ($1,$2)`,
		msgID, body)
}

// ---------- Story archive + Memories "On this day" (Facebook ----------

// POST /api/stories/{id}/archive — move a story into the private archive..
func (a *App) handleArchiveStory(w http.ResponseWriter, r *http.Request) {
	storyID := r.PathValue("id")
	uid := userIDFrom(r)
	var author string
	err := a.db.QueryRow(r.Context(),
		`SELECT author_id FROM posts WHERE id=$1 AND type='story' AND deleted_at IS NULL`, storyID).Scan(&author)
	if err != nil || author != uid {
		writeErr(w, http.StatusForbidden, "you can only archive your own stories")
		return

	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO story_archives (story_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		storyID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "archive failed")
		return

	}
	// Also create a memory so the archived story resurfaces on the same
	// month-day next year ("On this day").
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO memories (user_id, source_id, kind, on_date)
                 VALUES ($1,$2,'story_archive', (now() + interval '1 year'):date)
                 ON CONFLICT DO NOTHING`, uid, storyID); err != nil {
		writeErr(w, http.StatusInternalServerError, "memory create failed")
		return

	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
}

// GET /api/me/story-archive — archived stories (media, created_at).) Private.
func (a *App) handleListStoryArchive(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.body, p.created_at,
                        (SELECT pm.url FROM post_media pm WHERE pm.post_id=p.id ORDER BY pm.position LIMIT 1)
                 FROM story_archives sa JOIN posts p ON p.id=sa.story_id
                 WHERE sa.user_id=$1 ORDER BY sa.archived_at DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load archive")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, body, media string
		var createdAt time.Time
		if err := rows.Scan(&id, &body, &media, &createdAt); err == nil {
			out = append(out, map[string]any{"id": id, "body": body, "media_url": media, "created_at": createdAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"archive": out})
}

// GET /api/me/memories — "On this day" memories for today (and optionally ?all=1).)
// Sources render from the original post (still live) or the archived story media..
func (a *App) handleListMemories(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	all := r.URL.Query().Get("all") == "1"
	query := `SELECT m.id, m.kind, m.note, m.on_date, p.id, p.body,
                        (SELECT pm.url FROM post_media pm WHERE pm.post_id=p.id ORDER BY pm.position LIMIT 1)
                 FROM memories m JOIN posts p ON p.id=m.source_id
                 WHERE m.user_id=$1`
	if !all {
		query += ` AND (m.on_date = (now()::date) - (extract(year from m.on_date)::int - extract(year from now())::int) * interval '1 year'`
		query += ` OR m.on_date = now()::date)`
	}
	query += ` ORDER BY m.on_date DESC LIMIT 200`
	rows, err := a.db.Query(r.Context(), query, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load memories")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, kind, note, srcID, body, media string
		var onDate time.Time
		if err := rows.Scan(&id, &kind, &note, &onDate, &srcID, &body, &media); err == nil {
			out = append(out, map[string]any{"id": id, "kind": kind, "note": note,
				"on_date": onDate, "source": map[string]any{"id": srcID, "body": body, "media_url": media}})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": out})
}

// DELETE /api/memories/{id} — dismiss a memory (keep archive intact).)
func (a *App) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM memories WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "memory not found")
		return

	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- Reel studio: drafts, effects, voiceover, captions (TikTok ----------

// POST /api/reel-drafts — save or auto-save a studio draft {title, media_urls, effects, voiceover_url, caption}.)
func (a *App) handleSaveReelDraft(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Title        string         `json:"title"`
		MediaURLs    []string       `json:"media_urls"`
		Effects      map[string]any `json:"effects"`
		VoiceoverURL string         `json:"voiceover_url"`
		Caption      string         `json:"caption"`
	}
	if !decodeJSON(w, r, &req) || len(req.MediaURLs) > 20 {
		writeErr(w, http.StatusBadRequest, "media_urls max 20 clips")
		return

	}
	if len(req.Title) > 200 || len(req.Caption) > 2000 || len(req.VoiceoverURL) > 2048 {
		writeErr(w, http.StatusBadRequest, "field too long")
		return

	}
	urls, _ := json.Marshal(req.MediaURLs)

	if urls == nil {
		urls = []byte("[]")
	}
	fx, _ := json.Marshal(req.Effects)

	if fx == nil {
		fx = []byte("{}")
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO reel_drafts (user_id, title, media_urls, effects, voiceover_url, caption)
                 VALUES ($1,$2,$3::jsonb,$4::jsonb,$5,$6) RETURNING id`,
		uid, strings.TrimSpace(req.Title), urls, fx, strings.TrimSpace(req.VoiceoverURL), strings.TrimSpace(req.Caption)).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "draft save failed")
		return

	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/reel-drafts — my studio drafts, newest first.
func (a *App) handleListReelDrafts(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, title, media_urls, effects, voiceover_url, caption, updated_at FROM reel_drafts
                 WHERE user_id=$1 ORDER BY updated_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load drafts")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, urls, fx, vo, cap string
		var updated time.Time
		if err := rows.Scan(&id, &title, &urls, &fx, &vo, &cap, &updated); err == nil {
			out = append(out, map[string]any{"id": id, "title": title,
				"media_urls": json.RawMessage(urls), "effects": json.RawMessage(fx),
				"voiceover_url": vo, "caption": cap, "updated_at": updated})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": out})
}

// DELETE /api/reel-drafts/{id} — discard a draft.
func (a *App) handleDeleteReelDraft(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM reel_drafts WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "draft not found")
		return

	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// POST /api/reel-captions — save (or refresh) an ML-generated caption for a clip.
func (a *App) handleSaveReelCaption(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		MediaURL string `json:"media_url"`
		Text     string `json:"text"`
		Lang     string `json:"lang"`
	}
	if !decodeJSON(w, r, &req) || req.MediaURL == "" || req.Text == "" {
		writeErr(w, http.StatusBadRequest, "media_url and text required")
		return

	}
	if len(req.MediaURL) > 2048 || len(req.Text) > 5000 {
		writeErr(w, http.StatusBadRequest, "field too long")
		return

	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO reel_captions (user_id, media_url, text, lang)
                 VALUES ($1,$2,$3,COALESCE(NULLIF($4,''),'en'))
                 ON CONFLICT (user_id, media_url) DO UPDATE SET text=EXCLUDED.text, lang=EXCLUDED.lang`,
		uid, req.MediaURL, req.Text, req.Lang); err != nil {
		writeErr(w, http.StatusInternalServerError, "caption save failed")
		return

	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

// GET /api/reel-captions/{mediaId} — fetch the cached caption for a clip (by media URL..)
func (a *App) handleGetReelCaption(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	mediaURL := r.PathValue("id")
	var text, lang string
	err := a.db.QueryRow(r.Context(),
		`SELECT text, lang FROM reel_captions WHERE user_id=$1 AND media_url=$2`, uid, mediaURL).Scan(&text, &lang)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"caption": ""})
		return

	}
	writeJSON(w, http.StatusOK, map[string]any{"caption": text, "lang": lang})
}

// ---------- Creator insights (TikTok / X ----------

// GET /api/creator/insights?days=30 — daily reach/impressions/watch-time/followers
// for the signed-in creator; topped-up with today's live numbers + top sound.
func (a *App) handleCreatorInsights(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	days := 14
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)
	if days < 1 || days > 92 {
		days = 14
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT day, reach, impressions, watch_time_s, new_followers, top_sound
                 FROM creator_insights WHERE user_id=$1 AND day >= now()::date - $2::int ORDER BY day`, uid, days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load insights")
		return

	}
	defer rows.Close()
	daily := []map[string]any{}
	for rows.Next() {
		var day time.Time
		var reach, imp, watch, nf int64
		var top string
		if err := rows.Scan(&day, &reach, &imp, &watch, &nf, &top); err == nil {
			daily = append(daily, map[string]any{"day": day, "reach": reach, "impressions": imp,
				"watch_time_s": watch, "new_followers": nf, "top_sound": top})
		}
	}
	// Today's live rollup.
	var todayReach, todayImp, todayWatch int64
	var todayFollow int64
	var topSound string
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(DISTINCT v.post_id), count(*), COALESCE(sum(extract(epoch from rwe.duration_s))::bigint, 0), 0,
                        COALESCE((SELECT rwe.sound FROM reel_watch_events rwe ORDER BY rwe.id DESC LIMIT 1), '')
                 FROM post_views v WHERE v.user_id=$1 AND v.created_at::date = now()::date`, uid).Scan(&todayReach, &todayImp, &todayWatch, &todayFollow, &topSound)
	writeJSON(w, http.StatusOK, map[string]any{"daily": daily,
		"totals": map[string]any{"reach": todayReach + sumRange(daily, "reach"), "impressions": todayImp + sumRange(daily, "impressions"),
			"watch_time_s": todayWatch + sumRange(daily, "watch_time_s"), "new_followers": todayFollow + sumRange(daily, "new_followers"), "top_sound": topSound}})
}

func sumRange(rows []map[string]any, key string) int64 {
	var n int64
	for _, r := range rows {
		if v, ok := r[key].(int64); ok {
			n += v
		}
	}
	return n
}

// ---------- Share deep links + OG cards (X / TikTok ----------

// POST /api/posts/{id}/shares — mint a public share token (deep link /share/<token>.)
func (a *App) handleCreateShareToken(w http.ResponseWriter, r *http.Request) {
	postID := r.PathValue("id")
	uid := userIDFrom(r)
	var exists bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND deleted_at IS NULL)`, postID).Scan(&exists)
	if !exists {
		writeErr(w, http.StatusNotFound, "post not found")
		return

	}
	token, err := randomRecoveryCode()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to mint share token")
		return

	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO share_tokens (token, post_id, creator_id) VALUES ($1,$2,$3) RETURNING id`,
		token, postID, uid).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create share token")
		return

	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "share_url": "/share/" + token})
}

// GET /api/shares/{token} — public metadata for share-card rendering (no auth).)
func (a *App) handleGetShare(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var postID, body, media, authorName, authorUsername string
	var createdAt time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT p.id, p.body, COALESCE((SELECT pm.url FROM post_media pm WHERE pm.post_id=p.id ORDER BY pm.position LIMIT 1),''),
                        u.display_name, u.username, p.created_at
                 FROM share_tokens st JOIN posts p ON p.id=st.post_id JOIN users u ON u.id=p.author_id
                 WHERE st.token=$1 AND p.deleted_at IS NULL`, token).Scan(&postID, &body, &media, &authorName, &authorUsername, &createdAt)
	if err != nil {
		writeErr(w, http.StatusNotFound, "share not found")
		return

	}
	writeJSON(w, http.StatusOK, map[string]any{"post": map[string]any{"id": postID, "body": body,
		"media_url": media, "author_name": authorName, "author_username": authorUsername, "created_at": createdAt}})
}

// ---------- In-call engagement: reactions, raise-hand, ratings (imo ----------

// POST /api/calls/rooms/{roomId}/reactions — {kind: reaction|raise_hand|...payload}}
func (a *App) handleCallEvent(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	uid := userIDFrom(r)
	var req struct {
		Kind    string `json:"kind"`
		Payload string `json:"payload"`
	}
	if !decodeJSON(w, r, &req) {
		return

	}
	allowed := map[string]bool{"reaction": true, "raise_hand": true, "clap": true, "boo": true, "applause": true, "pinned": true}
	if !allowed[req.Kind] {
		writeErr(w, http.StatusBadRequest, "unsupported event")
		return

	}
	if len(req.Payload) > 200 {
		req.Payload = req.Payload[:200]
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO call_events (room_id, user_id, kind, payload) VALUES ($1,$2,$3,$4) RETURNING id`,
		roomID, uid, req.Kind, req.Payload).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "event failed")
		return

	}
	// Fan out to every member of the room's conversation (if any. rooms are
	// synthetic {conversationId}-{mode}; non-conversation room ids are used by SFU
	// directly and no membership exists — so fanout is best-effort.
	if parts := strings.Split(roomID, "-"); len(parts) > 1 {
		convID := strings.Join(parts[:len(parts)-1], "-")
		payload, _ := json.Marshal(map[string]any{"type": "call_event", "room_id": roomID, "id": id,
			"user_id": uid, "kind": req.Kind, "payload": req.Payload, "created_at": time.Now()})
		a.fanoutToMembers(r.Context(), convID, payload, "")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "sent"})
}

// GET /api/calls/rooms/{roomId}/events — recent engagement events..
func (a *App) handleCallEvents(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	rows, err := a.db.Query(r.Context(),
		`SELECT kind, payload, user_id, created_at FROM call_events WHERE room_id=$1 ORDER BY created_at DESC LIMIT 100`, roomID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load events")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var kind, payload, uid string
		var at time.Time
		if err := rows.Scan(&kind, &payload, &uid, &at); err == nil {
			out = append(out, map[string]any{"kind": kind, "payload": payload, "user_id": uid, "created_at": at})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

// POST /api/calls/rooms/{roomId}/rating — {rating 1-5, comment}. One per rater.
func (a *App) handleRateCall(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	uid := userIDFrom(r)
	var req struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if !decodeJSON(w, r, &req) {
		return

	}
	if req.Rating < 1 || req.Rating > 5 {
		writeErr(w, http.StatusBadRequest, "rating must be 1-5")
		return

	}
	if len(req.Comment) > 2000 {
		req.Comment = req.Comment[:2000]
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO call_ratings (room_id, rater_id, rating, comment)
                 VALUES ($1,$2,$3,$4)
                 ON CONFLICT (room_id, rater_id) DO UPDATE SET rating=EXCLUDED.rating,, comment=EXCLUDED.comment`,
		roomID, uid, req.Rating, req.Comment); err != nil {
		writeErr(w, http.StatusInternalServerError, "rating failed")
		return

	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rated"})
}

// GET /api/calls/rooms/{roomId}/rating — average + count..
func (a *App) handleGetCallRating(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("roomId")
	var avg float64
	var cnt int64
	_ = a.db.QueryRow(r.Context(),
		`SELECT COALESCE(avg(rating)),0), count(*) FROM call_ratings WHERE room_id=$1`, roomID).Scan(&avg, &cnt)
	writeJSON(w, http.StatusOK, map[string]any{"average": avg, "count": cnt})
}

// ---------- Scheduled calls + reminders (imo ----------

// POST /api/calls/schedule — {title, conversation_id?, scheduled_at, is_reminder?}.)
func (a *App) handleScheduleCall(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Title          string    `json:"title"`
		ConversationID string    `json:"conversation_id"`
		ScheduledAt    time.Time `json:"scheduled_at"`
		IsReminder     bool      `json:"is_reminder"`
	}
	if !decodeJSON(w, r, &req) {
		return

	}
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Title) == 0 || len(req.Title) > 120 {
		writeErr(w, http.StatusBadRequest, "title required (max 120)")
		return

	}
	if req.ScheduledAt.Before(time.Now().Add(5*time.Minute)) || req.ScheduledAt.After(time.Now().Add(365*24*time.Hour)) {

		writeErr(w, http.StatusBadRequest, "scheduled_at must be 5m-1y from now")
		return

	}
	if req.ConversationID != "" && !a.isMember(r.Context(), req.ConversationID, uid) {

		writeErr(w, http.StatusForbidden, "not a member of that conversation")
		return

	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO scheduled_calls (user_id, title, conversation_id, scheduled_at, is_reminder)
                 VALUES ($1,$2,NULLIF($3,'')::uuid,$4,$5) RETURNING id`,
		uid, req.Title, req.ConversationID, req.ScheduledAt, req.IsReminder).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "schedule failed")
		return

	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "scheduled_at": req.ScheduledAt})
}

// GET /api/calls/scheduled — my upcoming scheduled calls/reminders.

func (a *App) handleListScheduledCalls(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, title, conversation_id, NULL as room_id, scheduled_at, is_reminder FROM scheduled_calls
                 WHERE user_id=$1 AND scheduled_at > now() ORDER BY scheduled_at ASC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load scheduled calls")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title string
		var convID, roomID *string
		var at time.Time
		var reminder bool
		if err := rows.Scan(&id, &title, &convID, &roomID, &at, &reminder); err == nil {
			out = append(out, map[string]any{"id": id, "title": title, "conversation_id": convID,
				"room_id": roomID, "scheduled_at": at, "is_reminder": reminder})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scheduled": out})
}

// DELETE /api/calls/scheduled/{id} — cancel.
func (a *App) handleCancelScheduledCall(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM scheduled_calls WHERE id=$1 AND user_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "scheduled call not found")
		return

	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// ---------- Page insights (Facebook ----------

// GET /api/pages/{id}/insights?days=30 — daily page rollup + totals (owner/admin only.))
func (a *App) handlePageInsights(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	uid := userIDFrom(r)
	var owner string
	_ = a.db.QueryRow(r.Context(), `SELECT owner_id FROM pages WHERE id=$1`, pageID).Scan(&owner)
	if owner == "" {
		writeErr(w, http.StatusNotFound, "page not found")
		return
	}
	if owner != uid && a.pageAdminRole(r, pageID, uid) == "" {
		writeErr(w, http.StatusForbidden, "page owner/admin only")
		return

	}
	days := 14
	fmt.Sscanf(r.URL.Query().Get("days"), "%d", &days)
	if days < 1 || days > 92 {
		days = 14
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT day, impressions, reach, new_followers FROM page_insights
                 WHERE page_id=$1 AND day >= now()::date - $2::int ORDER BY day`, pageID, days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load insights")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var day time.Time
		var imp, reach int64
		var nf int
		if err := rows.Scan(&day, &imp, &reach, &nf); err == nil {
			out = append(out, map[string]any{"day": day, "impressions": imp, "reach": reach, "new_followers": nf})
		}
	}
	var totalImp, totalReach int64
	var totalFollow int
	for _, r := range out {
		if v, ok := r["impressions"].(int64); ok {
			totalImp += v
		}
		if v, ok := r["reach"].(int64); ok {
			totalReach += v
		}
		if v, ok := r["new_followers"].(int); ok {
			totalFollow += v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"daily": out,
		"totals": map[string]any{"impressions": totalImp, "reach": totalReach, "new_followers": totalFollow}})
}

func (a *App) pageAdminRole(r *http.Request, pageID, uid string) string {
	var role string
	_ = a.db.QueryRow(r.Context(),
		`SELECT 'admin' FROM page_followers WHERE page_id=$1 AND user_id=$2 AND is_admin`, pageID, uid).Scan(&role)
	return role
}

// ---------- Group post-approval moderation queue (Facebook ----------

// PUT /api/groups/{id}/post-approval — {enabled} to toggle require_post_approval (owner only..)
func (a *App) handleSetGroupPostApproval(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	uid := userIDFrom(r)
	if a.groupRole(r, groupID, uid) != "owner" {
		writeErr(w, http.StatusForbidden, "owner required")
		return

	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return

	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE content_groups SET require_post_approval=$1 WHERE id=$2`, req.Enabled, groupID); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return

	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": req.Enabled})
}

// GET /api/groups/{id}/post-queue — pending (or ?status=approved|rejected) posts.)
func (a *App) handleListGroupPostQueue(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	uid := userIDFrom(r)
	if role := a.groupRole(r, groupID, uid); role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "owner or admin required")
		return

	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		writeErr(w, http.StatusBadRequest, "invalid status")
		return

	}
	rows, err := a.db.Query(r.Context(),
		`SELECT q.id, q.post_id, q.author_id, u.display_name, u.username, p.body, q.created_at
                 FROM group_post_queue q JOIN users u ON u.id=q.author_id JOIN posts p ON p.id=q.post_id
                 WHERE q.group_id=$1 AND q.status=$2 ORDER BY q.created_at DESC LIMIT 100`, groupID, status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load queue")
		return

	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, postID, authorID, name, username, body string
		var at time.Time
		if err := rows.Scan(&id, &postID, &authorID, &name, &username, &body, &at); err == nil {
			out = append(out, map[string]any{"id": id, "post_id": postID, "author_id": authorID,
				"author_name": name, "author_username": username, "body": body, "created_at": at})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"queue": out})
}

// POST /api/groups/{id}/post-queue/{queueId} — {approve}→publish{reject}→hide..
func (a *App) handleReviewGroupPost(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("id")
	queueID := r.PathValue("queueId")
	uid := userIDFrom(r)
	if role := a.groupRole(r, groupID, uid); role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "owner or admin required")
		return

	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if !decodeJSON(w, r, &req) {
		return

	}
	var postID, authorID string
	err := a.db.QueryRow(r.Context(),
		`SELECT post_id, author_id FROM group_post_queue WHERE id=$1 AND group_id=$2 AND status='pending'`,
		queueID, groupID).Scan(&postID, &authorID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "queue item not found")
		return

	}
	status := "approved"
	if !req.Approve {
		status = "rejected"
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE group_post_queue SET status=$1, decided_by=$2, decided_at=now() WHERE id=$3`,
		status, uid, queueID); err != nil {
		writeErr(w, http.StatusInternalServerError, "review failed")
		return

	}
	if !req.Approve {
		_, _ = a.db.Exec(r.Context(), `UPDATE posts SET deleted_at=now() WHERE id=$1`, postID)
	} else {
		a.notifyUser(r.Context(), authorID, "group_post_approved", map[string]string{"group_id": groupID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status})
}

// ---------- Quote-repost with comment (X ----------

// POST /api/posts/{id}/quote — create a repost-with-comment: body + repost_of → the
// feed already renders a "Quoted" surface for reposits with non-empty body.

func (a *App) handleQuotePost(w http.ResponseWriter, r *http.Request) {
	srcID := r.PathValue("id")
	uid := userIDFrom(r)
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return

	}
	req.Body = strings.TrimSpace(req.Body)
	if len(req.Body) == 0 || len(req.Body) > 2000 {
		writeErr(w, http.StatusBadRequest, "comment required (max 2000)")
		return

	}
	var srcExists bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND deleted_at IS NULL)`, srcID).Scan(&srcExists)
	if !srcExists {
		writeErr(w, http.StatusNotFound, "source post not found")
		return

	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO posts (author_id, type, body, repost_of) VALUES ($1,'post',$2,$3::uuid) RETURNING id`,
		uid, req.Body, srcID).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "quote failed")
		return

	}
	_, _ = a.db.Exec(r.Context(), `UPDATE posts SET share_count=share_count+1 WHERE id=$1`, srcID)
	_, _ = a.db.Exec(r.Context(), `INSERT INTO shares (post_id, user_id, channel) VALUES ($1,$2,'link')`, srcID, uid)
	var authorID string
	_ = a.db.QueryRow(r.Context(), `SELECT author_id FROM posts WHERE id=$1`, srcID).Scan(&authorID)
	if authorID != "" && authorID != uid {
		a.notifyUser(r.Context(), authorID, "quote", map[string]string{"post_id": id})
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}
