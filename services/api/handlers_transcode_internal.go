package main

// Internal control plane for the C++ transcode worker. The worker polls for
// jobs (claimed FOR UPDATE SKIP LOCKED), runs ffmpeg, writes renditions to
// the media volume, and reports the ladder back here. Endpoints are guarded
// by the shared CLUSTER_SECRET bearer token (fail-closed when unset).

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func (a *App) requireInternal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.ClusterSecret == "" {
			writeErr(w, http.StatusForbidden, "internal endpoints disabled")
			return
		}
		h := r.Header.Get("Authorization")
		if h != "Bearer "+a.cfg.ClusterSecret {
			writeErr(w, http.StatusUnauthorized, "invalid internal token")
			return
		}
		next(w, r)
	}
}

// handleTranscodeClaim atomically claims the oldest queued job.
func (a *App) handleTranscodeClaim(w http.ResponseWriter, r *http.Request) {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "claim failed")
		return
	}
	defer tx.Rollback(r.Context())
	var id, mediaID, sourceURL, kind string
	var params []byte
	err = tx.QueryRow(r.Context(),
		`UPDATE transcode_jobs SET status='running', claimed_at=now(), attempts=attempts+1
		 WHERE id = (
		   SELECT id FROM transcode_jobs
		   WHERE status='queued' AND attempts < 5
		   ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED
		 )
		 RETURNING id, media_id, source_url, kind, COALESCE(params,'{}'::jsonb)`).Scan(&id, &mediaID, &sourceURL, &kind, &params)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"job": nil})
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "claim failed")
		return
	}
	if kind == "live" {
		// The worker is now the armed RTMP endpoint for this stream key.
		_, _ = a.db.Exec(r.Context(),
			`UPDATE live_streams SET status='live'
			 WHERE stream_key = (SELECT params->>'stream_key' FROM transcode_jobs WHERE id=$1)
			   AND status='pending'`, id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": map[string]any{
		"id": id, "media_id": mediaID, "source_url": sourceURL, "kind": kind,
		"params": json.RawMessage(params),
	}})
}

// handleTranscodeComplete records the produced ladder (or the failure).
func (a *App) handleTranscodeComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JobID      string          `json:"job_id"`
		MediaID    string          `json:"media_id"`
		Status     string          `json:"status"` // done | failed
		Ladder     json.RawMessage `json:"ladder"`
		ThumbURL   string          `json:"thumb_url"`
		DurationMs int             `json:"duration_ms"`
		Error      string          `json:"error"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Status != "done" && req.Status != "failed" {
		writeErr(w, http.StatusBadRequest, "status must be done or failed")
		return
	}
	if req.Ladder == nil {
		req.Ladder = json.RawMessage("[]")
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE transcode_jobs SET status=$2, ladder=$3, thumb_url=$4, duration_ms=$5, error=$6,
		 finished_at=now() WHERE id=$1 AND status='running'`,
		req.JobID, req.Status, req.Ladder, req.ThumbURL, req.DurationMs, req.Error)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "job not running")
		return
	}
	// Attach the produced HLS master playlist + thumbnail to the media row so
	// reel/story players can switch to adaptive playback.
	if req.Status == "done" {
		var master string
		var ladder []map[string]any
		if json.Unmarshal(req.Ladder, &ladder) == nil {
			for _, r := range ladder {
				if r["master"] == true {
					master, _ = r["url"].(string)
				}
			}
		}
		if (master != "" || req.ThumbURL != "") && req.MediaID != "" {
			_, _ = a.db.Exec(r.Context(),
				`UPDATE post_media SET url = CASE WHEN $2 <> '' THEN $2 ELSE url END,
				 thumb_url = CASE WHEN $3 <> '' THEN $3 ELSE thumb_url END
				 WHERE url LIKE '%'||$1||'%'`, req.MediaID, master, req.ThumbURL)
		}
		// Live ingest jobs end the stream when the publisher disconnects
		// (a live job's media_id is the live room id).
		_, _ = a.db.Exec(r.Context(),
			`UPDATE live_streams SET status='ended', ended_at=now()
			 WHERE room_id=$1 AND status='live'`, req.MediaID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// requeueStaleTranscodes returns running jobs whose worker died (claimed
// long ago, never finished) back to the queue. Called by the scheduler tick.
func (a *App) requeueStaleTranscodes() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = a.db.Exec(ctx,
		`UPDATE transcode_jobs SET status='queued', claimed_at=NULL
		 WHERE status='running' AND claimed_at < now() - interval '10 minutes'`)
}
