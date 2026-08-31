package main

// Watch-time signal ingestion and the For-You-Page feed. Signals flow:
// client -> POST /api/reels/{id}/watch -> reel_watch_events -> the ML
// service blends completion/rewatch/not-interested rates into ranking.
// The FYP query itself computes per-reel aggregates inline so results stay
// correct even while the ML service is catching up.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"time"
)

func (a *App) handleReelWatch(w http.ResponseWriter, r *http.Request) {
	postID := r.PathValue("id")
	var req struct {
		WatchedMs     int  `json:"watched_ms"`
		DurationMs    int  `json:"duration_ms"`
		Completed     bool `json:"completed"`
		Rewatched     bool `json:"rewatched"`
		NotInterested bool `json:"not_interested"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.WatchedMs < 0 || req.DurationMs < 0 || req.WatchedMs > 24*3600*1000 || req.DurationMs > 24*3600*1000 {
		writeErr(w, http.StatusBadRequest, "invalid watch timings")
		return
	}
	// Only reels produce watch signals.
	var isReel bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND type='reel' AND deleted_at IS NULL)`,
		postID).Scan(&isReel); err != nil || !isReel {
		writeErr(w, http.StatusNotFound, "reel not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO reel_watch_events (user_id, post_id, watched_ms, duration_ms, completed, rewatched, not_interested)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		userIDFrom(r), postID, req.WatchedMs, req.DurationMs, req.Completed, req.Rewatched, req.NotInterested); err != nil {
		writeErr(w, http.StatusInternalServerError, "signal failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "recorded"})
}

// handleFYP is the For-You-Page: reels ranked by a blend of engagement and
// watch-quality signals, excluding the viewer's own content, muted authors,
// not-interested reels and word-filtered bodies.

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (a *App) handleFYP(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	limit, offset := pageParams(r)
	cacheKey := fmt.Sprintf("fyp:%s:%d:%d", uid, limit, offset)
	if cached, ok := a.cache.get(r.Context(), cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "hit")
		_, _ = w.Write(cached)
		return
	}
	rows, err := a.db.Query(r.Context(),
		`WITH agg AS (
		   SELECT post_id,
		          AVG(LEAST(watched_ms::float8 / NULLIF(duration_ms,0), 1.0)) AS avg_watch_pct,
		          AVG(completed::int::float8) AS completion_rate,
		          AVG(rewatched::int::float8) AS rewatch_rate,
		          AVG(not_interested::int::float8) AS not_interested_rate,
		          COUNT(*) AS signals
		   FROM reel_watch_events GROUP BY post_id
		 )
		 SELECT p.id, p.author_id, u.display_name, u.username, u.avatar_url,
		        p.body, p.like_count, p.comment_count, p.view_count, p.created_at,
		        COALESCE(a.signals,0), COALESCE(a.avg_watch_pct,0), COALESCE(a.completion_rate,0), COALESCE(a.rewatch_rate,0),
                     (SELECT pm.url FROM post_media pm WHERE pm.post_id=p.id ORDER BY pm.position LIMIT 1) AS media_url
		 FROM posts p
		 JOIN users u ON u.id = p.author_id
		 LEFT JOIN agg a ON a.post_id = p.id
		 WHERE p.type='reel' AND p.deleted_at IS NULL AND p.visibility='public'
		   AND p.author_id <> $1
		   AND NOT EXISTS(SELECT 1 FROM user_mutes m WHERE m.user_id=$1 AND m.muted_id=p.author_id)
		   AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE (b.blocker_id=$1 AND b.blocked_id=p.author_id)
		                                               OR (b.blocker_id=p.author_id AND b.blocked_id=$1))
		   AND NOT EXISTS(SELECT 1 FROM reel_watch_events w WHERE w.user_id=$1 AND w.post_id=p.id AND w.not_interested)
		   AND NOT EXISTS(SELECT 1 FROM word_filters f WHERE f.user_id=$1 AND p.body ILIKE '%'||f.phrase||'%')
		 ORDER BY (
		   (p.like_count*3 + p.comment_count*5 + p.share_count*4 + LEAST(p.view_count,100000))
		   * (0.5 + COALESCE(a.avg_watch_pct,0.3))
		   * (1.0 + COALESCE(a.completion_rate,0))
		   * (1.0 + COALESCE(a.rewatch_rate,0)*2)
		   / (1.0 + COALESCE(a.not_interested_rate,0)*4)
		   / (1.0 + EXTRACT(EPOCH FROM (now()-p.created_at))/86400.0)
		 ) DESC
		 LIMIT $2 OFFSET $3`, uid, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "feed failed")
		return
	}
	defer rows.Close()
	posts := []map[string]any{}
	for rows.Next() {
		var id, authorID, name, username, avatar, body string
		var mediaURL *string
		var likes, comments int
		var views, signals int64
		var watchPct, completion, rewatch float64
		var createdAt time.Time
		if err := rows.Scan(&id, &authorID, &name, &username, &avatar, &body,
			&likes, &comments, &views, &createdAt, &signals, &watchPct, &completion, &rewatch, &mediaURL); err != nil {
			continue
		}
		posts = append(posts, map[string]any{
			"id": id, "author_id": authorID, "display_name": name, "username": username,
			"avatar_url": avatar, "body": body, "like_count": likes, "comment_count": comments,
			"view_count": views, "created_at": createdAt,
			"watch_pct": math.Round(watchPct*100) / 100, "completion_rate": math.Round(completion*100) / 100,
			"rewatch_rate": math.Round(rewatch*100) / 100, "media_url": deref(mediaURL),
		})
	}
	a.mlRerankFYP(uid, posts)
	body, err := json.Marshal(map[string]any{"posts": posts})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "feed failed")
		return
	}
	a.cache.set(r.Context(), cacheKey, body, 15*time.Second)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// mlRerankFYP re-orders FYP candidates using the ML service's watch-signal
// scorer (services/ml /rank/watch). On any transport or shape error the
// SQL-scored order is kept — the feed degrades to the local scorer, never to
// no feed.
type mlFeature struct {
	PostID        string  `json:"post_id"`
	AuthorID      string  `json:"author_id"`
	CreatedAt     float64 `json:"created_at"`
	LikeCount     int     `json:"like_count"`
	CommentCount  int     `json:"comment_count"`
	ViewCount     int64   `json:"view_count"`
	AvgCompletion float64 `json:"avg_completion"`
	RewatchRate   float64 `json:"rewatch_rate"`
}

func (a *App) mlRerankFYP(uid string, posts []map[string]any) {
	if a.cfg.MLServiceURL == "" || len(posts) < 2 {
		return
	}
	type rankReq struct {
		UserID string      `json:"user_id"`
		Posts  []mlFeature `json:"posts"`
	}
	req := rankReq{UserID: uid}
	for _, post := range posts {
		f := mlFeature{PostID: toStr(post["id"]), AuthorID: toStr(post["author_id"])}
		if t, ok := post["created_at"].(time.Time); ok {
			f.CreatedAt = float64(t.Unix())
		}
		f.LikeCount, _ = post["like_count"].(int)
		f.CommentCount, _ = post["comment_count"].(int)
		f.ViewCount, _ = post["view_count"].(int64)
		f.AvgCompletion, _ = post["completion_rate"].(float64)
		f.RewatchRate, _ = post["rewatch_rate"].(float64)
		req.Posts = append(req.Posts, f)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.MLServiceURL+"/rank/watch", bytes.NewReader(body))
	if err != nil {
		return
	}
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var out struct {
		Ranking []struct {
			PostID string `json:"post_id"`
		} `json:"ranking"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || len(out.Ranking) == 0 {
		return
	}
	order := make(map[string]int, len(out.Ranking))
	for i, entry := range out.Ranking {
		order[entry.PostID] = i
	}
	sort.Slice(posts, func(i, j int) bool {
		oi, okI := order[toStr(posts[i]["id"])]
		oj, okJ := order[toStr(posts[j]["id"])]
		if okI && okJ {
			return oi < oj
		}
		return false
	})
}

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ---- transcode job endpoints (C++ worker claims these via SKIP LOCKED) ----

func (a *App) handleRequestTranscode(w http.ResponseWriter, r *http.Request) {
	mediaID := r.PathValue("id")
	var req struct {
		SourceURL string `json:"source_url"`
		Kind      string `json:"kind"` // video | audio
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Kind != "video" && req.Kind != "audio" {
		writeErr(w, http.StatusBadRequest, "kind must be video or audio")
		return
	}
	if req.SourceURL == "" || len(req.SourceURL) > 2048 {
		writeErr(w, http.StatusBadRequest, "valid source_url required")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO transcode_jobs (media_id, source_url, kind)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (id) DO NOTHING
		 RETURNING id`,
		mediaID, req.SourceURL, req.Kind).Scan(&id)
	if err != nil {
		// A job for this media may already exist; look it up.
		if err2 := a.db.QueryRow(r.Context(),
			`SELECT id FROM transcode_jobs WHERE media_id=$1 ORDER BY created_at DESC LIMIT 1`,
			mediaID).Scan(&id); err2 != nil {
			writeErr(w, http.StatusInternalServerError, "enqueue failed")
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]string{"job_id": id, "status": "queued"})
}

func (a *App) handleTranscodeStatus(w http.ResponseWriter, r *http.Request) {
	var status, ladder, thumb string
	var durationMs, attempts int
	var errMsg string
	err := a.db.QueryRow(r.Context(),
		`SELECT status, ladder::text, thumb_url, duration_ms, attempts, error
		 FROM transcode_jobs WHERE media_id=$1 ORDER BY created_at DESC LIMIT 1`,
		r.PathValue("id")).Scan(&status, &ladder, &thumb, &durationMs, &attempts, &errMsg)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no transcode job for this media")
		return
	}
	var ladderJSON any
	_ = json.Unmarshal([]byte(ladder), &ladderJSON)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": status, "ladder": ladderJSON, "thumb_url": thumb,
		"duration_ms": durationMs, "attempts": attempts, "error": errMsg,
	})
}
