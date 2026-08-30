package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Calls pack: screen-share state broadcast (the media track itself is a
// client-side getDisplayMedia WebRTC track through the SFU; this endpoint
// announces state to the room so every client renders the layout) and call
// recordings uploaded by the recording participant through the media
// pipeline.

func roomConvID(roomID string) (string, bool) {
	parts := strings.Split(roomID, "-")
	if len(parts) < 2 {
		return "", false
	}
	return strings.Join(parts[:len(parts)-1], "-"), true
}

// POST /api/calls/rooms/{roomId}/screenshare {on: true|false}
func (a *App) handleScreenShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	roomID := r.PathValue("roomId")
	convID, ok := roomConvID(roomID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "screenshare", "room_id": roomID,
		"conversation_id": convID, "user_id": uid, "on": req.On,
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusOK, map[string]string{"status": "broadcast"})
}

// POST /api/calls/rooms/{roomId}/recordings — register a recording the caller
// captured (MediaRecorder on web/desktop, ReplayKit/MediaProjection on
// mobile) and uploaded through the signed media pipeline.
func (a *App) handleSaveRecording(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaURL  string `json:"media_url"`
		DurationS int    `json:"duration_s"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.MediaURL) == "" {
		writeErr(w, http.StatusBadRequest, "media_url required")
		return
	}
	uid := userIDFrom(r)
	roomID := r.PathValue("roomId")
	convID, ok := roomConvID(roomID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO call_recordings (room_id, owner_id, media_url, duration_s)
                 VALUES ($1,$2,$3,$4) RETURNING id`, roomID, uid, req.MediaURL, req.DurationS).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save recording")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "call_recording", "room_id": roomID,
		"conversation_id": convID, "owner_id": uid, "recording_id": id,
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/calls/rooms/{roomId}/recordings — recordings any member may replay.
func (a *App) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	roomID := r.PathValue("roomId")
	convID, ok := roomConvID(roomID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id, u.username, c.media_url, c.duration_s, c.created_at
                 FROM call_recordings c JOIN users u ON u.id = c.owner_id
                 WHERE c.room_id=$1 ORDER BY c.created_at DESC`, roomID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load recordings")
		return
	}
	defer rows.Close()
	type rec struct {
		ID        string    `json:"id"`
		Owner     string    `json:"owner"`
		MediaURL  string    `json:"media_url"`
		DurationS int       `json:"duration_s"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []rec{}
	for rows.Next() {
		var rc rec
		if err := rows.Scan(&rc.ID, &rc.Owner, &rc.MediaURL, &rc.DurationS, &rc.CreatedAt); err == nil {
			out = append(out, rc)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recordings": out})
}

// DELETE /api/calls/recordings/{id} — only the recording owner may delete.
func (a *App) handleDeleteRecording(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM call_recordings WHERE id=$1 AND owner_id=$2`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "recording not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
