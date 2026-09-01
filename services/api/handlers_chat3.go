package main

// Messaging polish: group invite links, slow mode, forum topics, per-user
// delete ("delete for me"), cross-device draft sync, and server-side link
// preview resolution (own resolver, no third-party API).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ---- invite links ----

func (a *App) convRole(ctx context.Context, convID, uid string) string {
	var role string
	_ = a.db.QueryRow(ctx,
		`SELECT role FROM conversation_members WHERE conversation_id=$1 AND user_id=$2`,
		convID, uid).Scan(&role)
	return role
}

func (a *App) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	role := a.convRole(r.Context(), convID, uid)
	if role == "" {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var req struct {
		MaxUses   int  `json:"max_uses"`
		ExpiresIn *int `json:"expires_in_seconds"`
	}
	if r.Body != nil && r.ContentLength > 0 && !decodeJSON(w, r, &req) {
		return
	}
	if req.MaxUses < 0 || req.MaxUses > 100000 {
		writeErr(w, http.StatusBadRequest, "invalid max_uses")
		return
	}
	code, err := randomToken(9)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "invite failed")
		return
	}
	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(*req.ExpiresIn) * time.Second)
		expiresAt = &t
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO conversation_invites (conversation_id, code, created_by, max_uses, expires_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`, convID, code, uid, req.MaxUses, expiresAt).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "invite failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "code": code})
}

func (a *App) handleListInvites(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	role := a.convRole(r.Context(), convID, userIDFrom(r))
	if role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "owner or admin required")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT id, code, max_uses, use_count, expires_at, created_at
		 FROM conversation_invites WHERE conversation_id=$1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`, convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load invites")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, code string
		var maxUses, useCount int
		var expiresAt *time.Time
		var createdAt time.Time
		if err := rows.Scan(&id, &code, &maxUses, &useCount, &expiresAt, &createdAt); err == nil {
			out = append(out, map[string]any{
				"id": id, "code": code, "max_uses": maxUses, "use_count": useCount,
				"expires_at": expiresAt, "created_at": createdAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"invites": out})
}

func (a *App) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	role := a.convRole(r.Context(), convID, userIDFrom(r))
	if role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "owner or admin required")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE conversation_invites SET revoked_at=now() WHERE id=$1 AND conversation_id=$2`,
		r.PathValue("inviteId"), convID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (a *App) handleJoinByInvite(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	uid := userIDFrom(r)
	var inviteID, convID string
	var maxUses, useCount int
	var expiresAt, revokedAt *time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT id, conversation_id, max_uses, use_count, expires_at, revoked_at
		 FROM conversation_invites WHERE code=$1`, code).
		Scan(&inviteID, &convID, &maxUses, &useCount, &expiresAt, &revokedAt)
	if err != nil || revokedAt != nil || (expiresAt != nil && expiresAt.Before(time.Now())) ||
		(maxUses > 0 && useCount >= maxUses) {
		writeErr(w, http.StatusNotFound, "invite invalid or expired")
		return
	}
	if a.isMember(r.Context(), convID, uid) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_member", "conversation_id": convID})
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "join failed")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`UPDATE conversation_invites SET use_count = use_count + 1
		 WHERE id=$1 AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > now())
		   AND (max_uses = 0 OR use_count < max_uses)`, inviteID); err != nil {
		writeErr(w, http.StatusGone, "invite exhausted")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO conversation_members (conversation_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		convID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "join failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "join failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "joined", "conversation_id": convID})
}

// ---- slow mode ----

func (a *App) handleSetSlowMode(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	role := a.convRole(r.Context(), convID, userIDFrom(r))
	if role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "owner or admin required")
		return
	}
	var req struct {
		Seconds int `json:"seconds"` // 0 disables
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Seconds < 0 || req.Seconds > 3600 {
		writeErr(w, http.StatusBadRequest, "slow mode must be 0-3600 seconds")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE conversations SET slow_mode_seconds=$2 WHERE id=$1`, convID, req.Seconds); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "slow_mode", "conversation_id": convID, "seconds": req.Seconds,
	})
	a.fanoutConv(r.Context(), convID, payload)
	writeJSON(w, http.StatusOK, map[string]int{"slow_mode_seconds": req.Seconds})
}

// ---- forum topics ----

func (a *App) handleCreateTopic(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	if a.convRole(r.Context(), convID, uid) == "" {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Title) < 1 || len(req.Title) > 100 {
		writeErr(w, http.StatusBadRequest, "topic title must be 1-100 characters")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO conversation_topics (conversation_id, title, created_by) VALUES ($1,$2,$3) RETURNING id`,
		convID, req.Title, uid).Scan(&id); err != nil {
		writeErr(w, http.StatusConflict, "topic already exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "title": req.Title})
}

func (a *App) handleListTopics(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	if a.convRole(r.Context(), convID, userIDFrom(r)) == "" {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT t.id, t.title, u.username, t.created_at,
		        (SELECT COUNT(*) FROM messages m WHERE m.topic_id=t.id AND m.deleted_at IS NULL)
		 FROM conversation_topics t JOIN users u ON u.id=t.created_by
		 WHERE t.conversation_id=$1 ORDER BY t.created_at`, convID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load topics")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, uname string
		var createdAt time.Time
		var count int64
		if err := rows.Scan(&id, &title, &uname, &createdAt, &count); err == nil {
			out = append(out, map[string]any{
				"id": id, "title": title, "created_by": uname, "created_at": createdAt, "message_count": count,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": out})
}

// ---- per-user delete ("delete for me") ----

func (a *App) handleHideMessageForMe(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, _, ok := a.messageConv(r.Context(), msgID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO message_hidden (message_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		msgID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "hide failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "hidden"})
}

// handleUnhideMessageForMe reverses a per-user deletion ("delete for me" undo).
func (a *App) handleUnhideMessageForMe(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, _, ok := a.messageConv(r.Context(), msgID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`DELETE FROM message_hidden WHERE message_id=$1 AND user_id=$2`,
		msgID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "unhide failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restored"})
}

// ---- cross-device drafts ----

func (a *App) handleSaveDraft(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Body) > 8192 {
		writeErr(w, http.StatusBadRequest, "draft too long")
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		_, _ = a.db.Exec(r.Context(),
			`DELETE FROM conversation_drafts WHERE conversation_id=$1 AND user_id=$2`, convID, uid)
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO conversation_drafts (conversation_id, user_id, body, updated_at)
		 VALUES ($1,$2,$3,now())
		 ON CONFLICT (conversation_id, user_id) DO UPDATE SET body=$3, updated_at=now()`,
		convID, uid, req.Body); err != nil {
		writeErr(w, http.StatusInternalServerError, "draft save failed")
		return
	}
	// Sync to the user's other devices in realtime.
	payload, _ := json.Marshal(map[string]any{
		"type": "draft", "conversation_id": convID, "body": req.Body, "updated_at": time.Now(),
	})
	a.hub.sendTo(uid, payload)
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (a *App) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	uid := userIDFrom(r)
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	var body string
	var updatedAt time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT body, updated_at FROM conversation_drafts WHERE conversation_id=$1 AND user_id=$2`,
		convID, uid).Scan(&body, &updatedAt)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"body": "", "updated_at": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"body": body, "updated_at": updatedAt})
}

// ---- link previews (own resolver) ----

var (
	ogTitleRe  = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']{1,300})["']`)
	ogDescRe   = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:description["'][^>]+content=["']([^"']{1,500})["']`)
	ogImageRe  = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']{1,1000})["']`)
	titleTagRe = regexp.MustCompile(`(?i)<title[^>]*>([^<]{1,300})</title>`)
)

// handleLinkPreview fetches a URL server-side and extracts OpenGraph/title
// metadata. SSRF-guarded: http(s) only, redirects capped, body capped.
func (a *App) handleLinkPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		writeErr(w, http.StatusBadRequest, "invalid url")
		return
	}
	if a.cfg.AppEnv == "production" && u.Scheme != "https" {
		writeErr(w, http.StatusBadRequest, "https only")
		return
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "169.254.") || strings.HasSuffix(host, ".internal") {
		writeErr(w, http.StatusBadRequest, "private addresses are not allowed")
		return
	}
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	hreq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid url")
		return
	}
	hreq.Header.Set("User-Agent", "ChatApp-LinkPreview/1.0")
	hreq.Header.Set("Accept", "text/html")
	resp, err := client.Do(hreq)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not fetch url")
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		writeErr(w, http.StatusBadRequest, "url is not a web page")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not read page")
		return
	}
	html := string(body)
	pick := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(html); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
		return ""
	}
	title := pick(ogTitleRe)
	if title == "" {
		title = pick(titleTagRe)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"url": u.String(), "title": title, "description": pick(ogDescRe), "image": pick(ogImageRe),
	})
}
