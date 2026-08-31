package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Gap pack 7 (migration 022): advanced search operators (X), curated Moments
// (X), audio-room recording & replay (X Spaces), live-room replay, chunked
// resumable uploads (Telegram 2GB), E2E verification fingerprints (Telegram),
// related reels via text embeddings (TikTok), and username profile links.

// ---------- Advanced post search with operators (X parity) ----------
//
// Supported operators: from:<username>, since:YYYY-MM-DD, until:YYYY-MM-DD,
// filter:reels|media|links|posts, has:media|link. Everything else is matched
// as case-insensitive terms against the post body (all terms must match).

var searchOpRe = regexp.MustCompile(`^(from|since|until|filter|has):(\S+)$`)

func (a *App) handleSearchPosts(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	limit, offset := pageParams(r)
	raw := strings.TrimSpace(r.URL.Query().Get("q"))
	if raw == "" {
		writeJSON(w, http.StatusOK, map[string]any{"posts": []any{}})
		return
	}
	var terms []string
	var negTerms []string
	var fromUser, negFromUser, since, until, filter, negFilter string
	var hasMedia, hasLink, negMedia, negLink bool
	for _, tok := range strings.Fields(raw) {
		negated := strings.HasPrefix(tok, "-")
		if negated {
			tok = strings.TrimPrefix(tok, "-")
			if tok == "" {
				continue
			}
		}
		if m := searchOpRe.FindStringSubmatch(tok); m != nil {
			switch strings.ToLower(m[1]) {
			case "from":
				if negated {
					negFromUser = strings.ToLower(strings.TrimPrefix(m[2], "@"))
				} else {
					fromUser = strings.ToLower(strings.TrimPrefix(m[2], "@"))
				}
			case "since":
				if !negated {
					since = m[2]
				}
			case "until":
				if !negated {
					until = m[2]
				}
			case "filter":
				if negated {
					negFilter = strings.ToLower(m[2])
				} else {
					filter = strings.ToLower(m[2])
				}
			case "has":
				switch strings.ToLower(m[2]) {
				case "media":
					if negated {
						negMedia = true
					} else {
						hasMedia = true
					}
				case "link":
					if negated {
						negLink = true
					} else {
						hasLink = true
					}
				}
			}
			continue
		}
		if len(tok) <= 100 {
			if negated {
				negTerms = append(negTerms, tok)
			} else {
				terms = append(terms, tok)
			}
		}
	}
	var b strings.Builder
	b.WriteString(postSelect + `
		WHERE p.deleted_at IS NULL AND p.type <> 'story'
		  AND (p.publish_at IS NULL OR p.publish_at <= now())
		  AND (p.visibility = 'public' OR p.author_id = $1)
		  AND NOT EXISTS(SELECT 1 FROM user_blocks bl WHERE (bl.blocker_id=$1 AND bl.blocked_id=p.author_id)
		                                             OR (bl.blocker_id=p.author_id AND bl.blocked_id=$1))
		  AND NOT EXISTS(SELECT 1 FROM user_mutes mu WHERE mu.user_id=$1 AND mu.muted_id=p.author_id)
		  AND NOT EXISTS(SELECT 1 FROM word_filters wf WHERE wf.user_id=$1 AND p.body ILIKE '%'||wf.phrase||'%')`)
	args := []any{uid}
	next := 2
	if fromUser != "" {
		fmt.Fprintf(&b, " AND u.username = $%d", next)
		args = append(args, fromUser)
		next++
	}
	if negFromUser != "" {
		fmt.Fprintf(&b, " AND u.username <> $%d", next)
		args = append(args, negFromUser)
		next++
	}
	if since != "" {
		fmt.Fprintf(&b, " AND p.created_at >= $%d::date", next)
		args = append(args, since)
		next++
	}
	if until != "" {
		fmt.Fprintf(&b, " AND p.created_at < ($%d::date + interval '1 day')", next)
		args = append(args, until)
		next++
	}
	switch filter {
	case "reels":
		b.WriteString(" AND p.type = 'reel'")
	case "media":
		b.WriteString(" AND EXISTS(SELECT 1 FROM post_media pm WHERE pm.post_id = p.id)")
	case "links":
		b.WriteString(" AND p.body ~* 'https?://'")
	case "posts":
		b.WriteString(" AND p.type = 'post'")
	}
	if hasMedia {
		b.WriteString(" AND EXISTS(SELECT 1 FROM post_media pm WHERE pm.post_id = p.id)")
	}
	if hasLink {
		b.WriteString(" AND p.body ~* 'https?://'")
	}
	switch negFilter {
	case "reels":
		b.WriteString(" AND p.type <> 'reel'")
	case "media":
		b.WriteString(" AND NOT EXISTS(SELECT 1 FROM post_media pm WHERE pm.post_id = p.id)")
	case "links":
		b.WriteString(" AND p.body !~* 'https?://'")
	case "posts":
		b.WriteString(" AND p.type <> 'post'")
	}
	if negMedia {
		b.WriteString(" AND NOT EXISTS(SELECT 1 FROM post_media pm WHERE pm.post_id = p.id)")
	}
	if negLink {
		b.WriteString(" AND p.body !~* 'https?://'")
	}
	for _, t := range terms {
		fmt.Fprintf(&b, " AND p.body ILIKE '%%'||$%d||'%%'", next)
		args = append(args, t)
		next++
	}
	for _, t := range negTerms {
		fmt.Fprintf(&b, " AND p.body NOT ILIKE '%%'||$%d||'%%'", next)
		args = append(args, t)
		next++
	}
	fmt.Fprintf(&b, " ORDER BY p.created_at DESC LIMIT $%d OFFSET $%d", next, next+1)
	args = append(args, limit, offset)
	posts, err := a.scanPosts(r.Context(), b.String(), args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// ---------- X Moments: curated collections ----------

// GET /api/moments — published moments, newest first.
func (a *App) handleListMoments(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT m.id, m.title, m.summary, m.cover_url, m.published_at,
		        (SELECT COUNT(*) FROM moment_items mi WHERE mi.moment_id = m.id),
		        COALESCE(u.display_name,''), COALESCE(u.username,'')
		 FROM moments m LEFT JOIN users u ON u.id = m.created_by
		 WHERE m.published_at IS NOT NULL
		 ORDER BY m.published_at DESC LIMIT 50`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load moments")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, summary, cover, name, uname string
		var published time.Time
		var items int
		if err := rows.Scan(&id, &title, &summary, &cover, &published, &items, &name, &uname); err == nil {
			out = append(out, map[string]any{
				"id": id, "title": title, "summary": summary, "cover_url": cover,
				"published_at": published, "item_count": items,
				"curator_name": name, "curator_username": uname,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"moments": out})
}

// GET /api/moments/{id} — a published moment with its posts.
func (a *App) handleGetMoment(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	momentID := r.PathValue("id")
	var title, summary, cover string
	var published time.Time
	if err := a.db.QueryRow(r.Context(),
		`SELECT title, summary, cover_url, published_at FROM moments
		 WHERE id=$1 AND published_at IS NOT NULL`, momentID).
		Scan(&title, &summary, &cover, &published); err != nil {
		writeErr(w, http.StatusNotFound, "moment not found")
		return
	}
	posts, err := a.scanPosts(r.Context(), postSelect+`
		WHERE p.id IN (SELECT mi.post_id FROM moment_items mi WHERE mi.moment_id = $2)
		  AND p.deleted_at IS NULL
		ORDER BY (SELECT mi.position FROM moment_items mi
		          WHERE mi.moment_id = $2 AND mi.post_id = p.id)`, uid, momentID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load moment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"moment": map[string]any{"id": momentID, "title": title, "summary": summary,
			"cover_url": cover, "published_at": published},
		"posts": posts,
	})
}

// GET /api/admin/moments — all moments including drafts (curation queue).
func (a *App) handleAdminListMoments(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT m.id, m.title, m.summary, m.cover_url, m.published_at, m.created_at,
		        (SELECT COUNT(*) FROM moment_items mi WHERE mi.moment_id = m.id)
		 FROM moments m ORDER BY m.created_at DESC LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load moments")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, summary, cover string
		var published *time.Time
		var created time.Time
		var items int
		if err := rows.Scan(&id, &title, &summary, &cover, &published, &created, &items); err == nil {
			out = append(out, map[string]any{
				"id": id, "title": title, "summary": summary, "cover_url": cover,
				"published_at": published, "created_at": created, "item_count": items,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"moments": out})
}

// POST /api/admin/moments — create a draft moment (curator = the admin user).
func (a *App) handleAdminCreateMoment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Cover   string `json:"cover_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 200 || len(req.Summary) > 2000 {
		writeErr(w, http.StatusBadRequest, "title required (200 chars max), summary 2000 max")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO moments (title, summary, cover_url, created_by) VALUES ($1,$2,$3,$4) RETURNING id`,
		req.Title, strings.TrimSpace(req.Summary), strings.TrimSpace(req.Cover), userIDFrom(r)).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create moment")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "moment.create", id, map[string]any{"title": req.Title})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// POST /api/admin/moments/{id}/items — add a post to a moment.
func (a *App) handleAdminMomentAddItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PostID   string `json:"post_id"`
		Position int    `json:"position"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	momentID := r.PathValue("id")
	var exists bool
	_ = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND deleted_at IS NULL)`,
		req.PostID).Scan(&exists)
	if !exists {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO moment_items (moment_id, post_id, position) VALUES ($1,$2,$3)
		 ON CONFLICT (moment_id, post_id) DO UPDATE SET position=EXCLUDED.position`,
		momentID, req.PostID, req.Position); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add item")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "moment.add_item", momentID, map[string]any{"post_id": req.PostID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// DELETE /api/admin/moments/{id}/items/{postId}
func (a *App) handleAdminMomentRemoveItem(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM moment_items WHERE moment_id=$1 AND post_id=$2`,
		r.PathValue("id"), r.PathValue("postId"))
	a.audit(r.Context(), userIDFrom(r), "moment.remove_item", r.PathValue("id"), map[string]any{"post_id": r.PathValue("postId")})
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// POST /api/admin/moments/{id}/publish — publish (or unpublish with {"publish":false}).
func (a *App) handleAdminMomentPublish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Publish *bool `json:"publish"`
	}
	_ = decodeJSON(w, r, &req)
	publish := req.Publish == nil || *req.Publish
	momentID := r.PathValue("id")
	if publish {
		var items int
		_ = a.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM moment_items WHERE moment_id=$1`, momentID).Scan(&items)
		if items == 0 {
			writeErr(w, http.StatusBadRequest, "cannot publish an empty moment")
			return
		}
	}
	var err error
	var affected int64
	if publish {
		var tag interface{ RowsAffected() int64 }
		tag, err = a.db.Exec(r.Context(),
			`UPDATE moments SET published_at=now() WHERE id=$1`, momentID)
		if tag != nil {
			affected = tag.RowsAffected()
		}
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, err = a.db.Exec(r.Context(),
			`UPDATE moments SET published_at=NULL WHERE id=$1`, momentID)
		if tag != nil {
			affected = tag.RowsAffected()
		}
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "publish failed")
		return
	}
	if affected == 0 {
		writeErr(w, http.StatusNotFound, "moment not found")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "moment.publish", momentID, map[string]any{"publish": publish})
	writeJSON(w, http.StatusOK, map[string]any{"status": map[bool]string{true: "published", false: "unpublished"}[publish]})
}

// DELETE /api/admin/moments/{id}
func (a *App) handleAdminDeleteMoment(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(), `DELETE FROM moments WHERE id=$1`, r.PathValue("id"))
	a.audit(r.Context(), userIDFrom(r), "moment.delete", r.PathValue("id"), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- Audio room recording & replay (X Spaces) ----------

// POST /api/audio-rooms/{id}/recordings — host/cohost attaches a recording
// (uploaded through the normal media pipeline first).
func (a *App) handleSaveAudioRoomRecording(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaID   string `json:"media_id"`
		DurationS int    `json:"duration_s"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	roomID := r.PathValue("id")
	var allowed bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM audio_room_participants
		 WHERE room_id=$1 AND user_id=$2 AND role IN ('host','cohost'))`, roomID, uid).Scan(&allowed); err != nil || !allowed {
		writeErr(w, http.StatusForbidden, "only the host or a co-host can record this space")
		return
	}
	req.MediaID = strings.TrimSpace(req.MediaID)
	if req.MediaID == "" || len(req.MediaID) > 200 {
		writeErr(w, http.StatusBadRequest, "media_id required")
		return
	}
	if req.DurationS < 0 || req.DurationS > 24*3600 {
		writeErr(w, http.StatusBadRequest, "invalid duration")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO audio_room_recordings (room_id, media_id, duration_s, created_by)
		 VALUES ($1,$2,$3,$4) RETURNING id`, roomID, req.MediaID, req.DurationS, uid).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save recording")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/audio-rooms/{id}/recordings — replay list for a space.
func (a *App) handleListAudioRoomRecordings(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT rec.id, rec.media_id, rec.duration_s, rec.created_at, u.display_name, u.username
		 FROM audio_room_recordings rec JOIN users u ON u.id = rec.created_by
		 WHERE rec.room_id=$1 ORDER BY rec.created_at DESC LIMIT 50`, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load recordings")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, mediaID, name, uname string
		var dur int
		var created time.Time
		if err := rows.Scan(&id, &mediaID, &dur, &created, &name, &uname); err == nil {
			out = append(out, map[string]any{
				"id": id, "media_url": "/media/" + mediaID, "duration_s": dur,
				"created_at": created, "host_name": name, "host_username": uname,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"recordings": out})
}

// DELETE /api/audio-room-recordings/{id} — the recorder or a host removes it.
func (a *App) handleDeleteAudioRoomRecording(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM audio_room_recordings rec WHERE rec.id=$1 AND (
		   rec.created_by=$2 OR EXISTS(SELECT 1 FROM audio_room_participants ap
		     WHERE ap.room_id=rec.room_id AND ap.user_id=$2 AND ap.role IN ('host','cohost')))`,
		r.PathValue("id"), uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "recording not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- Live-room replay ----------

// PUT /api/live-rooms/{id}/replay — the host attaches the recorded media so
// latecomers can replay an ended live.
func (a *App) handleSetLiveRoomReplay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaID string `json:"media_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	roomID := r.PathValue("id")
	req.MediaID = strings.TrimSpace(req.MediaID)
	if req.MediaID == "" || len(req.MediaID) > 200 {
		writeErr(w, http.StatusBadRequest, "media_id required")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE live_rooms SET replay_media_id=$2 WHERE id=$1 AND host_id=$3`,
		roomID, req.MediaID, uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "live room not found or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "replay set"})
}

// ---------- Chunked resumable uploads (Telegram 2GB files) ----------

// POST /api/media/uploads — open an upload session. Bytes flow straight to
// the C++ media edge (/upload/init, /upload/{id}/chunk, /upload/{id}/complete)
// authorized by a Rust-signed grant bound to this session id.
func (a *App) handleCreateUploadSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Filename   string `json:"filename"`
		TotalBytes int64  `json:"total_bytes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Filename = strings.TrimSpace(req.Filename)
	if req.Filename == "" || len(req.Filename) > 128 || strings.Contains(req.Filename, "..") ||
		strings.ContainsAny(req.Filename, "/\\") {
		writeErr(w, http.StatusBadRequest, "invalid filename")
		return
	}
	if req.TotalBytes <= 0 || req.TotalBytes > (2<<30) {
		writeErr(w, http.StatusBadRequest, "total_bytes must be 1..2GiB")
		return
	}
	uid := userIDFrom(r)
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO upload_sessions (user_id, filename, total_bytes) VALUES ($1,$2,$3) RETURNING id`,
		uid, req.Filename, req.TotalBytes).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create upload session")
		return
	}
	t, err := a.signPayload(r.Context(), "/upload/"+id, 86400)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "upload signing unavailable")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"upload_id": id, "expires": t.Expires, "signature": t.Signature,
		"media_base": a.cfg.MediaServiceURL,
	})
}

// GET /api/media/uploads/{id} — session status for resume (owner only).
func (a *App) handleGetUploadSession(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var filename, status, mediaURL string
	var total, received int64
	var expires time.Time
	if err := a.db.QueryRow(r.Context(),
		`SELECT filename, total_bytes, received_bytes, status, media_url, expires_at
		 FROM upload_sessions WHERE id=$1 AND user_id=$2`, r.PathValue("id"), uid).
		Scan(&filename, &total, &received, &status, &mediaURL, &expires); err != nil {
		writeErr(w, http.StatusNotFound, "upload session not found")
		return
	}
	if status == "active" && time.Now().After(expires) {
		status = "expired"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"upload_id": r.PathValue("id"), "filename": filename, "total_bytes": total,
		"received_bytes": received, "status": status, "media_url": mediaURL,
	})
}

// POST /api/media/uploads/{id}/complete — record the finalized media URL
// after the edge confirmed byte integrity.
func (a *App) handleCompleteUploadSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaURL      string `json:"media_url"`
		ReceivedBytes int64  `json:"received_bytes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if !strings.HasPrefix(req.MediaURL, "/media/") || len(req.MediaURL) > 300 {
		writeErr(w, http.StatusBadRequest, "invalid media_url")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE upload_sessions SET status='completed', media_url=$3, received_bytes=$4
		 WHERE id=$1 AND user_id=$2 AND status='active' AND expires_at > now()
		   AND total_bytes = $4`,
		r.PathValue("id"), uid, req.MediaURL, req.ReceivedBytes)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusBadRequest, "session not active or byte count mismatch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "completed", "media_url": req.MediaURL})
}

// POST /api/media/uploads/{id}/abort
func (a *App) handleAbortUploadSession(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`UPDATE upload_sessions SET status='aborted' WHERE id=$1 AND user_id=$2 AND status='active'`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "abort failed")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "upload session not found or not active")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "aborted"})
}

// ---------- E2E verification fingerprint (Telegram key verification) ----------
//
// GET /api/e2e/verify/{userId} — the Signal/Telegram-style safety number for
// the caller and {userId}: both clients fetch it and compare out-of-band.
// Derived by the Rust security service; the identical algorithm is applied
// locally as a fallback so verification never depends on a network hop.

func e2eFingerprintLocal(keyA, keyB string) (string, string) {
	lo, hi := keyA, keyB
	if lo > hi {
		lo, hi = hi, lo
	}
	d1 := sha256.Sum256([]byte("ChatApp-SAS-v1|" + lo + "|" + hi))
	d2 := sha256.Sum256(d1[:])
	buf := append(d1[:], d2[:16]...)
	var sas strings.Builder
	for i := 0; i+4 <= len(buf); i += 4 {
		fmt.Fprintf(&sas, "%05d", binary.BigEndian.Uint32(buf[i:i+4])%100000)
	}
	return hex.EncodeToString(d1[:]), sas.String()
}

func (a *App) handleE2EVerify(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	other := r.PathValue("userId")
	if other == uid {
		writeErr(w, http.StatusBadRequest, "cannot verify against yourself")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT id, e2e_identity_key FROM users WHERE id = ANY($1) AND e2e_identity_key IS NOT NULL`,
		[]string{uid, other})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load keys")
		return
	}
	defer rows.Close()
	keys := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err == nil {
			keys[id] = key
		}
	}
	if keys[uid] == "" || keys[other] == "" {
		writeErr(w, http.StatusBadRequest, "both parties must publish E2E identity keys first")
		return
	}
	fingerprint, sas := e2eFingerprintLocal(keys[uid], keys[other])
	// Prefer the Rust security service when reachable (the audited trust
	// boundary); the local derivation above is bit-identical.
	if a.cfg.SecuritySvcURL != "" {
		reqBody, _ := json.Marshal(map[string]string{"key_a": keys[uid], "key_b": keys[other]})
		req, err := http.NewRequestWithContext(r.Context(), "POST",
			a.cfg.SecuritySvcURL+"/e2e/fingerprint", strings.NewReader(string(reqBody)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if resp, err := securityClient.Do(req); err == nil {
				var out struct {
					Fingerprint string `json:"fingerprint"`
					SAS         string `json:"sas"`
				}
				if json.NewDecoder(resp.Body).Decode(&out) == nil && out.Fingerprint != "" {
					fingerprint, sas = out.Fingerprint, out.SAS
				}
				resp.Body.Close()
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"fingerprint": fingerprint, "sas": sas, "user_id": other,
	})
}

// ---------- Related reels via text embeddings (TikTok) ----------

func (a *App) mlEmbed(ctx context.Context, text string) ([]float64, error) {
	if a.cfg.MLServiceURL == "" {
		return nil, fmt.Errorf("ml service not configured")
	}
	reqBody, _ := json.Marshal(map[string]string{"text": text})
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", a.cfg.MLServiceURL+"/embed",
		strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := securityClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Vector []float64 `json:"vector"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Vector) == 0 {
		return nil, fmt.Errorf("embed failed")
	}
	return out.Vector, nil
}

// ensureEmbedding returns the stored embedding or computes + persists it.
func (a *App) ensureEmbedding(ctx context.Context, postID, body string) ([]float64, error) {
	var stored []float32
	err := a.db.QueryRow(ctx, `SELECT embedding FROM posts WHERE id=$1 AND embedding IS NOT NULL`, postID).Scan(&stored)
	if err == nil && len(stored) > 0 {
		out := make([]float64, len(stored))
		for i, v := range stored {
			out[i] = float64(v)
		}
		return out, nil
	}
	vec, err := a.mlEmbed(ctx, body)
	if err != nil {
		return nil, err
	}
	f32 := make([]float32, len(vec))
	for i, v := range vec {
		f32[i] = float32(v)
	}
	_, _ = a.db.Exec(ctx, `UPDATE posts SET embedding=$2 WHERE id=$1`, postID, f32)
	return vec, nil
}

func cosineSim(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// GET /api/reels/{id}/related — reels ranked by embedding cosine similarity
// to the given reel (TikTok "related videos").
func (a *App) handleRelatedReels(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	reelID := r.PathValue("id")
	var body string
	var vis string
	if err := a.db.QueryRow(r.Context(),
		`SELECT body, visibility FROM posts WHERE id=$1 AND type='reel' AND deleted_at IS NULL`,
		reelID).Scan(&body, &vis); err != nil {
		writeErr(w, http.StatusNotFound, "reel not found")
		return
	}
	target, err := a.ensureEmbedding(r.Context(), reelID, body)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "embedding service unavailable")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.body FROM posts p
		 WHERE p.type='reel' AND p.deleted_at IS NULL AND p.visibility='public' AND p.id <> $2
		   AND p.author_id <> $1
		   AND NOT EXISTS(SELECT 1 FROM user_blocks b WHERE (b.blocker_id=$1 AND b.blocked_id=p.author_id)
		                                              OR (b.blocker_id=p.author_id AND b.blocked_id=$1))
		   AND NOT EXISTS(SELECT 1 FROM user_mutes m WHERE m.user_id=$1 AND m.muted_id=p.author_id)
		 ORDER BY p.created_at DESC LIMIT 120`, uid, reelID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load candidates")
		return
	}
	type cand struct {
		id   string
		body string
		sim  float64
	}
	cands := []cand{}
	ids := []string{}
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.id, &c.body); err == nil {
			cands = append(cands, c)
		}
	}
	rows.Close()
	for i := range cands {
		vec, err := a.ensureEmbedding(r.Context(), cands[i].id, cands[i].body)
		if err != nil {
			continue
		}
		cands[i].sim = cosineSim(target, vec)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].sim > cands[j].sim })
	if len(cands) > 10 {
		cands = cands[:10]
	}
	for _, c := range cands {
		if c.sim > 0.02 {
			ids = append(ids, c.id)
		}
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"reels": []any{}})
		return
	}
	posts, err := a.scanPosts(r.Context(), postSelect+` WHERE p.id = ANY($2)`, uid, ids)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load reels")
		return
	}
	order := map[string]int{}
	for i, id := range ids {
		order[id] = i
	}
	sort.Slice(posts, func(i, j int) bool { return order[posts[i].ID] < order[posts[j].ID] })
	writeJSON(w, http.StatusOK, map[string]any{"reels": posts})
}

// ---------- Username profile links (t.me/<username> parity) ----------

// GET /api/users/by-username/{username} — public profile by @username; the
// web client routes /u/<username> here.
func (a *App) handleGetUserByUsername(w http.ResponseWriter, r *http.Request) {
	username := strings.ToLower(strings.TrimPrefix(r.PathValue("username"), "@"))
	var id string
	if err := a.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE username=$1 AND status='active'`, username).Scan(&id); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	u, err := a.getUser(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	a.recordProfileView(r.Context(), id, userIDFrom(r))
	var followers, following int
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM follows WHERE followee_id=$1`, id).Scan(&followers)
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM follows WHERE follower_id=$1`, id).Scan(&following)
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "followers": followers, "following": following})
}
