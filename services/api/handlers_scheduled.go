package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ---- Scheduled messages (Telegram-style "send later") ----

// POST /api/conversations/{id}/schedule — {body, media_url?, send_at}
func (a *App) handleScheduleMessage(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var req struct {
		Body     string    `json:"body"`
		MediaURL string    `json:"media_url"`
		SendAt   time.Time `json:"send_at"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Body = capRunes(strings.TrimSpace(req.Body), 5000)
	if req.Body == "" && req.MediaURL == "" {
		writeErr(w, http.StatusBadRequest, "body or media required")
		return
	}
	if req.SendAt.Before(time.Now().Add(5*time.Second)) || req.SendAt.After(time.Now().Add(365*24*time.Hour)) {
		writeErr(w, http.StatusBadRequest, "send_at must be between 5s and 1y from now")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO scheduled_messages (conversation_id, sender_id, body, media_url, send_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		convID, uid, req.Body, req.MediaURL, req.SendAt).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "schedule failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "send_at": req.SendAt})
}

// GET /api/conversations/{id}/scheduled — my pending scheduled messages.
func (a *App) handleListScheduled(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	convID := r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT id, body, media_url, send_at FROM scheduled_messages
		 WHERE conversation_id=$1 AND sender_id=$2 AND sent_at IS NULL
		 ORDER BY send_at ASC LIMIT 100`, convID, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load")
		return
	}
	defer rows.Close()
	type sched struct {
		ID       string    `json:"id"`
		Body     string    `json:"body"`
		MediaURL string    `json:"media_url"`
		SendAt   time.Time `json:"send_at"`
	}
	out := []sched{}
	for rows.Next() {
		var s sched
		if err := rows.Scan(&s.ID, &s.Body, &s.MediaURL, &s.SendAt); err == nil {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"scheduled": out})
}

// DELETE /api/scheduled/{id} — cancel a pending scheduled message.
func (a *App) handleCancelScheduled(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM scheduled_messages WHERE id=$1 AND sender_id=$2 AND sent_at IS NULL`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "scheduled message not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// startScheduler delivers due scheduled messages every 2 seconds.
func (a *App) startScheduler() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.deliverScheduled(context.Background())
			a.deliverEventReminders(context.Background())
			a.requeueStaleTranscodes()
		}
	}()
}

func (a *App) deliverScheduled(ctx context.Context) {
	// Claim due rows atomically; SKIP LOCKED keeps multi-node deployments safe.
	rows, err := a.db.Query(ctx,
		`UPDATE scheduled_messages SET sent_at = now()
		 WHERE id IN (
		   SELECT id FROM scheduled_messages
		   WHERE sent_at IS NULL AND send_at <= now()
		   ORDER BY send_at LIMIT 50 FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, conversation_id, sender_id, body, media_url`)
	if err != nil {
		return
	}
	defer rows.Close()
	type due struct {
		id, convID, senderID, body, mediaURL string
	}
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.convID, &d.senderID, &d.body, &d.mediaURL); err == nil {
			dues = append(dues, d)
		}
	}
	rows.Close()
	for _, d := range dues {
		var msgID string
		var createdAt time.Time
		if err := a.db.QueryRow(ctx,
			`INSERT INTO messages (conversation_id, sender_id, body, media_url)
			 VALUES ($1,$2,$3,$4) RETURNING id, created_at`,
			d.convID, d.senderID, d.body, d.mediaURL).Scan(&msgID, &createdAt); err != nil {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"type": "message", "id": msgID, "conversation_id": d.convID,
			"sender_id": d.senderID, "body": d.body, "media_url": d.mediaURL,
			"created_at": createdAt, "scheduled": true,
		})
		a.fanoutToMembers(ctx, d.convID, payload, "")
	}
}
