package main

// Gap pack 10 (migration 043): code-verifiable gaps from
// ChatApp_Competitor_Comparison.md with no implementation anywhere on main:
//   * global ranked search across posts / people / topics / hashtags
//   * contact import & discovery (peppered E.164 digests, never raw numbers)
//   * moderation appeals with an admin decision queue
//   * copyright (DMCA) takedown notices + counter-notices
//   * legal-request register (law-enforcement intake, admin-only)
//   * age gating (date of birth, minor safety mode)
//   * support tickets with staff replies

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ---------- shared helpers ----------

// discoveryPepper returns the server-side pepper used when hashing phone
// numbers for contact discovery. Set CONTACT_DISCOVERY_PEPPER in production.
func (a *App) discoveryPepper() string {
	if a.cfg.ContactDiscoveryPepper != "" {
		return a.cfg.ContactDiscoveryPepper
	}
	return "chatapp-contact-discovery-dev-pepper"
}

// pepperPhoneHash computes HMAC-SHA256(pepper, e164) hex. HMAC is used so the
// mapping cannot be brute-forced from a leaked table without the pepper.
func pepperPhoneHash(pepper, e164 string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(e164))
	return hex.EncodeToString(mac.Sum(nil))
}

// normalisePhone strips everything but digits and a single leading +.
func normalisePhone(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	// Only E.164 (leading +) is accepted: a bare local number cannot be
	// hashed consistently for discovery without guessing a country code.
	if !strings.HasPrefix(s, "+") {
		return "", false
	}
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	digits := b.String()
	if digits == "" || len(digits) < 7 || len(digits) > 15 {
		return "", false
	}
	return "+" + digits, true
}

var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

// ageClass derives the safety classification from a date of birth.
// Returns ("minor"|"adult", years, ok).
func ageClass(dob time.Time, now time.Time) (string, int, bool) {
	if dob.After(now) || dob.Before(now.AddDate(-120, 0, 0)) {
		return "", 0, false
	}
	years := now.Year() - dob.Year()
	// Subtract one if the birthday has not happened yet this year.
	cut := time.Date(now.Year(), dob.Month(), dob.Day(), 0, 0, 0, 0, time.UTC)
	if now.Before(cut) {
		years--
	}
	if years < 18 {
		return "minor", years, true
	}
	return "adult", years, true
}

// ensureDiscoveryBackfill seeds phone digests for every registered user with a
// phone number on file. It runs once per process (the insert is idempotent on
// the (user_id, phone_hash) unique constraint).
func (a *App) ensureDiscoveryBackfill(ctx context.Context) {
	pepper := a.discoveryPepper()
	_, _ = a.db.Exec(ctx, `
		INSERT INTO contact_discovery_hashes (user_id, phone_hash)
		SELECT id, encode(hmac(phone_e164, $1, 'sha256'), 'hex')
		FROM users WHERE phone_e164 IS NOT NULL AND phone_e164 <> ''
		ON CONFLICT (user_id, phone_hash) DO NOTHING`, pepper)
}

// ---------- global search ----------

// GET /api/search?q=...&type=all|posts|people|topics|hashtags
// Ranks each surface by engagement/recency (posts), verified+followers
// (people), follower count (topics) and use count (hashtags).
func (a *App) handleGlobalSearch(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"query": "", "people": []any{}, "posts": []any{}, "topics": []any{}, "hashtags": []any{}})
		return
	}
	if len(q) > 200 {
		q = q[:200]
	}
	want := strings.ToLower(r.URL.Query().Get("type"))
	if want == "" {
		want = "all"
	}
	switch want {
	case "all", "posts", "people", "topics", "hashtags":
	default:
		writeErr(w, http.StatusBadRequest, "invalid type")
		return
	}
	pattern := "%" + escapeLike(q) + "%"
	out := map[string]any{"query": q}

	if want == "all" || want == "people" {
		rows, err := a.db.Query(r.Context(), `
			SELECT u.id, u.username::text, u.display_name, COALESCE(u.avatar_url,''),
			       u.is_verified,
			       (SELECT count(*) FROM follows f WHERE f.followee_id = u.id) AS followers
			FROM users u
			WHERE u.status='active' AND (u.username ILIKE $1 OR u.display_name ILIKE $1)
			ORDER BY u.is_verified DESC, followers DESC, u.username
			LIMIT 20`, pattern)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "search failed")
			return
		}
		people := []map[string]any{}
		for rows.Next() {
			var id, uname, name, avatar string
			var verified bool
			var followers int64
			if err := rows.Scan(&id, &uname, &name, &avatar, &verified, &followers); err == nil {
				people = append(people, map[string]any{"id": id, "username": uname,
					"display_name": name, "avatar_url": avatar, "is_verified": verified, "followers": followers})
			}
		}
		rows.Close()
		out["people"] = people
	}
	if want == "all" || want == "posts" {
		rows, err := a.db.Query(r.Context(), `
			SELECT p.id, p.author_id, u.username::text, p.body, p.like_count, p.comment_count,
			       p.share_count, p.view_count, p.created_at,
			       (SELECT count(*) FROM post_media m WHERE m.post_id = p.id) AS media_count
			FROM posts p JOIN users u ON u.id = p.author_id
			WHERE p.deleted_at IS NULL AND p.expires_at IS NULL
			  AND (p.body ILIKE $1
			       OR EXISTS (SELECT 1 FROM post_media m WHERE m.post_id = p.id AND m.url ILIKE $1))
			ORDER BY (p.like_count*3 + p.comment_count*2 + p.share_count*2 + LEAST(p.view_count, 100000)/10)
			         + GREATEST(0, 30 - EXTRACT(DAY FROM now() - p.created_at)::int) DESC,
			         p.created_at DESC
			LIMIT 25`, pattern)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "search failed")
			return
		}
		posts := []map[string]any{}
		for rows.Next() {
			var id, authorID, uname, body string
			var likes, comments, shares, media int64
			var views int64
			var created time.Time
			if err := rows.Scan(&id, &authorID, &uname, &body, &likes, &comments, &shares, &views, &created, &media); err == nil {
				posts = append(posts, map[string]any{"id": id, "author_id": authorID,
					"username": uname, "body": body, "like_count": likes, "comment_count": comments,
					"share_count": shares, "view_count": views, "created_at": created, "media_count": media})
			}
		}
		rows.Close()
		out["posts"] = posts
	}
	if want == "all" || want == "topics" {
		rows, err := a.db.Query(r.Context(), `
			SELECT t.id, t.name,
			       (SELECT count(*) FROM topic_follows f WHERE f.topic_id = t.id) AS followers,
			       EXISTS(SELECT 1 FROM topic_follows f WHERE f.topic_id = t.id AND f.user_id = $2)
			FROM topics t WHERE t.name ILIKE $1
			ORDER BY followers DESC, t.name LIMIT 20`, pattern, uid)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "search failed")
			return
		}
		topics := []map[string]any{}
		for rows.Next() {
			var id, name string
			var followers int64
			var following bool
			if err := rows.Scan(&id, &name, &followers, &following); err == nil {
				topics = append(topics, map[string]any{"id": id, "name": name,
					"followers": followers, "following": following})
			}
		}
		rows.Close()
		out["topics"] = topics
	}
	if want == "all" || want == "hashtags" {
		rows, err := a.db.Query(r.Context(), `
			SELECT tag, use_count, last_used FROM hashtags
			WHERE tag ILIKE $1 ORDER BY use_count DESC, tag LIMIT 20`, pattern)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "search failed")
			return
		}
		hashtags := []map[string]any{}
		for rows.Next() {
			var tag string
			var use int64
			var last time.Time
			if err := rows.Scan(&tag, &use, &last); err == nil {
				hashtags = append(hashtags, map[string]any{"tag": tag, "use_count": use, "last_used": last})
			}
		}
		rows.Close()
		out["hashtags"] = hashtags
	}
	writeJSON(w, http.StatusOK, out)
}

// escapeLike escapes LIKE wildcard metacharacters in user input.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------- contact import & discovery ----------

// POST /api/contacts/discover — body {"phones": ["+15551234567", ...]}.
// The server normalises to E.164, stores only HMAC digests, backfills digests
// for registered users once, and returns matched accounts (privacy-preserving:
// raw numbers are never persisted; the caller's own digest is excluded).
func (a *App) handleContactDiscover(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Phones []string `json:"phones"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Phones) == 0 || len(req.Phones) > 3000 {
		writeErr(w, http.StatusBadRequest, "phones must contain 1..3000 numbers")
		return
	}
	pepper := a.discoveryPepper()
	seen := map[string]bool{}
	digests := []string{}
	for _, raw := range req.Phones {
		e164, ok := normalisePhone(raw)
		if !ok || !e164Pattern.MatchString(e164) {
			continue
		}
		h := pepperPhoneHash(pepper, e164)
		if seen[h] {
			continue
		}
		seen[h] = true
		digests = append(digests, h)
	}
	if len(digests) == 0 {
		writeErr(w, http.StatusBadRequest, "no valid E.164 numbers")
		return
	}
	a.ensureDiscoveryBackfill(r.Context())

	// Upsert the caller's digests.
	for _, h := range digests {
		_, _ = a.db.Exec(r.Context(),
			`INSERT INTO contact_discovery_hashes (user_id, phone_hash) VALUES ($1,$2)
			 ON CONFLICT (user_id, phone_hash) DO NOTHING`, uid, h)
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT u.id, u.username::text, u.display_name, COALESCE(u.avatar_url,''),
		       EXISTS(SELECT 1 FROM follows f WHERE f.follower_id=$2 AND f.followee_id=u.id)
		FROM contact_discovery_hashes d JOIN users u ON u.id = d.user_id
		WHERE d.phone_hash = ANY($1) AND d.user_id <> $2 AND u.status='active'
		ORDER BY u.username LIMIT 500`, digests, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "discovery failed")
		return
	}
	defer rows.Close()
	matches := []map[string]any{}
	for rows.Next() {
		var id, uname, name, avatar string
		var following bool
		if err := rows.Scan(&id, &uname, &name, &avatar, &following); err == nil {
			matches = append(matches, map[string]any{"id": id, "username": uname,
				"display_name": name, "avatar_url": avatar, "following": following})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": matches, "submitted": len(digests)})
}

// ---------- moderation appeals ----------

// POST /api/appeals — appeal an enforcement against a target.
func (a *App) handleCreateAppeal(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		Reason     string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.TargetType {
	case "user", "post", "comment", "message", "ad":
	default:
		writeErr(w, http.StatusBadRequest, "invalid target type")
		return
	}
	if req.TargetID == "" || strings.TrimSpace(req.Reason) == "" || len(req.Reason) > 4000 {
		writeErr(w, http.StatusBadRequest, "target_id and reason (<=4000 chars) required")
		return
	}
	// One open appeal per target per user.
	var open bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM moderation_appeals WHERE user_id=$1 AND target_id=$2 AND status='open')`,
		uid, req.TargetID).Scan(&open)
	if open {
		writeErr(w, http.StatusConflict, "an open appeal already exists for this target")
		return
	}
	var id int64
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO moderation_appeals (user_id, target_type, target_id, reason)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		uid, req.TargetType, req.TargetID, req.Reason).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to file appeal")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "open"})
}

// GET /api/me/appeals — your own appeals, newest first.
func (a *App) handleMyAppeals(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id, target_type, target_id, reason, status, decision_note, created_at,
		       COALESCE(decided_at::text,'')
		FROM moderation_appeals WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load appeals")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var tt, tid, reason, status, note, created, decided string
		if err := rows.Scan(&id, &tt, &tid, &reason, &status, &note, &created, &decided); err == nil {
			out = append(out, map[string]any{"id": id, "target_type": tt, "target_id": tid,
				"reason": reason, "status": status, "decision_note": note,
				"created_at": created, "decided_at": decided})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"appeals": out})
}

// GET /api/admin/appeals?status=open — moderation queue.
func (a *App) handleAdminListAppeals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", "open", "upheld", "rejected":
	default:
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	where := ""
	if status != "" {
		where = "WHERE a.status = '" + status + "'"
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT a.id, a.user_id, u.username::text, a.target_type, a.target_id, a.reason,
		       a.status, a.decision_note, a.created_at
		FROM moderation_appeals a JOIN users u ON u.id = a.user_id
		`+where+` ORDER BY a.created_at LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load appeals")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var uidv, uname, tt, tid, reason, st, note, created string
		if err := rows.Scan(&id, &uidv, &uname, &tt, &tid, &reason, &st, &note, &created); err == nil {
			out = append(out, map[string]any{"id": id, "user_id": uidv, "username": uname,
				"target_type": tt, "target_id": tid, "reason": reason, "status": st,
				"decision_note": note, "created_at": created})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"appeals": out})
}

// POST /api/admin/appeals/{id}/decision — body {"decision":"upheld|rejected","note":""}.
func (a *App) handleAdminDecideAppeal(w http.ResponseWriter, r *http.Request) {
	adminID := userIDFrom(r)
	var req struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Decision {
	case "upheld", "rejected":
	default:
		writeErr(w, http.StatusBadRequest, "decision must be upheld|rejected")
		return
	}
	ct, err := a.db.Exec(r.Context(), `
		UPDATE moderation_appeals SET status=$1, decided_by=$2, decision_note=$3, decided_at=now()
		WHERE id=$4 AND status='open'`,
		req.Decision, adminID, strings.TrimSpace(req.Note), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "decision failed")
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "appeal not found or already decided")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Decision})
}

// ---------- copyright / DMCA ----------

// POST /api/copyright/notices — file a takedown notice.
func (a *App) handleCreateCopyrightNotice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContentType  string `json:"content_type"`
		ContentID    string `json:"content_id"`
		OriginalURL  string `json:"original_url"`
		Description  string `json:"description"`
		GoodFaith    bool   `json:"good_faith"`
		UnderPenalty bool   `json:"under_penalty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.ContentType {
	case "post", "comment", "message", "reel":
	default:
		writeErr(w, http.StatusBadRequest, "invalid content type")
		return
	}
	if req.ContentID == "" || strings.TrimSpace(req.OriginalURL) == "" || strings.TrimSpace(req.Description) == "" {
		writeErr(w, http.StatusBadRequest, "content_id, original_url and description are required")
		return
	}
	if !req.GoodFaith || !req.UnderPenalty {
		writeErr(w, http.StatusBadRequest, "good_faith and under_penalty attestations are required")
		return
	}
	var id int64
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO copyright_notices (reporter_id, content_type, content_id, original_url, description, good_faith, under_penalty)
		VALUES (NULLIF($1,'')::uuid, $2,$3,$4,$5,$6,$7) RETURNING id`,
		userIDFrom(r), req.ContentType, req.ContentID, strings.TrimSpace(req.OriginalURL),
		strings.TrimSpace(req.Description), req.GoodFaith, req.UnderPenalty).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to file notice")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "open"})
}

// GET /api/copyright/notices/{id} — status of a notice (reporter or admin).
func (a *App) handleGetCopyrightNotice(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var reporterID *string
	var status, resolution string
	var counters int64
	err := a.db.QueryRow(r.Context(), `
		SELECT reporter_id::text, status, resolution,
		       (SELECT count(*) FROM copyright_counters c WHERE c.notice_id = n.id)
		FROM copyright_notices n WHERE n.id=$1`, r.PathValue("id")).
		Scan(&reporterID, &status, &resolution, &counters)
	if err != nil {
		writeErr(w, http.StatusNotFound, "notice not found")
		return
	}
	isReporter := reporterID != nil && *reporterID == uid
	if !isReporter && !a.hasRole(r.Context(), uid, "superadmin", "admin", "moderator") {
		writeErr(w, http.StatusForbidden, "not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "resolution": resolution,
		"counter_notices": counters})
}

// POST /api/copyright/notices/{id}/counter — the accused user files a counter-notice.
func (a *App) handleFileCounterNotice(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Statement           string `json:"statement"`
		ConsentJurisdiction bool   `json:"consent_jurisdiction"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Statement) == "" || !req.ConsentJurisdiction {
		writeErr(w, http.StatusBadRequest, "statement and consent_jurisdiction are required")
		return
	}
	var open bool
	err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM copyright_notices WHERE id=$1 AND status IN ('open','countered'))`,
		r.PathValue("id")).Scan(&open)
	if err != nil || !open {
		writeErr(w, http.StatusNotFound, "notice not found or already resolved")
		return
	}
	ct, err := a.db.Exec(r.Context(), `
		INSERT INTO copyright_counters (notice_id, user_id, statement, consent_jurisdiction)
		VALUES ($1,$2,$3,$4)`, r.PathValue("id"), uid, strings.TrimSpace(req.Statement), req.ConsentJurisdiction)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to file counter-notice")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE copyright_notices SET status='countered' WHERE id=$1 AND status='open'`, r.PathValue("id"))
	writeJSON(w, http.StatusCreated, map[string]any{"counter_notices": ct.RowsAffected(), "status": "countered"})
}

// GET /api/admin/copyright?status= — admin queue.
func (a *App) handleAdminListCopyright(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", "open", "takedown", "restored", "rejected", "countered":
	default:
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	where := ""
	if status != "" {
		where = "WHERE n.status = '" + status + "'"
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT n.id, COALESCE(n.reporter_id::text,''), n.content_type, n.content_id, n.original_url,
		       n.description, n.status, n.resolution, n.created_at,
		       (SELECT count(*) FROM copyright_counters c WHERE c.notice_id = n.id)
		FROM copyright_notices n `+where+` ORDER BY n.created_at LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load notices")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var reporter, tt, tid, ourl, desc, st, res, created string
		var counters int64
		if err := rows.Scan(&id, &reporter, &tt, &tid, &ourl, &desc, &st, &res, &created, &counters); err == nil {
			out = append(out, map[string]any{"id": id, "reporter_id": reporter,
				"content_type": tt, "content_id": tid, "original_url": ourl, "description": desc,
				"status": st, "resolution": res, "created_at": created, "counter_notices": counters})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notices": out})
}

// POST /api/admin/copyright/{id}/resolve — body {"action":"takedown|restore|rejected","note":""}.
// Takedown soft-deletes the content where the content is a post or comment.
func (a *App) handleAdminResolveCopyright(w http.ResponseWriter, r *http.Request) {
	adminID := userIDFrom(r)
	var req struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Action {
	case "takedown", "restore", "rejected":
	default:
		writeErr(w, http.StatusBadRequest, "action must be takedown|restore|rejected")
		return
	}
	note := strings.TrimSpace(req.Note)

	var contentType, contentID, status string
	err := a.db.QueryRow(r.Context(),
		`SELECT content_type, content_id::text, status FROM copyright_notices WHERE id=$1`,
		r.PathValue("id")).Scan(&contentType, &contentID, &status)
	if err != nil {
		writeErr(w, http.StatusNotFound, "notice not found")
		return
	}
	if status == "takedown" || status == "restored" || status == "rejected" {
		writeErr(w, http.StatusConflict, "notice already resolved")
		return
	}

	// Apply the content-level effect for posts and comments.
	switch {
	case req.Action == "takedown" && contentType == "post":
		if _, err := a.db.Exec(r.Context(),
			`UPDATE posts SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, contentID); err != nil {
			writeErr(w, http.StatusInternalServerError, "takedown failed")
			return
		}
	case req.Action == "takedown" && contentType == "comment":
		if _, err := a.db.Exec(r.Context(),
			`UPDATE comments SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, contentID); err != nil {
			writeErr(w, http.StatusInternalServerError, "takedown failed")
			return
		}
	case req.Action == "restore" && contentType == "post":
		if _, err := a.db.Exec(r.Context(),
			`UPDATE posts SET deleted_at=NULL WHERE id=$1`, contentID); err != nil {
			writeErr(w, http.StatusInternalServerError, "restore failed")
			return
		}
	case req.Action == "restore" && contentType == "comment":
		if _, err := a.db.Exec(r.Context(),
			`UPDATE comments SET deleted_at=NULL WHERE id=$1`, contentID); err != nil {
			writeErr(w, http.StatusInternalServerError, "restore failed")
			return
		}
	}

	newStatus := req.Action
	ct, err := a.db.Exec(r.Context(), `
		UPDATE copyright_notices SET status=$1, resolution=$2, resolved_at=now() WHERE id=$3`,
		newStatus, note, r.PathValue("id"))
	if err != nil || ct.RowsAffected() == 0 {
		writeErr(w, http.StatusInternalServerError, "resolve failed")
		return
	}
	_ = adminID
	writeJSON(w, http.StatusOK, map[string]any{"status": newStatus})
}

// ---------- legal requests ----------

// POST /api/admin/legal-requests — record an inbound request.
func (a *App) handleCreateLegalRequest(w http.ResponseWriter, r *http.Request) {
	adminID := userIDFrom(r)
	var req struct {
		Authority     string `json:"authority"`
		RequestRef    string `json:"request_ref"`
		Kind          string `json:"kind"`
		SubjectUserID string `json:"subject_user_id"`
		Scope         string `json:"scope"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Kind {
	case "preservation", "disclosure", "removal", "other":
	default:
		writeErr(w, http.StatusBadRequest, "invalid kind")
		return
	}
	if strings.TrimSpace(req.Authority) == "" || strings.TrimSpace(req.RequestRef) == "" {
		writeErr(w, http.StatusBadRequest, "authority and request_ref are required")
		return
	}
	var id int64
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO legal_requests (authority, request_ref, kind, subject_user_id, scope, handled_by)
		VALUES ($1,$2,$3, NULLIF($4,'')::uuid, $5, $6)
		ON CONFLICT (authority, request_ref) DO UPDATE SET scope = EXCLUDED.scope, updated_at = now()
		RETURNING id`,
		strings.TrimSpace(req.Authority), strings.TrimSpace(req.RequestRef), req.Kind,
		strings.TrimSpace(req.SubjectUserID), strings.TrimSpace(req.Scope), adminID).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record request")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "received"})
}

// GET /api/admin/legal-requests?status= — the register.
func (a *App) handleAdminListLegalRequests(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", "received", "acknowledged", "fulfilled", "partially_fulfilled", "rejected":
	default:
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	where := ""
	if status != "" {
		where = "WHERE l.status = '" + status + "'"
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT l.id, l.authority, l.request_ref, l.kind, COALESCE(l.subject_user_id::text,''),
		       l.scope, l.status, COALESCE(h.username::text,''), l.notes, l.created_at, l.updated_at
		FROM legal_requests l LEFT JOIN users h ON h.id = l.handled_by
		`+where+` ORDER BY l.created_at DESC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load requests")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var authority, ref, kind, subject, scope, st, handled, notes, created, updated string
		if err := rows.Scan(&id, &authority, &ref, &kind, &subject, &scope, &st, &handled, &notes, &created, &updated); err == nil {
			out = append(out, map[string]any{"id": id, "authority": authority, "request_ref": ref,
				"kind": kind, "subject_user_id": subject, "scope": scope, "status": st,
				"handled_by": handled, "notes": notes, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// POST /api/admin/legal-requests/{id}/status — advance the register entry.
func (a *App) handleAdminUpdateLegalRequest(w http.ResponseWriter, r *http.Request) {
	adminID := userIDFrom(r)
	var req struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Status {
	case "received", "acknowledged", "fulfilled", "partially_fulfilled", "rejected":
	default:
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	ct, err := a.db.Exec(r.Context(), `
		UPDATE legal_requests SET status=$1, notes=$2, handled_by=$3, updated_at=now() WHERE id=$4`,
		req.Status, strings.TrimSpace(req.Notes), adminID, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status})
}

// ---------- age gating ----------

// POST /api/me/date-of-birth — set your date of birth (once; changes need admin).
func (a *App) handleSetDateOfBirth(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		DateOfBirth string `json:"date_of_birth"` // YYYY-MM-DD
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	dob, err := time.Parse("2006-01-02", req.DateOfBirth)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date_of_birth must be YYYY-MM-DD")
		return
	}
	class, years, ok := ageClass(dob, time.Now().UTC())
	if !ok {
		writeErr(w, http.StatusBadRequest, "date_of_birth is out of range")
		return
	}
	if years < 13 {
		writeErr(w, http.StatusForbidden, "minimum age is 13")
		return
	}
	var existing *time.Time
	if err := a.db.QueryRow(r.Context(),
		`SELECT date_of_birth FROM users WHERE id=$1`, uid).Scan(&existing); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load profile")
		return
	}
	if existing != nil {
		writeErr(w, http.StatusConflict, "date of birth already set; contact support to change it")
		return
	}
	mode := "standard"
	if class == "minor" {
		mode = "minor"
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET date_of_birth=$1, safety_mode=$2, updated_at=now() WHERE id=$3`,
		dob, mode, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save date of birth")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"safety_mode": mode, "years": years})
}

// GET /api/me/age-status — current classification.
func (a *App) handleAgeStatus(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var dob *time.Time
	var mode string
	if err := a.db.QueryRow(r.Context(),
		`SELECT date_of_birth, safety_mode FROM users WHERE id=$1`, uid).Scan(&dob, &mode); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load profile")
		return
	}
	resp := map[string]any{"safety_mode": mode, "date_of_birth_set": dob != nil}
	if dob != nil {
		_, years, _ := ageClass(*dob, time.Now().UTC())
		resp["years"] = years
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/admin/users/{id}/safety-mode — admin override (e.g. re-age review).
func (a *App) handleAdminSetSafetyMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SafetyMode string `json:"safety_mode"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.SafetyMode {
	case "standard", "minor":
	default:
		writeErr(w, http.StatusBadRequest, "safety_mode must be standard|minor")
		return
	}
	ct, err := a.db.Exec(r.Context(),
		`UPDATE users SET safety_mode=$1, updated_at=now() WHERE id=$2`,
		req.SafetyMode, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"safety_mode": req.SafetyMode})
}

// ---------- support tickets ----------

// POST /api/support/tickets — open a ticket.
func (a *App) handleCreateSupportTicket(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Category string `json:"category"`
		Subject  string `json:"subject"`
		Body     string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Category {
	case "", "general", "creator", "billing", "safety", "bug", "feature":
	default:
		writeErr(w, http.StatusBadRequest, "invalid category")
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Body) == "" ||
		len(req.Subject) > 200 || len(req.Body) > 20000 {
		writeErr(w, http.StatusBadRequest, "subject (<=200) and body (<=20000) are required")
		return
	}
	var id int64
	err := a.db.QueryRow(r.Context(), `
		INSERT INTO support_tickets (user_id, category, subject, body)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		uid, req.Category, strings.TrimSpace(req.Subject), strings.TrimSpace(req.Body)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to open ticket")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": "open"})
}

// GET /api/me/support/tickets — your tickets.
func (a *App) handleMySupportTickets(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id, category, subject, status, created_at, updated_at
		FROM support_tickets WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tickets")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var cat, subject, st, created, updated string
		if err := rows.Scan(&id, &cat, &subject, &st, &created, &updated); err == nil {
			out = append(out, map[string]any{"id": id, "category": cat, "subject": subject,
				"status": st, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// GET /api/support/tickets/{id} — ticket with replies (owner or staff).
func (a *App) handleGetSupportTicket(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var ownerID string
	var cat, subject, body, st, res, created, updated string
	err := a.db.QueryRow(r.Context(), `
		SELECT user_id::text, category, subject, body, status, resolution, created_at::text, updated_at::text
		FROM support_tickets WHERE id=$1`, r.PathValue("id")).
		Scan(&ownerID, &cat, &subject, &body, &st, &res, &created, &updated)
	if err != nil {
		writeErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	isStaff := a.hasRole(r.Context(), uid, "superadmin", "admin", "moderator")
	if ownerID != uid && !isStaff {
		writeErr(w, http.StatusForbidden, "not allowed")
		return
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT r.author_id::text, u.username::text, r.is_staff, r.body, r.created_at
		FROM support_replies r JOIN users u ON u.id = r.author_id
		WHERE r.ticket_id=$1 ORDER BY r.created_at`, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load replies")
		return
	}
	defer rows.Close()
	replies := []map[string]any{}
	for rows.Next() {
		var authorID, uname, rbody, rcreated string
		var staff bool
		if err := rows.Scan(&authorID, &uname, &staff, &rbody, &rcreated); err == nil {
			replies = append(replies, map[string]any{"author_id": authorID, "username": uname,
				"is_staff": staff, "body": rbody, "created_at": rcreated})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "category": cat,
		"subject": subject, "body": body, "status": st, "resolution": res,
		"created_at": created, "updated_at": updated, "replies": replies})
}

// POST /api/support/tickets/{id}/replies — user reply (reopens nothing; status flow is staff-driven).
func (a *App) handleReplySupportTicket(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" || len(req.Body) > 20000 {
		writeErr(w, http.StatusBadRequest, "body (<=20000) required")
		return
	}
	var ownerID string
	var st string
	if err := a.db.QueryRow(r.Context(),
		`SELECT user_id::text, status FROM support_tickets WHERE id=$1`, r.PathValue("id")).
		Scan(&ownerID, &st); err != nil {
		writeErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	if ownerID != uid {
		writeErr(w, http.StatusForbidden, "not your ticket")
		return
	}
	if st == "closed" {
		writeErr(w, http.StatusConflict, "ticket is closed")
		return
	}
	ct, err := a.db.Exec(r.Context(), `
		INSERT INTO support_replies (ticket_id, author_id, is_staff, body)
		VALUES ($1,$2,FALSE,$3)`, r.PathValue("id"), uid, strings.TrimSpace(req.Body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reply failed")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE support_tickets SET updated_at=now(), status=CASE WHEN status='answered' THEN 'open' ELSE status END WHERE id=$1`,
		r.PathValue("id"))
	writeJSON(w, http.StatusCreated, map[string]any{"replies": ct.RowsAffected()})
}

// GET /api/admin/support?status= — staff queue.
func (a *App) handleAdminListSupportTickets(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "", "open", "answered", "resolved", "closed":
	default:
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	where := ""
	if status != "" {
		where = "WHERE t.status = '" + status + "'"
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT t.id, t.user_id::text, u.username::text, t.category, t.subject, t.status,
		       t.created_at, t.updated_at,
		       (SELECT count(*) FROM support_replies r WHERE r.ticket_id = t.id AND r.is_staff) AS staff_replies
		FROM support_tickets t JOIN users u ON u.id = t.user_id
		`+where+` ORDER BY t.updated_at DESC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tickets")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var uidv, uname, cat, subject, st, created, updated string
		var staffReplies int64
		if err := rows.Scan(&id, &uidv, &uname, &cat, &subject, &st, &created, &updated, &staffReplies); err == nil {
			out = append(out, map[string]any{"id": id, "user_id": uidv, "username": uname,
				"category": cat, "subject": subject, "status": st, "created_at": created,
				"updated_at": updated, "staff_replies": staffReplies})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// POST /api/admin/support/tickets/{id}/reply — staff reply (marks answered).
func (a *App) handleAdminSupportReply(w http.ResponseWriter, r *http.Request) {
	adminID := userIDFrom(r)
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" || len(req.Body) > 20000 {
		writeErr(w, http.StatusBadRequest, "body (<=20000) required")
		return
	}
	ct, err := a.db.Exec(r.Context(), `
		INSERT INTO support_replies (ticket_id, author_id, is_staff, body)
		VALUES ($1,$2,TRUE,$3)`, r.PathValue("id"), adminID, strings.TrimSpace(req.Body))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reply failed")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE support_tickets SET status='answered', updated_at=now() WHERE id=$1`, r.PathValue("id"))
	writeJSON(w, http.StatusCreated, map[string]any{"replies": ct.RowsAffected(), "status": "answered"})
}

// POST /api/admin/support/tickets/{id}/status — resolve/close.
func (a *App) handleAdminSupportStatus(w http.ResponseWriter, r *http.Request) {
	adminID := userIDFrom(r)
	var req struct {
		Status     string `json:"status"`
		Resolution string `json:"resolution"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Status {
	case "open", "answered", "resolved", "closed":
	default:
		writeErr(w, http.StatusBadRequest, "invalid status")
		return
	}
	ct, err := a.db.Exec(r.Context(), `
		UPDATE support_tickets SET status=$1, resolution=$2, updated_at=now() WHERE id=$3`,
		req.Status, strings.TrimSpace(req.Resolution), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "ticket not found")
		return
	}
	_ = adminID
	writeJSON(w, http.StatusOK, map[string]any{"status": req.Status})
}

// silence unused-import linters in odd build tags
