package main

// QoE and call-quality telemetry ingestion (§72/§73). Clients report video
// playback and call-quality events; the admin plane reads percentile
// summaries for the observability dashboards.

import "net/http"

type qoeInput struct {
	VideoID         string `json:"video_id"`
	StartupMS       *int   `json:"startup_ms"`
	BufferMS        *int   `json:"buffer_ms"`
	BufferingEvents *int   `json:"buffering_events"`
	PlaybackFailed  bool   `json:"playback_failed"`
	Resolution      string `json:"resolution"`
	BitrateKbps     *int   `json:"bitrate_kbps"`
	Completed       bool   `json:"completed"`
	WatchMS         *int   `json:"watch_ms"`
}

func clampInt(v *int, max int) *int {
	if v == nil {
		return nil
	}
	if *v < 0 {
		z := 0
		return &z
	}
	if *v > max {
		m := max
		return &m
	}
	return v
}

// POST /api/telemetry/qoe
func (a *App) handleQoEIngest(w http.ResponseWriter, r *http.Request) {
	var in qoeInput
	if !decodeJSON(w, r, &in) {
		return
	}
	_, err := a.db.Exec(r.Context(), `
INSERT INTO video_qoe_events
  (user_id, video_id, startup_ms, buffer_ms, buffering_events, playback_failed,
   resolution, bitrate_kbps, completed, watch_ms)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		userIDFrom(r), in.VideoID,
		clampInt(in.StartupMS, 600000), clampInt(in.BufferMS, 600000),
		clampInt(in.BufferingEvents, 100000), in.PlaybackFailed,
		in.Resolution, clampInt(in.BitrateKbps, 1000000), in.Completed,
		clampInt(in.WatchMS, 86_400_000))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "qoe ingest failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}

type callQualityInput struct {
	RoomID        string  `json:"room_id"`
	PacketLossPct *float64 `json:"packet_loss_pct"`
	JitterMS      *float64 `json:"jitter_ms"`
	RTTMS         *float64 `json:"rtt_ms"`
	BitrateKbps   *int     `json:"bitrate_kbps"`
	FrameRate     *int     `json:"frame_rate"`
	Resolution    string   `json:"resolution"`
}

func clampFloat(v *float64, max float64) *float64 {
	if v == nil {
		return nil
	}
	if *v < 0 {
		z := 0.0
		return &z
	}
	if *v > max {
		m := max
		return &m
	}
	return v
}

// POST /api/telemetry/call-quality
func (a *App) handleCallQualityIngest(w http.ResponseWriter, r *http.Request) {
	var in callQualityInput
	if !decodeJSON(w, r, &in) {
		return
	}
	_, err := a.db.Exec(r.Context(), `
INSERT INTO call_quality_events
  (user_id, room_id, packet_loss_pct, jitter_ms, rtt_ms, bitrate_kbps, frame_rate, resolution)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		userIDFrom(r), in.RoomID,
		clampFloat(in.PacketLossPct, 100), clampFloat(in.JitterMS, 10000),
		clampFloat(in.RTTMS, 600000), clampInt(in.BitrateKbps, 1000000),
		clampInt(in.FrameRate, 240), in.Resolution)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "call-quality ingest failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}

// GET /api/admin/qoe/summary — percentile video-QoE metrics over a window.
func (a *App) handleAdminQoESummary(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
SELECT to_char(date_trunc('hour', created_at), 'YYYY-MM-DD HH24:MI') AS bucket,
       count(*)                                                      AS events,
       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY startup_ms)) AS p50_startup_ms,
       round(percentile_cont(0.95) WITHIN GROUP (ORDER BY startup_ms)) AS p95_startup_ms,
       round(percentile_cont(0.5) WITHIN GROUP (ORDER BY buffer_ms))  AS p50_buffer_ms,
       round(100.0 * count(*) FILTER (WHERE playback_failed) / GREATEST(count(*), 1), 2) AS failure_pct,
       round(100.0 * count(*) FILTER (WHERE completed) / GREATEST(count(*), 1), 2)       AS completion_pct,
       round(avg(bitrate_kbps))                                       AS avg_bitrate_kbps
FROM video_qoe_events
WHERE created_at > now() - interval '24 hours'
GROUP BY 1 ORDER BY 1`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "qoe summary failed")
		return
	}
	defer rows.Close()
	type qoeRow struct {
		Bucket        string   `json:"bucket"`
		Events        int64    `json:"events"`
		P50StartupMS  *int64   `json:"p50_startup_ms"`
		P95StartupMS  *int64   `json:"p95_startup_ms"`
		P50BufferMS   *int64   `json:"p50_buffer_ms"`
		FailurePct    *float64 `json:"failure_pct"`
		CompletionPct *float64 `json:"completion_pct"`
		AvgBitrate    *int64   `json:"avg_bitrate_kbps"`
	}
	out := []qoeRow{}
	for rows.Next() {
		var b qoeRow
		if err := rows.Scan(&b.Bucket, &b.Events, &b.P50StartupMS, &b.P95StartupMS,
			&b.P50BufferMS, &b.FailurePct, &b.CompletionPct, &b.AvgBitrate); err != nil {
			writeErr(w, http.StatusInternalServerError, "qoe scan failed")
			return
		}
		out = append(out, b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": out})
}

// GET /api/admin/call-quality/summary — percentile call metrics over a window.
func (a *App) handleAdminCallQualitySummary(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
SELECT to_char(date_trunc('hour', created_at), 'YYYY-MM-DD HH24:MI') AS bucket,
       count(*)                                                       AS calls,
       round(percentile_cont(0.5)  WITHIN GROUP (ORDER BY packet_loss_pct)::numeric, 3) AS p50_packet_loss_pct,
       round(percentile_cont(0.95) WITHIN GROUP (ORDER BY packet_loss_pct)::numeric, 3) AS p95_packet_loss_pct,
       round(percentile_cont(0.5)  WITHIN GROUP (ORDER BY jitter_ms))  AS p50_jitter_ms,
       round(percentile_cont(0.5)  WITHIN GROUP (ORDER BY rtt_ms))     AS p50_rtt_ms,
       round(avg(bitrate_kbps))                                        AS avg_bitrate_kbps,
       round(percentile_cont(0.5)  WITHIN GROUP (ORDER BY frame_rate)) AS p50_frame_rate
FROM call_quality_events
WHERE created_at > now() - interval '24 hours'
GROUP BY 1 ORDER BY 1`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "call-quality summary failed")
		return
	}
	defer rows.Close()
	type cqRow struct {
		Bucket          string   `json:"bucket"`
		Calls           int64    `json:"calls"`
		P50Loss         *float64 `json:"p50_packet_loss_pct"`
		P95Loss         *float64 `json:"p95_packet_loss_pct"`
		P50Jitter       *float64 `json:"p50_jitter_ms"`
		P50RTT          *float64 `json:"p50_rtt_ms"`
		AvgBitrate      *int64   `json:"avg_bitrate_kbps"`
		P50FrameRate    *int64   `json:"p50_frame_rate"`
	}
	out := []cqRow{}
	for rows.Next() {
		var c cqRow
		if err := rows.Scan(&c.Bucket, &c.Calls, &c.P50Loss, &c.P95Loss, &c.P50Jitter,
			&c.P50RTT, &c.AvgBitrate, &c.P50FrameRate); err != nil {
			writeErr(w, http.StatusInternalServerError, "call-quality scan failed")
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"rows": out})
}
