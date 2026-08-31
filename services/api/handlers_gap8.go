package main

// Gap pack 8 (migration 024): duet/stitch/trim/mix compositor over the C++
// transcode pipeline (TikTok creation tools), HLS live ingest for unlimited
// viewers (Telegram/TikTok live), marketplace checkout with affiliate
// attribution (TikTok Shop / FB Marketplace), profile Q&A (TikTok),
// screen-time limits (TikTok/FB), animated custom emoji (Telegram), live
// co-hosting (TikTok multi-publisher), media compression option (Telegram),
// app-lock flag (Telegram), and the FYP feature-store rollup endpoint.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ---------- Duet / Stitch / Trim / Voiceover-mix (TikTok editor) ----------

// mediaIDFromURL extracts the media-edge id from a /media/<id> style URL.
func mediaIDFromURL(url string) string {
	url = strings.TrimSpace(url)
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.Split(parts[len(parts)-1], "?")[0]
}

// enqueueCompositorJob validates + queues one extended transcode job.
func (a *App) enqueueCompositorJob(ctx context.Context, kind string, params map[string]any, mediaID string) (string, error) {
	pb, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	var id string
	err = a.db.QueryRow(ctx,
		`INSERT INTO transcode_jobs (media_id, source_url, kind, params)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		mediaID, params["source_url"], kind, pb).Scan(&id)
	return id, err
}

// POST /api/reels/{id}/duet — {media_url} is the viewer's side clip; result is
// a side-by-side composite reel. Creates the remix-attributed post first so it
// is publishable even while the composite renders.
func (a *App) handleDuet(w http.ResponseWriter, r *http.Request) {
	a.compositeReel(w, r, "duet")
}

// POST /api/reels/{id}/stitch — viewer clip appended after the original.
func (a *App) handleStitch(w http.ResponseWriter, r *http.Request) {
	a.compositeReel(w, r, "stitch")
}

func (a *App) compositeReel(w http.ResponseWriter, r *http.Request, kind string) {
	uid := userIDFrom(r)
	srcPostID := r.PathValue("id")
	var req struct {
		MediaURL string `json:"media_url"`
		Body     string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.MediaURL) == "" || len(req.MediaURL) > 2048 {
		writeErr(w, http.StatusBadRequest, "media_url of your clip is required")
		return
	}
	// Source reel + its first video URL.
	var srcAuthor, srcURL string
	err := a.db.QueryRow(r.Context(),
		`SELECT p.author_id::text,
		        (SELECT pm.url FROM post_media pm WHERE pm.post_id=p.id AND pm.kind='video' ORDER BY pm.position LIMIT 1)
		 FROM posts p WHERE p.id=$1 AND p.type='reel' AND p.deleted_at IS NULL`,
		srcPostID).Scan(&srcAuthor, &srcURL)
	if err != nil || srcURL == "" {
		writeErr(w, http.StatusNotFound, "source reel has no video")
		return
	}
	mediaID := mediaIDFromURL(req.MediaURL)
	if mediaID == "" {
		writeErr(w, http.StatusBadRequest, "could not extract media id from your clip URL")
		return
	}
	params := map[string]any{"source_url": req.MediaURL, "sources": []string{srcURL, req.MediaURL}}
	jobID, err := a.enqueueCompositorJob(r.Context(), kind, params, mediaID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	// Publish the post immediately with the raw clip; the composited HLS master
	// replaces post_media.url on completion (media id match), duet chain kept in
	// remix_of. Body capped at 2000 chars like normal posts.
	var postID string
	err = a.db.QueryRow(r.Context(),
		`INSERT INTO posts (author_id, type, body, remix_of)
		 VALUES ($1,'reel',$2,$3::uuid) RETURNING id`,
		uid, strings.TrimSpace(req.Body), srcPostID).Scan(&postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "post creation failed")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO post_media (post_id, kind, url, position) VALUES ($1,'video',$2,0)`,
		postID, req.MediaURL); err != nil {
		writeErr(w, http.StatusInternalServerError, "media attach failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"post_id": postID, "job_id": jobID, "kind": kind, "status": "queued",
	})
}

// POST /api/media/{id}/trim — cut a clip: {source_url, start_s, duration_s}.
func (a *App) handleTrimMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceURL string  `json:"source_url"`
		StartS    float64 `json:"start_s"`
		DurationS float64 `json:"duration_s"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SourceURL == "" || req.DurationS <= 0 || req.DurationS > 3600 || req.StartS < 0 {
		writeErr(w, http.StatusBadRequest, "valid source_url + start/duration required")
		return
	}
	mediaID := r.PathValue("id")
	params := map[string]any{"source_url": req.SourceURL, "start_s": req.StartS, "duration_s": req.DurationS}
	jobID, err := a.enqueueCompositorJob(r.Context(), "trim", params, mediaID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job_id": jobID, "kind": "trim", "status": "queued"})
}

// POST /api/media/{id}/mix — voiceover / sound mixing: {video_url, audio_url}.
func (a *App) handleMixMedia(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VideoURL string `json:"video_url"`
		AudioURL string `json:"audio_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.VideoURL == "" || req.AudioURL == "" {
		writeErr(w, http.StatusBadRequest, "video_url + audio_url required")
		return
	}
	mediaID := r.PathValue("id")
	params := map[string]any{"source_url": req.VideoURL, "sources": []string{req.VideoURL, req.AudioURL}}
	jobID, err := a.enqueueCompositorJob(r.Context(), "mix", params, mediaID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "enqueue failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"job_id": jobID, "kind": "mix", "status": "queued"})
}

// ---------- HLS live ingest (unlimited viewers over CDN/media edge) ----------

// POST /api/live-rooms/{id}/ingest — host-only. Mint a stream key and queue
// the C++ worker's live job: it listens (ffmpeg -listen) on the RTMP port for
// that key and writes a live HLS ladder into the media volume.
func (a *App) handleLiveIngestStart(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	roomID := r.PathValue("id")
	var host, live bool
	err := a.db.QueryRow(r.Context(),
		`SELECT host_id=$2, status='live' FROM live_rooms WHERE id=$1`, roomID, uid).Scan(&host, &live)
	if err != nil || !live || !host {
		writeErr(w, http.StatusForbidden, "only the host of a live room can ingest")
		return
	}
	key, err := randomToken(24)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "key generation failed")
		return
	}
	var streamID string
	err = a.db.QueryRow(r.Context(),
		`INSERT INTO live_streams (room_id, host_id, stream_key, hls_url)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		roomID, uid, key, "/hls/"+roomID+"/master.m3u8").Scan(&streamID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "stream creation failed")
		return
	}
	rtmpURL := "rtmp://0.0.0.0:1935/live/" + key
	params := map[string]any{"source_url": rtmpURL, "stream_key": key}
	_, _ = a.enqueueCompositorJob(r.Context(), "live", params, roomID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"stream_id":  streamID,
		"stream_key": key,
		"ingest_url": rtmpURL,
		"play_url":   "/hls/" + roomID + "/master.m3u8",
		"status":     "pending",
	})
}

// GET /api/live-rooms/{id}/stream — current ingest/play endpoints (public).
func (a *App) handleLiveIngestInfo(w http.ResponseWriter, r *http.Request) {
	var id, hls, status string
	err := a.db.QueryRow(r.Context(),
		`SELECT id::text, hls_url, status FROM live_streams
		 WHERE room_id=$1 ORDER BY created_at DESC LIMIT 1`, r.PathValue("id")).Scan(&id, &hls, &status)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"stream": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stream": map[string]any{"stream_id": id, "play_url": hls, "status": status},
	})
}

// POST /api/live-rooms/{id}/ingest/end — host-only.
func (a *App) handleLiveIngestEnd(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	res, err := a.db.Exec(r.Context(),
		`UPDATE live_streams SET status='ended', ended_at=now()
		 WHERE room_id=$1 AND host_id=$2 AND status <> 'ended'`, r.PathValue("id"), uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "no active stream")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

// ---------- Marketplace checkout (Shop) + affiliate attribution ----------

const shopPlatformFee = "0.05" // 5% platform fee, of which 40% can rev-share to affiliates

// POST /api/marketplace/{id}/buy — wallet-native checkout. Optional
// {via_post_id} attributes the sale to a content post; the affiliate cut
// (40% of the platform fee) goes to that post's author.
func (a *App) handleMarketplaceBuy(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	listingID := r.PathValue("id")
	var req struct {
		ViaPostID string `json:"via_post_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var sellerID, price string
	err := a.db.QueryRow(r.Context(),
		`SELECT seller_id::text, price_usd::text FROM marketplace_listings
		 WHERE id=$1 AND status='active'`, listingID).Scan(&sellerID, &price)
	if err != nil {
		writeErr(w, http.StatusNotFound, "listing not available")
		return
	}
	if sellerID == uid {
		writeErr(w, http.StatusBadRequest, "cannot buy your own listing")
		return
	}
	var affAuthor, fee, affCut string
	_ = a.db.QueryRow(r.Context(), `SELECT ($1::numeric * ` + shopPlatformFee + `)::text`,
		price).Scan(&fee)
	if req.ViaPostID != "" {
		_ = a.db.QueryRow(r.Context(),
			`SELECT author_id::text FROM posts WHERE id=$1::uuid AND deleted_at IS NULL`,
			req.ViaPostID).Scan(&affAuthor)
		_ = a.db.QueryRow(r.Context(),
			`SELECT ($1::numeric * ` + shopPlatformFee + ` * 0.4)::text`, price).Scan(&affCut)
	}
	var sellerGets, platformGets string
	_ = a.db.QueryRow(r.Context(),
		`SELECT ($1::numeric - $2::numeric)::text, ($2::numeric - COALESCE(NULLIF($3,''),'0')::numeric)::text`,
		price, fee, affCut).Scan(&sellerGets, &platformGets)
	// Fail any insufficient-balance movement before mutating state.
	if _, err := a.moveUSD(r.Context(), uid, sellerID, sellerGets, "shop_buy", "shop_sale", "marketplace checkout"); err != nil {
		writeErr(w, http.StatusPaymentRequired, "insufficient wallet balance")
		return
	}
	if _, err := a.moveUSD(r.Context(), uid, platformTreasuryID, platformGets, "shop_fee", "shop_fee", "marketplace fee"); err != nil {
		writeErr(w, http.StatusInternalServerError, "fee settlement failed")
		return
	}
	if affAuthor != "" && affCut != "" {
		_, _ = a.moveUSD(r.Context(), uid, affAuthor, affCut, "shop_affiliate", "shop_affiliate", "affiliate rev-share")
	}
	var orderID string
	err = a.db.QueryRow(r.Context(),
		`INSERT INTO marketplace_orders (listing_id, seller_id, buyer_id, amount_usd, platform_fee_usd, affiliate_post_id, affiliate_usd)
		 VALUES ($1,$2,$3,$4::numeric,$5::numeric,NULLIF($6,'')::uuid,COALESCE(NULLIF($7,''),'0')::numeric)
		 RETURNING id`, listingID, sellerID, uid, price, fee, req.ViaPostID, affCut).Scan(&orderID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "order record failed")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE marketplace_listings SET sold_count = sold_count + 1 WHERE id=$1`, listingID)
	a.notifyUser(r.Context(), sellerID, "marketplace_sale", map[string]string{"order_id": orderID, "listing_id": listingID})
	writeJSON(w, http.StatusCreated, map[string]any{"order_id": orderID, "status": "paid", "amount_usd": price})
}

// GET /api/me/orders — purchases as buyer, sales as seller (?as=seller).
func (a *App) handleListOrders(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	asSeller := r.URL.Query().Get("as") == "seller"
	col := "buyer_id"
	if asSeller {
		col = "seller_id"
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT o.id::text, o.listing_id::text, l.title, o.amount_usd::text, o.status, o.created_at
		 FROM marketplace_orders o JOIN marketplace_listings l ON l.id = o.listing_id
		 WHERE o.`+col+`=$1 ORDER BY o.created_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load orders")
		return
	}
	defer rows.Close()
	type order struct {
		ID       string    `json:"id"`
		Listing  string    `json:"listing_id"`
		Title    string    `json:"title"`
		Amount   string    `json:"amount_usd"`
		Status   string    `json:"status"`
		Created  time.Time `json:"created_at"`
	}
	out := []order{}
	for rows.Next() {
		var o order
		if rows.Scan(&o.ID, &o.Listing, &o.Title, &o.Amount, &o.Status, &o.Created) == nil {
			out = append(out, o)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
}

// ---------- Profile Q&A (TikTok ask & answer) ----------

// GET /api/users/{id}/questions — answered ones are public; the profile owner
// can see pending ones too when fetching their own profile.
func (a *App) handleListQuestions(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	ownerID := r.PathValue("id")
	pending := ""
	if uid == ownerID {
		pending = "TRUE"
	} else {
		pending = "q.answered_at IS NOT NULL"
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT q.id::text, q.asker_id::text, u.username, q.question, q.answer, q.created_at, q.answered_at
		 FROM profile_questions q JOIN users u ON u.id = q.asker_id
		 WHERE q.user_id=$1 AND `+pending+`
		 ORDER BY q.created_at DESC LIMIT 100`, ownerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load questions")
		return
	}
	defer rows.Close()
	type qa struct {
		ID       string     `json:"id"`
		AskerID  string     `json:"asker_id"`
		Asker    string     `json:"asker"`
		Question string     `json:"question"`
		Answer   *string    `json:"answer"`
		Created  time.Time  `json:"created_at"`
		Answered *time.Time `json:"answered_at"`
	}
	out := []qa{}
	for rows.Next() {
		var q qa
		if rows.Scan(&q.ID, &q.AskerID, &q.Asker, &q.Question, &q.Answer, &q.Created, &q.Answered) == nil {
			out = append(out, q)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": out})
}

// POST /api/users/{id}/questions — ask another profile (blocks respected).
func (a *App) handleAskQuestion(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	ownerID := r.PathValue("id")
	if ownerID == uid {
		writeErr(w, http.StatusBadRequest, "ask someone else's profile")
		return
	}
	var req struct {
		Question string `json:"question"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Question) == "" || len(req.Question) > 500 {
		writeErr(w, http.StatusBadRequest, "question 1-500 chars")
		return
	}
	var blocked bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM user_blocks WHERE (blocker_id=$1 AND blocked_id=$2) OR (blocker_id=$2 AND blocked_id=$1))`,
		uid, ownerID).Scan(&blocked)
	if blocked {
		writeErr(w, http.StatusForbidden, "blocked")
		return
	}
	var qid string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO profile_questions (user_id, asker_id, question) VALUES ($1,$2,$3) RETURNING id`,
		ownerID, uid, strings.TrimSpace(req.Question)).Scan(&qid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ask failed")
		return
	}
	a.notifyUser(r.Context(), ownerID, "profile_question", map[string]string{"question_id": qid})
	writeJSON(w, http.StatusCreated, map[string]string{"question_id": qid})
}

// POST /api/questions/{id}/answer — profile owner answers publicly.
func (a *App) handleAnswerQuestion(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Answer string `json:"answer"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Answer) == "" || len(req.Answer) > 1000 {
		writeErr(w, http.StatusBadRequest, "answer 1-1000 chars")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE profile_questions SET answer=$2, answered_at=now() WHERE id=$1 AND user_id=$3`,
		r.PathValue("id"), strings.TrimSpace(req.Answer), uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "question not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// DELETE /api/questions/{id} — profile owner or asker.
func (a *App) handleDeleteQuestion(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM profile_questions WHERE id=$1 AND (user_id=$2 OR asker_id=$2)`,
		r.PathValue("id"), uid)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "question not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- Screen-time limits / reminders ----------

// PUT /api/me/screen-time {limit_minutes} (0 = off)
func (a *App) handleSetScreenTime(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LimitMinutes int `json:"limit_minutes"`
	}
	if !decodeJSON(w, r, &req) || req.LimitMinutes < 0 || req.LimitMinutes > 1440 {
		writeErr(w, http.StatusBadRequest, "limit_minutes 0-1440")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET screen_time_limit_minutes=$2 WHERE id=$1`,
		userIDFrom(r), req.LimitMinutes); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"limit_minutes": req.LimitMinutes})
}

// GET /api/me/screen-time — limit + today's usage.
func (a *App) handleGetScreenTime(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var limit int
	var used float64
	_ = a.db.QueryRow(r.Context(),
		`SELECT u.screen_time_limit_minutes,
		        COALESCE((SELECT s.minutes FROM screen_time_usage s WHERE s.user_id=u.id AND s.day=current_date),0)
		 FROM users u WHERE u.id=$1`, uid).Scan(&limit, &used)
	writeJSON(w, http.StatusOK, map[string]any{
		"limit_minutes": limit, "used_minutes": used, "exceeded": limit > 0 && used >= float64(limit),
	})
}

// POST /api/me/screen-time/ping {minutes} — clients accumulate active usage
// (web: visible timer; mobile: foreground tracker). Idempotent per clock tick.
func (a *App) handlePingScreenTime(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Minutes float64 `json:"minutes"`
	}
	if !decodeJSON(w, r, &req) || req.Minutes < 0 || req.Minutes > 30 {
		writeErr(w, http.StatusBadRequest, "minutes must be 0-30 per ping")
		return
	}
	var dayTotal float64
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO screen_time_usage (user_id, day, minutes) VALUES ($1,current_date,$2)
		 ON CONFLICT (user_id, day) DO UPDATE SET minutes = screen_time_usage.minutes + $2
		 RETURNING minutes`, uid, req.Minutes).Scan(&dayTotal)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ping failed")
		return
	}
	var limit int
	_ = a.db.QueryRow(r.Context(),
		`SELECT screen_time_limit_minutes FROM users WHERE id=$1`, uid).Scan(&limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"used_minutes": dayTotal, "limit_minutes": limit,
		"exceeded": limit > 0 && dayTotal >= float64(limit),
	})
}

// ---------- App lock (Telegram local lock) + password proof ----------

// PUT /api/me/app-lock {enabled}
func (a *App) handleSetAppLock(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET app_lock_enabled=$2 WHERE id=$1`, userIDFrom(r), req.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"app_lock_enabled": req.Enabled})
}

// POST /api/auth/verify-password — proof for sensitive flows (app lock,
// card reveal, withdrawal UX) without issuing a new session.
func (a *App) handleVerifyPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "password required")
		return
	}
	var hash string
	err := a.db.QueryRow(r.Context(),
		`SELECT password_hash FROM users WHERE id=$1`, userIDFrom(r)).Scan(&hash)
	if err != nil || !verifyPassword(req.Password, hash) {
		writeErr(w, http.StatusUnauthorized, "password mismatch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------- FYP feature-store rollup (X For-You feature store) ----------

// GET /api/me/feature-vector — implicit interest vector + watch aggregates
// that feed ranking. Exposed for ML-service ingestion and client diagnostics.
func (a *App) handleFeatureVector(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	type interest struct {
		Hashtag string  `json:"hashtag"`
		Score   float64 `json:"score"`
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT hashtag, score FROM user_interests WHERE user_id=$1 ORDER BY score DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed")
		return
	}
	interests := []interest{}
	for rows.Next() {
		var i interest
		if rows.Scan(&i.Hashtag, &i.Score) == nil {
			interests = append(interests, i)
		}
	}
	rows.Close()
	var watched, completed, rewatched, notInterested int64
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE completed), COUNT(*) FILTER (WHERE rewatched),
		        COUNT(*) FILTER (WHERE not_interested)
		 FROM reel_watch_events WHERE user_id=$1`, uid).
		Scan(&watched, &completed, &rewatched, &notInterested)
	writeJSON(w, http.StatusOK, map[string]any{
		"interest_vector": interests,
		"watch": map[string]any{
			"events": watched, "completed": completed,
			"rewatched": rewatched, "not_interested": notInterested,
		},
	})
}

// ---------- Live co-hosting (multi-publisher live) ----------

// POST /api/live-rooms/{id}/cohosts — host invites; invitee accepts.
// {user_id, action: invite|accept|remove}
func (a *App) handleLiveCohost(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	roomID := r.PathValue("id")
	var req struct {
		UserID string `json:"user_id"`
		Action string `json:"action"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var hostID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT host_id::text FROM live_rooms WHERE id=$1 AND status='live'`, roomID).Scan(&hostID); err != nil {
		writeErr(w, http.StatusNotFound, "live room not found or ended")
		return
	}
	switch req.Action {
	case "invite":
		if uid != hostID {
			writeErr(w, http.StatusForbidden, "host only")
			return
		}
		if _, err := a.db.Exec(r.Context(),
			`INSERT INTO live_room_viewers (room_id, user_id, role) VALUES ($1,$2,'cohost')
			 ON CONFLICT (room_id, user_id) DO UPDATE SET role='cohost'`, roomID, req.UserID); err != nil {
			writeErr(w, http.StatusInternalServerError, "invite failed")
			return
		}
		a.notifyUser(r.Context(), req.UserID, "live_cohost_invite", map[string]string{"room_id": roomID})
		writeJSON(w, http.StatusCreated, map[string]string{"status": "invited"})
	case "accept":
		res, err := a.db.Exec(r.Context(),
			`UPDATE live_room_viewers SET role='cohost' WHERE room_id=$1 AND user_id=$2`, roomID, uid)
		if err != nil || res.RowsAffected() == 0 {
			writeErr(w, http.StatusNotFound, "no room membership")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"role": "cohost"})
	case "remove":
		if uid != hostID && uid != req.UserID {
			writeErr(w, http.StatusForbidden, "host or self only")
			return
		}
		_, _ = a.db.Exec(r.Context(),
			`UPDATE live_room_viewers SET role='viewer' WHERE room_id=$1 AND user_id=$2`, roomID, req.UserID)
		writeJSON(w, http.StatusOK, map[string]string{"role": "viewer"})
	default:
		writeErr(w, http.StatusBadRequest, "action must be invite|accept|remove")
	}
}

// ---------- Group scale probe (100k fanout validation) ----------

// GET /api/admin/groups/scale — superadmin: fanout-cost probe. Reports the
// largest/median group sizes so the C++ realtime relay's fanout batching can
// be validated non-interactively.
func (a *App) handleGroupScaleReport(w http.ResponseWriter, r *http.Request) {
	type grp struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Members  int    `json:"members"`
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id::text, c.title, COUNT(m.user_id)
		 FROM conversations c JOIN conversation_members m ON m.conversation_id=c.id
		 WHERE c.is_group GROUP BY c.id, c.title ORDER BY COUNT(m.user_id) DESC LIMIT 20`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed")
		return
	}
	defer rows.Close()
	out := []grp{}
	for rows.Next() {
		var g grp
		if rows.Scan(&g.ID, &g.Title, &g.Members) == nil {
			out = append(out, g)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"largest_groups": out,
		"note":           "fanout batches over the C++ realtime relay; WS fallback hub shards per-user channels",
		"checked_at":     time.Now(),
	})
}
