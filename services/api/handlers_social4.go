package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Social parity pack: multi-type post reactions, pinned profile post, post
// edit history, scheduled posts, comment sorting, and photo albums.

var validReactions = map[string]bool{
	"like": true, "love": true, "haha": true,
	"wow": true, "sad": true, "angry": true,
}

// PUT /api/posts/{id}/react — set or change the caller's reaction. A 'like'
// reaction is mirrored into the legacy likes table; posts.like_count always
// tracks the total reaction count.
func (a *App) handleReactPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Reaction string `json:"reaction"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Reaction = strings.ToLower(strings.TrimSpace(req.Reaction))
	if !validReactions[req.Reaction] {
		writeErr(w, http.StatusBadRequest, "reaction must be like, love, haha, wow, sad or angry")
		return
	}
	uid, postID := userIDFrom(r), r.PathValue("id")

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	defer tx.Rollback(r.Context())

	var postOK bool
	if err := tx.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND deleted_at IS NULL)`,
		postID).Scan(&postOK); err != nil || !postOK {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	var prev string
	_ = tx.QueryRow(r.Context(),
		`SELECT reaction FROM post_reactions WHERE user_id=$1 AND post_id=$2`,
		uid, postID).Scan(&prev)
	if prev == req.Reaction {
		tx.Rollback(r.Context())
		writeJSON(w, http.StatusOK, map[string]string{"status": "reacted", "reaction": prev})
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO post_reactions (user_id, post_id, reaction) VALUES ($1,$2,$3)
		 ON CONFLICT (user_id, post_id) DO UPDATE SET reaction=$3, created_at=now()`,
		uid, postID, req.Reaction); err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	if prev == "" {
		if _, err := tx.Exec(r.Context(),
			`UPDATE posts SET like_count = like_count + 1 WHERE id=$1`, postID); err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction failed")
			return
		}
	}
	// Keep the legacy likes table in sync for the plain 'like' reaction.
	if req.Reaction == "like" {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO likes (user_id, post_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			uid, postID); err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction failed")
			return
		}
	} else if prev == "like" {
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM likes WHERE user_id=$1 AND post_id=$2`, uid, postID); err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction failed")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reacted", "reaction": req.Reaction})
}

// DELETE /api/posts/{id}/react — remove the caller's reaction.
func (a *App) handleUnreactPost(w http.ResponseWriter, r *http.Request) {
	uid, postID := userIDFrom(r), r.PathValue("id")

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	defer tx.Rollback(r.Context())

	var prev string
	err = tx.QueryRow(r.Context(),
		`DELETE FROM post_reactions WHERE user_id=$1 AND post_id=$2 RETURNING reaction`,
		uid, postID).Scan(&prev)
	if err == pgx.ErrNoRows {
		tx.Rollback(r.Context())
		writeJSON(w, http.StatusOK, map[string]string{"status": "unreacted"})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	if prev == "like" {
		if _, err := tx.Exec(r.Context(),
			`DELETE FROM likes WHERE user_id=$1 AND post_id=$2`, uid, postID); err != nil {
			writeErr(w, http.StatusInternalServerError, "reaction failed")
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE posts SET like_count = GREATEST(like_count - 1, 0) WHERE id=$1`, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "reaction failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unreacted"})
}

// GET /api/posts/{id}/reactions — counts per reaction type plus the caller's.
func (a *App) handlePostReactions(w http.ResponseWriter, r *http.Request) {
	postID := r.PathValue("id")
	rows, err := a.db.Query(r.Context(),
		`SELECT reaction, count(*) FROM post_reactions WHERE post_id=$1 GROUP BY reaction`, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load reactions")
		return
	}
	counts := map[string]int64{}
	total := int64(0)
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err == nil {
			counts[k] = v
			total += v
		}
	}
	rows.Close()
	var mine string
	_ = a.db.QueryRow(r.Context(),
		`SELECT reaction FROM post_reactions WHERE user_id=$1 AND post_id=$2`,
		userIDFrom(r), postID).Scan(&mine)
	writeJSON(w, http.StatusOK, map[string]any{
		"reactions": counts, "total": total, "my_reaction": mine,
	})
}

// ---- Pinned post on profile ----

// PUT /api/me/pinned-post — pin one of the caller's own posts to the profile.
func (a *App) handlePinPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PostID string `json:"post_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	var owns bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND author_id=$2 AND deleted_at IS NULL)`,
		req.PostID, uid).Scan(&owns)
	if !owns {
		writeErr(w, http.StatusNotFound, "post not found or not yours")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET pinned_post_id=$2 WHERE id=$1`, uid, req.PostID); err != nil {
		writeErr(w, http.StatusInternalServerError, "pin failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "pinned"})
}

// DELETE /api/me/pinned-post — unpin.
func (a *App) handleUnpinPost(w http.ResponseWriter, r *http.Request) {
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET pinned_post_id=NULL WHERE id=$1`, userIDFrom(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, "unpin failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unpinned"})
}

// ---- Post edit history ----

// GET /api/posts/{id}/edits — previous bodies, newest first.
func (a *App) handlePostEditHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT old_body, edited_at FROM post_edits WHERE post_id=$1 ORDER BY edited_at DESC`,
		r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	defer rows.Close()
	type edit struct {
		OldBody  string    `json:"old_body"`
		EditedAt time.Time `json:"edited_at"`
	}
	out := []edit{}
	for rows.Next() {
		var e edit
		if err := rows.Scan(&e.OldBody, &e.EditedAt); err == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"edits": out})
}

// ---- Scheduled posts ----

// GET /api/me/scheduled-posts — the caller's own unpublished scheduled posts.
func (a *App) handleScheduledPosts(w http.ResponseWriter, r *http.Request) {
	posts, err := a.scanPosts(r.Context(), postSelect+`
		WHERE p.deleted_at IS NULL AND p.author_id = $1 AND p.publish_at > now()
		ORDER BY p.publish_at ASC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load scheduled posts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// DELETE /api/scheduled-posts/{id} — cancel a scheduled post before it goes live.
func (a *App) handleCancelScheduledPost(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE posts SET deleted_at = now()
		 WHERE id=$1 AND author_id=$2 AND publish_at > now() AND deleted_at IS NULL`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "scheduled post not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- Photo albums ----

type albumJSON struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	ItemCount   int       `json:"item_count"`
	CoverURL    string    `json:"cover_url"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a *App) scanAlbums(ctx context.Context, w http.ResponseWriter, query string, args ...any) {
	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load albums")
		return
	}
	defer rows.Close()
	out := []albumJSON{}
	for rows.Next() {
		var al albumJSON
		if err := rows.Scan(&al.ID, &al.Title, &al.Description, &al.ItemCount,
			&al.CoverURL, &al.CreatedAt); err == nil {
			out = append(out, al)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"albums": out})
}

const albumSelect = `SELECT a.id, a.title, a.description,
       (SELECT count(*) FROM album_items i WHERE i.album_id = a.id),
       COALESCE((SELECT m.url FROM album_items i
                 JOIN post_media m ON m.post_id = i.post_id
                 WHERE i.album_id = a.id ORDER BY i.position, m.position LIMIT 1), ''),
       a.created_at
 FROM albums a `

// POST /api/albums — create an album.
func (a *App) handleCreateAlbum(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Title) < 1 || len(req.Title) > 100 || len(req.Description) > 500 {
		writeErr(w, http.StatusBadRequest, "title 1-100 chars, description up to 500")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO albums (owner_id, title, description) VALUES ($1,$2,$3) RETURNING id`,
		userIDFrom(r), req.Title, strings.TrimSpace(req.Description)).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create album")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GET /api/albums — the caller's albums.
func (a *App) handleMyAlbums(w http.ResponseWriter, r *http.Request) {
	a.scanAlbums(r.Context(), w, albumSelect+`WHERE a.owner_id=$1 ORDER BY a.created_at DESC`, userIDFrom(r))
}

// GET /api/users/{id}/albums — a user's albums.
func (a *App) handleUserAlbums(w http.ResponseWriter, r *http.Request) {
	a.scanAlbums(r.Context(), w, albumSelect+`WHERE a.owner_id=$1 ORDER BY a.created_at DESC`, r.PathValue("id"))
}

// GET /api/albums/{id} — album detail with its posts.
func (a *App) handleGetAlbum(w http.ResponseWriter, r *http.Request) {
	albumID := r.PathValue("id")
	var al albumJSON
	err := a.db.QueryRow(r.Context(), albumSelect+`WHERE a.id=$1`, albumID).
		Scan(&al.ID, &al.Title, &al.Description, &al.ItemCount, &al.CoverURL, &al.CreatedAt)
	if err != nil {
		writeErr(w, http.StatusNotFound, "album not found")
		return
	}
	posts, err := a.scanPosts(r.Context(), postSelect+`
		JOIN album_items i ON i.post_id = p.id
		WHERE i.album_id = $2 AND p.deleted_at IS NULL
		ORDER BY i.position, p.created_at`, userIDFrom(r), albumID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load album")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"album": al, "posts": posts})
}

// POST /api/albums/{id}/items — add one of the caller's own media posts.
func (a *App) handleAlbumAddItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PostID string `json:"post_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, albumID := userIDFrom(r), r.PathValue("id")
	var ownsAlbum bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM albums WHERE id=$1 AND owner_id=$2)`,
		albumID, uid).Scan(&ownsAlbum)
	if !ownsAlbum {
		writeErr(w, http.StatusNotFound, "album not found")
		return
	}
	var postOK bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts p
		 WHERE p.id=$1 AND p.author_id=$2 AND p.deleted_at IS NULL
		   AND EXISTS(SELECT 1 FROM post_media m WHERE m.post_id=p.id))`,
		req.PostID, uid).Scan(&postOK)
	if !postOK {
		writeErr(w, http.StatusBadRequest, "post not found, not yours, or has no media")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO album_items (album_id, post_id, position)
		 VALUES ($1,$2, COALESCE((SELECT MAX(position)+1 FROM album_items WHERE album_id=$1), 0))
		 ON CONFLICT DO NOTHING`, albumID, req.PostID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add item")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

// DELETE /api/albums/{id}/items/{postId} — remove an item.
func (a *App) handleAlbumRemoveItem(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM album_items i USING albums a
		 WHERE i.album_id=a.id AND a.owner_id=$1 AND i.album_id=$2 AND i.post_id=$3`,
		userIDFrom(r), r.PathValue("id"), r.PathValue("postId"))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// DELETE /api/albums/{id} — delete the album (posts themselves are kept).
func (a *App) handleDeleteAlbum(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM albums WHERE id=$1 AND owner_id=$2`, r.PathValue("id"), userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "album not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
