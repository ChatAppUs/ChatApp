package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Messaging pack: polls attached to chat messages, video notes, live
// location sharing, and pay-in-chat. All persist as first-class messages
// (messages.kind) and fan out over the realtime relay.

// ---------- Chat polls ----------

// POST /api/conversations/{id}/polls — create a poll message in the chat.
func (a *App) handleCreateChatPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
		Multi    bool     `json:"multi"`
		ClosesIn int      `json:"closes_in_minutes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" || len(req.Question) > 300 {
		writeErr(w, http.StatusBadRequest, "question required (300 chars max)")
		return
	}
	opts := make([]string, 0, len(req.Options))
	for _, o := range req.Options {
		o = strings.TrimSpace(o)
		if o != "" && len(o) <= 100 {
			opts = append(opts, o)
		}
	}
	if len(opts) < 2 || len(opts) > 10 {
		writeErr(w, http.StatusBadRequest, "polls need 2-10 options")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create poll")
		return
	}
	defer tx.Rollback(r.Context())
	var msgID string
	var closesAt *time.Time
	if req.ClosesIn > 0 && req.ClosesIn <= 7*24*60 {
		t := time.Now().Add(time.Duration(req.ClosesIn) * time.Minute)
		closesAt = &t
	}
	err = tx.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, kind)
                 VALUES ($1,$2,$3,'poll') RETURNING id`, convID, uid, req.Question).Scan(&msgID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create poll")
		return
	}
	var pollID string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO message_polls (message_id, question, multi, closes_at)
                 VALUES ($1,$2,$3,$4) RETURNING id`, msgID, req.Question, req.Multi, closesAt).Scan(&pollID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create poll")
		return
	}
	for i, o := range opts {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO message_poll_options (poll_id, label, position) VALUES ($1,$2,$3)`,
			pollID, o, i); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create poll")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create poll")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "id": msgID, "conversation_id": convID,
		"sender_id": uid, "body": req.Question, "kind": "poll",
		"poll_id": pollID, "created_at": time.Now(),
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusCreated, map[string]string{"id": pollID, "message_id": msgID})
}

// POST /api/chat-polls/{id}/vote — vote (multi polls allow several options;
// re-voting the same option removes the vote).
func (a *App) handleChatPollVote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OptionID string `json:"option_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	pollID := r.PathValue("id")
	var convID string
	var multi bool
	var closesAt *time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT m.conversation_id, p.multi, p.closes_at
                 FROM message_polls p JOIN messages m ON m.id = p.message_id
                 WHERE p.id=$1`, pollID).Scan(&convID, &multi, &closesAt)
	if err != nil {
		writeErr(w, http.StatusNotFound, "poll not found")
		return
	}
	if closesAt != nil && closesAt.Before(time.Now()) {
		writeErr(w, http.StatusBadRequest, "poll is closed")
		return
	}
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var validOption bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM message_poll_options WHERE id=$1 AND poll_id=$2)`,
		req.OptionID, pollID).Scan(&validOption)
	if !validOption {
		writeErr(w, http.StatusBadRequest, "invalid option")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM message_poll_votes WHERE poll_id=$1 AND option_id=$2 AND user_id=$3`,
		pollID, req.OptionID, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "vote failed")
		return
	}
	if tag.RowsAffected() == 0 {
		if !multi {
			_, _ = a.db.Exec(r.Context(),
				`DELETE FROM message_poll_votes WHERE poll_id=$1 AND user_id=$2`, pollID, uid)
		}
		if _, err := a.db.Exec(r.Context(),
			`INSERT INTO message_poll_votes (poll_id, option_id, user_id) VALUES ($1,$2,$3)`,
			pollID, req.OptionID, uid); err != nil {
			writeErr(w, http.StatusInternalServerError, "vote failed")
			return
		}
	}
	a.chatPollFanout(r, pollID, convID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "voted"})
}

// GET /api/chat-polls/{id} — options with counts and the caller's votes.
func (a *App) handleGetChatPoll(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	pollID := r.PathValue("id")
	var question string
	var multi bool
	var convID string
	var closesAt *time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT p.question, p.multi, m.conversation_id, p.closes_at
                 FROM message_polls p JOIN messages m ON m.id = p.message_id
                 WHERE p.id=$1`, pollID).Scan(&question, &multi, &convID, &closesAt)
	if err != nil {
		writeErr(w, http.StatusNotFound, "poll not found")
		return
	}
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	writeJSON(w, http.StatusOK, a.chatPollState(r, pollID, uid, question, multi, closesAt))
}

func (a *App) chatPollState(r *http.Request, pollID, uid, question string, multi bool, closesAt *time.Time) map[string]any {
	rows, err := a.db.Query(r.Context(),
		`SELECT o.id, o.label, o.position,
                        (SELECT count(*) FROM message_poll_votes v WHERE v.option_id=o.id),
                        EXISTS(SELECT 1 FROM message_poll_votes v WHERE v.option_id=o.id AND v.user_id=$2)
                 FROM message_poll_options o WHERE o.poll_id=$1 ORDER BY o.position`, pollID, uid)
	if err != nil {
		return map[string]any{"error": "failed to load poll"}
	}
	defer rows.Close()
	type opt struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Votes int    `json:"votes"`
		Mine  bool   `json:"my_vote"`
	}
	options := []opt{}
	total := 0
	for rows.Next() {
		var o opt
		var pos int
		if err := rows.Scan(&o.ID, &o.Label, &pos, &o.Votes, &o.Mine); err == nil {
			options = append(options, o)
			total += o.Votes
		}
	}
	return map[string]any{
		"id": pollID, "question": question, "multi": multi,
		"closes_at": closesAt, "options": options, "total_votes": total,
	}
}

func (a *App) chatPollFanout(r *http.Request, pollID, convID string) {
	var question string
	var multi bool
	var closesAt *time.Time
	_ = a.db.QueryRow(r.Context(),
		`SELECT question, multi, closes_at FROM message_polls WHERE id=$1`,
		pollID).Scan(&question, &multi, &closesAt)
	state := a.chatPollState(r, pollID, "", question, multi, closesAt)
	state["type"] = "chat_poll_update"
	state["conversation_id"] = convID
	payload, _ := json.Marshal(state)
	a.fanoutConv(r.Context(), convID, payload)
}

// ---------- Video notes ----------

// POST /api/conversations/{id}/video-note — a short round video message.
// The clip is uploaded through the normal media pipeline first; this
// endpoint persists the message with kind='video_note'.
func (a *App) handleSendVideoNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaURL  string `json:"media_url"`
		DurationS int    `json:"duration_s"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.MediaURL) == "" || req.DurationS < 1 || req.DurationS > 60 {
		writeErr(w, http.StatusBadRequest, "media_url required, duration 1-60s")
		return
	}
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var msgID string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, media_url, kind)
                 VALUES ($1,$2,'',$3,'video_note') RETURNING id`, convID, uid, req.MediaURL).Scan(&msgID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to send video note")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "id": msgID, "conversation_id": convID,
		"sender_id": uid, "kind": "video_note", "media_url": req.MediaURL,
		"duration_s": req.DurationS, "created_at": time.Now(),
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusCreated, map[string]string{"id": msgID})
}

// ---------- Live location ----------

// PUT /api/conversations/{id}/live-location — start/update sharing for
// duration_minutes (max 8h). Members see updates over WS and via GET.
func (a *App) handleShareLiveLocation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Lat     float64 `json:"lat"`
		Lng     float64 `json:"lng"`
		Minutes int     `json:"duration_minutes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		writeErr(w, http.StatusBadRequest, "invalid coordinates")
		return
	}
	if req.Minutes < 1 || req.Minutes > 480 {
		writeErr(w, http.StatusBadRequest, "duration must be 1-480 minutes")
		return
	}
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO live_locations (user_id, conversation_id, lat, lng, expires_at, updated_at)
                 VALUES ($1,$2,$3,$4, now() + make_interval(mins => $5), now())
                 ON CONFLICT (user_id, conversation_id) DO UPDATE
                 SET lat=$3, lng=$4, expires_at=now() + make_interval(mins => $5), updated_at=now()`,
		uid, convID, req.Lat, req.Lng, req.Minutes)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to share location")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "live_location", "conversation_id": convID, "user_id": uid,
		"lat": req.Lat, "lng": req.Lng, "minutes": req.Minutes,
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusOK, map[string]string{"status": "sharing"})
}

func (a *App) handleStopLiveLocation(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM live_locations WHERE user_id=$1 AND conversation_id=$2`,
		userIDFrom(r), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (a *App) handleGetLiveLocations(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT l.user_id, u.username, l.lat, l.lng, l.expires_at
                 FROM live_locations l JOIN users u ON u.id = l.user_id
                 WHERE l.conversation_id=$1 AND l.expires_at > now()`, convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load locations")
		return
	}
	defer rows.Close()
	type loc struct {
		UserID    string    `json:"user_id"`
		Username  string    `json:"username"`
		Lat       float64   `json:"lat"`
		Lng       float64   `json:"lng"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	out := []loc{}
	for rows.Next() {
		var l loc
		if err := rows.Scan(&l.UserID, &l.Username, &l.Lat, &l.Lng, &l.ExpiresAt); err == nil {
			out = append(out, l)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"locations": out})
}

// ---------- Pay-in-chat ----------

// POST /api/conversations/{id}/pay — send crypto to another member; the
// ledger transfer executes atomically and the payment appears as a message.
func (a *App) handlePayInChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToUserID string `json:"to_user_id"`
		Asset    string `json:"asset"`
		Chain    string `json:"chain"`
		Amount   string `json:"amount"`
		Note     string `json:"note"`
	}
	if !decodeJSON(w, r, &req) || !a.assetSupported(r.Context(), req.Asset, req.Chain) {
		writeErr(w, http.StatusBadRequest, "unsupported asset/chain combination")
		return
	}
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) || !a.isMember(r.Context(), convID, req.ToUserID) {
		writeErr(w, http.StatusForbidden, "both parties must be conversation members")
		return
	}
	if req.ToUserID == uid {
		writeErr(w, http.StatusBadRequest, "cannot pay yourself")
		return
	}
	if len(req.Note) > 200 {
		writeErr(w, http.StatusBadRequest, "note too long")
		return
	}
	var kyc string
	_ = a.db.QueryRow(r.Context(), `SELECT kyc_status FROM users WHERE id=$1`, uid).Scan(&kyc)
	if kyc != "verified" {
		writeErr(w, http.StatusForbidden, "KYC verification required before sending payments")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "payment failed")
		return
	}
	defer tx.Rollback(r.Context())
	var senderAcct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset=$2 AND chain=$3 FOR UPDATE`,
		uid, strings.ToUpper(req.Asset), strings.ToLower(req.Chain)).Scan(&senderAcct)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "no wallet account for this asset; create one first")
		return
	}
	var ok bool
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
                 FROM ledger_entries WHERE account_id=$2`, req.Amount, senderAcct).Scan(&ok); err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "insufficient balance or invalid amount")
		return
	}
	var recipientAcct string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO wallet_accounts (user_id, asset, chain) VALUES ($1,$2,$3)
                 ON CONFLICT (user_id, asset, chain) DO UPDATE SET user_id=EXCLUDED.user_id
                 RETURNING id`, req.ToUserID, strings.ToUpper(req.Asset), strings.ToLower(req.Chain)).Scan(&recipientAcct)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "payment failed")
		return
	}
	var txID string
	if err := tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&txID); err != nil {
		writeErr(w, http.StatusInternalServerError, "payment failed")
		return
	}
	memo := "chat:" + convID + ":" + strings.TrimSpace(req.Note)
	if len(memo) > 200 {
		memo = memo[:200]
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (account_id, tx_id, kind, amount, memo) VALUES
                 ($1,$3,'p2p_send',(-1)*$4::numeric,$5), ($2,$3,'p2p_recv',$4::numeric,$5)`,
		senderAcct, recipientAcct, txID, req.Amount, memo); err != nil {
		writeErr(w, http.StatusInternalServerError, "payment failed")
		return
	}
	var msgID string
	body := strings.TrimSpace(req.Note)
	err = tx.QueryRow(r.Context(),
		`INSERT INTO messages (conversation_id, sender_id, body, kind, payment_id)
                 VALUES ($1,$2,$3,'payment',$4) RETURNING id`, convID, uid, body, txID).Scan(&msgID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "payment failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "payment failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "message", "id": msgID, "conversation_id": convID,
		"sender_id": uid, "kind": "payment", "body": body,
		"payment_id": txID, "asset": strings.ToUpper(req.Asset),
		"chain": strings.ToLower(req.Chain), "amount": req.Amount,
		"to_user_id": req.ToUserID, "created_at": time.Now(),
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusCreated, map[string]string{"id": msgID, "tx_id": txID})
}
