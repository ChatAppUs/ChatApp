package main

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ChatApp Pulse — master plan §32 (X/Twitter-class public conversation). Short
// posts, threads (parent_id), quotes (quote_of), reposts (repost_of), topics,
// lists, and trends computed from real post volume.
// Trending and breaking surfaces are derived from pulse_trends, which the
// sweepPulseTrends job (registered in main.go) refreshes on a timer.

// pulseHashtagRe extracts #topics from a post body. Topic derivation is
// authoritative on the server so trends and topic feeds work for every client
// (including any native client that does not parse hashtags itself).
var pulseHashtagRe = regexp.MustCompile(`#([a-zA-Z0-9_]{1,64})`)

func topicsFromBody(body string) []string {
	out := []string{}
	for _, m := range pulseHashtagRe.FindAllStringSubmatch(body, 20) {
		out = append(out, m[1])
	}
	return out
}

// validUUID reports whether s is a well-formed UUID, so malformed path
// parameters are rejected with 400 instead of surfacing a Postgres cast error.
func validUUID(s string) bool {
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
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// requireUUIDPath validates a path parameter, writing a 400 when malformed.
func requireUUIDPath(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	v := r.PathValue(name)
	if !validUUID(v) {
		writeErr(w, http.StatusBadRequest, name+" must be a UUID")
		return "", false
	}
	return v, true
}

type pulsePostJSON struct {
	ID         string    `json:"id"`
	AuthorID   string    `json:"author_id"`
	Author     string    `json:"author"`
	Body       string    `json:"body"`
	ParentID   *string   `json:"parent_id,omitempty"`
	QuoteOf    *string   `json:"quote_of,omitempty"`
	RepostOf   *string   `json:"repost_of,omitempty"`
	Topics     []string  `json:"topics"`
	LocalTag   string    `json:"local_tag,omitempty"`
	ReplyCount int       `json:"reply_count"`
	RepostCount int      `json:"repost_count"`
	QuoteCount int       `json:"quote_count"`
	CreatedAt  time.Time `json:"created_at"`
}

const pulseSelect = `
 SELECT p.id, p.author_id, COALESCE(u.username,''), p.body, p.parent_id,
        p.quote_of, p.repost_of, p.topics, p.local_tag, p.created_at,
        (SELECT COUNT(*) FROM pulse_posts x WHERE x.parent_id = p.id),
        (SELECT COUNT(*) FROM pulse_posts x WHERE x.repost_of = p.id),
        (SELECT COUNT(*) FROM pulse_posts x WHERE x.quote_of = p.id)
   FROM pulse_posts p LEFT JOIN users u ON u.id = p.author_id `

func scanPulsePosts(rows interface {
	Next() bool
	Scan(...any) error
	Close()
}) []pulsePostJSON {
	out := []pulsePostJSON{}
	for rows.Next() {
		var p pulsePostJSON
		if err := rows.Scan(&p.ID, &p.AuthorID, &p.Author, &p.Body, &p.ParentID,
			&p.QuoteOf, &p.RepostOf, &p.Topics, &p.LocalTag, &p.CreatedAt,
			&p.ReplyCount, &p.RepostCount, &p.QuoteCount); err == nil {
			out = append(out, p)
		}
	}
	rows.Close()
	return out
}

func normalizeTopics(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "#")))
		if t == "" || len(t) > 64 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

// POST /api/pulse/posts — create a short post, reply, quote or repost.
func (a *App) handlePulseCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body     string   `json:"body"`
		ParentID string   `json:"parent_id"`
		QuoteOf  string   `json:"quote_of"`
		RepostOf string   `json:"repost_of"`
		Topics   []string `json:"topics"`
		LocalTag string   `json:"local_tag"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	if len(body) > 5000 {
		writeErr(w, http.StatusBadRequest, "body too long (max 5000 chars)")
		return
	}
	// A repost with no body is valid; every other post needs text.
	if body == "" && strings.TrimSpace(req.RepostOf) == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	if len(req.LocalTag) > 64 {
		writeErr(w, http.StatusBadRequest, "local_tag too long")
		return
	}
	uid := userIDFrom(r)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "post failed")
		return
	}
	defer tx.Rollback(r.Context())

	ref := func(kind, id string) (*string, bool) {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, true
		}
		var exists bool
		if err := tx.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM pulse_posts WHERE id=$1)`, id).Scan(&exists); err != nil || !exists {
			writeErr(w, http.StatusBadRequest, kind+" post not found")
			return nil, false
		}
		return &id, true
	}
	parent, ok := ref("parent", req.ParentID)
	if !ok {
		return
	}
	quote, ok := ref("quoted", req.QuoteOf)
	if !ok {
		return
	}
	repost, ok := ref("reposted", req.RepostOf)
	if !ok {
		return
	}

	// Server-side hashtag extraction is authoritative and merged with any
	// explicitly supplied topics.
	topics := normalizeTopics(append(topicsFromBody(body), req.Topics...))
	var id string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO pulse_posts (author_id, body, parent_id, quote_of, repost_of, topics, local_tag)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		uid, body, parent, quote, repost, topics,
		strings.ToLower(strings.TrimSpace(req.LocalTag))).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "post failed")
		return
	}
	// Register topics and notify a replied-to author. These run inside the same
	// transaction, so a failure must abort instead of being discarded — a
	// swallowed error leaves the transaction aborted and the Commit would fail
	// with a misleading error.
	for _, t := range topics {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO pulse_topics (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, t); err != nil {
			writeErr(w, http.StatusInternalServerError, "post failed")
			return
		}
	}
	var parentAuthor string
	if parent != nil {
		_ = tx.QueryRow(r.Context(),
			`SELECT author_id FROM pulse_posts WHERE id=$1 AND author_id <> $2`,
			*parent, uid).Scan(&parentAuthor)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "post failed")
		return
	}
	if parentAuthor != "" {
		a.notifyKind(parentAuthor, "pulse_reply", map[string]any{"post_id": id})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// GET /api/pulse/posts — the global or local feed, newest first.
func (a *App) handlePulseFeed(w http.ResponseWriter, r *http.Request) {
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	region := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("region")))
	q := pulseSelect
	args := []any{}
	// Only top-level posts appear in the main feed.
	if scope == "local" {
		if region == "" {
			writeErr(w, http.StatusBadRequest, "region required for the local feed")
			return
		}
		q += ` WHERE p.local_tag = $1 AND p.parent_id IS NULL`
		args = append(args, region)
	} else if topic := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("topic"))); topic != "" {
		q += ` WHERE $1 = ANY(p.topics) AND p.parent_id IS NULL`
		args = append(args, topic)
	} else {
		q += ` WHERE p.parent_id IS NULL`
	}
	q += ` ORDER BY p.created_at DESC LIMIT 100`
	rows, err := a.db.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load feed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": scanPulsePosts(rows)})
}

// GET /api/pulse/posts/{id}/thread — the full thread beneath a root post.
func (a *App) handlePulseThread(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUUIDPath(w, r, "id")
	if !ok {
		return
	}
	// Walk the reply tree with a recursive CTE so nested replies are returned.
	rows, err := a.db.Query(r.Context(),
		`WITH RECURSIVE thread AS (
		    SELECT p.* FROM pulse_posts p WHERE p.id = $1
		    UNION ALL
		    SELECT c.* FROM pulse_posts c JOIN thread t ON c.parent_id = t.id
		 )
		 SELECT p.id, p.author_id, COALESCE(u.username,''), p.body, p.parent_id,
		        p.quote_of, p.repost_of, p.topics, p.local_tag, p.created_at,
		        (SELECT COUNT(*) FROM pulse_posts x WHERE x.parent_id = p.id),
		        (SELECT COUNT(*) FROM pulse_posts x WHERE x.repost_of = p.id),
		        (SELECT COUNT(*) FROM pulse_posts x WHERE x.quote_of = p.id)
		   FROM thread p LEFT JOIN users u ON u.id = p.author_id
		  ORDER BY p.created_at ASC`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load thread")
		return
	}
	posts := scanPulsePosts(rows)
	if len(posts) == 0 {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	// X-style views: branch (root + direct replies), chronological (as stored),
	// relevant (highest-engagement first).
	relevant := make([]pulsePostJSON, len(posts))
	copy(relevant, posts)
	for i := 0; i < len(relevant); i++ {
		for j := i + 1; j < len(relevant); j++ {
			si := relevant[i].ReplyCount + relevant[i].RepostCount + relevant[i].QuoteCount
			sj := relevant[j].ReplyCount + relevant[j].RepostCount + relevant[j].QuoteCount
			if sj > si {
				relevant[i], relevant[j] = relevant[j], relevant[i]
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"root":         posts[0],
		"posts":        posts,
		"chronological": posts,
		"relevant":     relevant,
	})
}

// GET /api/pulse/trends — global trends computed from real post volume.
func (a *App) handlePulseTrends(w http.ResponseWriter, r *http.Request) {
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	if scope != "local" {
		scope = "global"
	}
	region := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("region")))
	// Compute live from the last 24h of real posts rather than trusting a
	// possibly-stale snapshot row.
	q := `
	 SELECT topic, COUNT(*) AS score
	   FROM pulse_posts p, unnest(p.topics) AS topic
	  WHERE p.created_at > now() - interval '24 hours'`
	args := []any{}
	if scope == "local" {
		if region == "" {
			writeErr(w, http.StatusBadRequest, "region required for local trends")
			return
		}
		q += ` AND p.local_tag = $1`
		args = append(args, region)
	}
	q += ` GROUP BY topic ORDER BY score DESC, topic ASC LIMIT 30`
	rows, err := a.db.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load trends")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var topic string
		var score int
		if err := rows.Scan(&topic, &score); err == nil {
			out = append(out, map[string]any{"topic": topic, "score": score, "scope": scope})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"trends": out})
}

// GET /api/pulse/topics — topic catalog with real post counts.
func (a *App) handlePulseTopicList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT t.name, t.description,
		        (SELECT COUNT(*) FROM pulse_posts p WHERE t.name = ANY(p.topics))
		   FROM pulse_topics t ORDER BY t.name ASC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load topics")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name, desc string
		var count int
		if err := rows.Scan(&name, &desc, &count); err == nil {
			out = append(out, map[string]any{"name": name, "description": desc, "posts": count})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": out})
}

// GET /api/pulse/users/{id} — a user's Pulse timeline.
func (a *App) handlePulseUserTimeline(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUUIDPath(w, r, "id")
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), pulseSelect+
		` WHERE p.author_id = $1 ORDER BY p.created_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load timeline")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": scanPulsePosts(rows)})
}

// DELETE /api/pulse/posts/{id} — author deletion.
func (a *App) handlePulseDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUUIDPath(w, r, "id")
	if !ok {
		return
	}
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM pulse_posts WHERE id=$1 AND author_id=$2`,
		id, userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- Lists ----

// POST /api/pulse/lists — create a curated list.
func (a *App) handlePulseListCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeErr(w, http.StatusBadRequest, "name required (max 100 chars)")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO pulse_lists (owner_id, name) VALUES ($1,$2) RETURNING id`,
		userIDFrom(r), name).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			writeErr(w, http.StatusConflict, "list already exists")
			return
		}
		writeErr(w, http.StatusBadRequest, "list creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "name": name})
}

// GET /api/pulse/lists — the caller's lists with member counts.
func (a *App) handlePulseListMine(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT l.id, l.name, l.created_at,
		        (SELECT COUNT(*) FROM pulse_list_members m WHERE m.list_id = l.id)
		   FROM pulse_lists l WHERE l.owner_id=$1 ORDER BY l.created_at DESC`,
		userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load lists")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var created time.Time
		var count int
		if err := rows.Scan(&id, &name, &created, &count); err == nil {
			out = append(out, map[string]any{
				"id": id, "name": name, "created_at": created, "member_count": count,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": out})
}

// PUT /api/pulse/lists/{id}/members/{uid} — add a member.
func (a *App) handlePulseListAdd(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireUUIDPath(w, r, "id")
	if !ok {
		return
	}
	memberID, ok := requireUUIDPath(w, r, "uid")
	if !ok {
		return
	}
	uid := userIDFrom(r)
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT owner_id FROM pulse_lists WHERE id=$1`, listID).Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, "list not found")
		return
	}
	if owner != uid {
		writeErr(w, http.StatusForbidden, "not your list")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO pulse_list_members (list_id, member_id) VALUES ($1,$2)
		 ON CONFLICT DO NOTHING`, listID, memberID); err != nil {
		writeErr(w, http.StatusBadRequest, "member not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "added"})
}

// DELETE /api/pulse/lists/{id}/members/{uid} — remove a member.
func (a *App) handlePulseListRemove(w http.ResponseWriter, r *http.Request) {
	listID, ok := requireUUIDPath(w, r, "id")
	if !ok {
		return
	}
	memberID, ok := requireUUIDPath(w, r, "uid")
	if !ok {
		return
	}
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT owner_id FROM pulse_lists WHERE id=$1`, listID).Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, "list not found")
		return
	}
	if owner != userIDFrom(r) {
		writeErr(w, http.StatusForbidden, "not your list")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`DELETE FROM pulse_list_members WHERE list_id=$1 AND member_id=$2`,
		listID, memberID); err != nil {
		writeErr(w, http.StatusInternalServerError, "remove failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// sweepPulseTrends materialises the trend snapshot table from real post
// volume. It is the same aggregation the trends endpoint performs live, kept
// so a persisted, ordered snapshot exists for the breaking-news surface.
func (a *App) sweepPulseTrends() {
	_, _ = a.db.Exec(context.Background(),
		`INSERT INTO pulse_trends (topic, scope, region, score, window_start)
		 SELECT topic, 'global', '', COUNT(*)::int, now()
		   FROM pulse_posts p, unnest(p.topics) AS topic
		  WHERE p.created_at > now() - interval '24 hours'
		  GROUP BY topic
		 ON CONFLICT (topic, scope, region)
		 DO UPDATE SET score = EXCLUDED.score, computed_at = now(),
		               window_start = EXCLUDED.window_start`)
	_, _ = a.db.Exec(context.Background(),
		`INSERT INTO pulse_trends (topic, scope, region, score, window_start)
		 SELECT topic, 'local', p.local_tag, COUNT(*)::int, now()
		   FROM pulse_posts p, unnest(p.topics) AS topic
		  WHERE p.created_at > now() - interval '24 hours' AND p.local_tag <> ''
		  GROUP BY topic, p.local_tag
		 ON CONFLICT (topic, scope, region)
		 DO UPDATE SET score = EXCLUDED.score, computed_at = now(),
		               window_start = EXCLUDED.window_start`)
}

// startPulseTrendWorker refreshes the persisted trend snapshot every ten
// minutes so the trends and breaking surfaces read from warm data.
func (a *App) startPulseTrendWorker() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			a.sweepPulseTrends()
			<-ticker.C
		}
	}()
}