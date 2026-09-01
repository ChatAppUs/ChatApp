package main

// Optional C++ counters engine client (services/counters). When COUNTERS_URL
// is set the write-hot counter paths — hashtag trends, post view counts and
// live-room viewer counts — route through the in-memory engine instead of
// per-event SQL UPDATEs; the engine flushes aggregated deltas back through
// /internal/counters/flush. Every call fails open with a short timeout so a
// counters outage falls back to the SQL path (same pattern as cache.go).

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type countersClient struct {
	url    string
	secret string
	http   *http.Client
}

type flushPayload struct {
	Hashtags []struct {
		Tag   string `json:"tag"`
		Delta int64  `json:"delta"`
	} `json:"hashtags"`
	Views []struct {
		ID    string `json:"id"`
		Delta int64  `json:"delta"`
	} `json:"views"`
	Peaks []struct {
		Room    string `json:"room"`
		Peak    int64  `json:"peak"`
		Viewers int64  `json:"viewers"`
	} `json:"peaks"`
}

func newCounters(url, secret string) *countersClient {
	if url == "" {
		return nil
	}
	return &countersClient{url: url, secret: secret,
		http: &http.Client{Timeout: 300 * time.Millisecond}}
}

func (c *countersClient) do(ctx context.Context, method, path string, body any) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, false
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, rd)
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false
	}
	return raw, true
}

// incr forwards a single counter event (hashtag use or post view).
func (c *countersClient) incr(ctx context.Context, kind, key string, delta int64) bool {
	_, ok := c.do(ctx, http.MethodPost, "/incr", map[string]any{
		"kind": kind, "key": key, "delta": delta})
	return ok
}

// viewer forwards a live-room join/leave event. ok=false means unreachable.
func (c *countersClient) viewer(ctx context.Context, room, op string) bool {
	_, ok := c.do(ctx, http.MethodPost, "/viewer", map[string]string{
		"room": room, "op": op})
	return ok
}

// viewers returns (viewers, peak) for a room from the engine.
func (c *countersClient) viewers(ctx context.Context, room string) (int64, int64, bool) {
	raw, ok := c.do(ctx, http.MethodGet, "/viewers?room="+room, nil)
	if !ok {
		return 0, 0, false
	}
	var out struct {
		Viewers int64 `json:"viewers"`
		Peak    int64 `json:"peak"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return 0, 0, false
	}
	return out.Viewers, out.Peak, true
}

// trendingTag is one trending row (same JSON shape as the SQL fallback).
type trendingTag struct {
	Tag   string `json:"tag"`
	Count int64  `json:"count"`
}

// topHashtags returns the trending list from the engine's 24h window.
func (c *countersClient) topHashtags(ctx context.Context, limit int) ([]trendingTag, bool) {
	raw, ok := c.do(ctx, http.MethodGet, "/top/hashtags", nil)
	if !ok {
		return nil, false
	}
	var out struct {
		Trending []trendingTag `json:"trending"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return nil, false
	}
	if limit > 0 && len(out.Trending) > limit {
		out.Trending = out.Trending[:limit]
	}
	return out.Trending, true
}

// isUUIDShape reports whether s looks like a canonical UUID. The engine is
// input-agnostic; flush drops malformed ids instead of poisoning the batch.
func isUUIDShape(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// handleCountersFlush applies engine deltas to Postgres in one transaction.
// Malformed rows are skipped (the engine has no insight into uuid shape) so
// a single bad key can never poison the flush; everything else commits
// atomically.
func (a *App) handleCountersFlush(w http.ResponseWriter, r *http.Request) {
	var p flushPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid payload")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "flush failed")
		return
	}
	defer tx.Rollback(r.Context())
	for _, h := range p.Hashtags {
		if h.Delta == 0 || h.Tag == "" {
			continue
		}
		if _, err = tx.Exec(r.Context(),
			`INSERT INTO hashtags (tag, use_count, last_used) VALUES ($1,$2,now())
             ON CONFLICT (tag) DO UPDATE SET use_count = hashtags.use_count + $2,
             last_used = CASE WHEN now() > hashtags.last_used THEN now() ELSE hashtags.last_used END`,
			h.Tag, h.Delta); err != nil {
			writeErr(w, http.StatusInternalServerError, "flush failed")
			return
		}
	}
	for _, v := range p.Views {
		if v.Delta == 0 || !isUUIDShape(v.ID) {
			continue
		}
		if _, err = tx.Exec(r.Context(),
			`UPDATE posts SET view_count = view_count + $2 WHERE id = $1`,
			v.ID, v.Delta); err != nil {
			writeErr(w, http.StatusInternalServerError, "flush failed")
			return
		}
	}
	for _, pk := range p.Peaks {
		if !isUUIDShape(pk.Room) {
			continue
		}
		if _, err = tx.Exec(r.Context(),
			`UPDATE live_rooms SET peak_viewers = GREATEST(peak_viewers, $2) WHERE id = $1`,
			pk.Room, pk.Peak); err != nil {
			writeErr(w, http.StatusInternalServerError, "flush failed")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "flush failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "flushed"})
}
