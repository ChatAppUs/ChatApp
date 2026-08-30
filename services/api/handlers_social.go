package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type mediaIn struct {
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	ThumbURL  string `json:"thumb_url"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	DurationS int    `json:"duration_s"`
}

type postOut struct {
	ID           string      `json:"id"`
	AuthorID     string      `json:"author_id"`
	AuthorName   string      `json:"author_name"`
	AuthorUser   string      `json:"author_username"`
	AuthorAvatar string      `json:"author_avatar"`
	Type         string      `json:"type"`
	Body         string      `json:"body"`
	Visibility   string      `json:"visibility"`
	LikeCount    int         `json:"like_count"`
	CommentCount int         `json:"comment_count"`
	ShareCount   int         `json:"share_count"`
	ViewCount    int64       `json:"view_count"`
	LikedByMe    bool        `json:"liked_by_me"`
	Media        []mediaIn   `json:"media"`
	CreatedAt    time.Time   `json:"created_at"`
	RepostOf     string      `json:"repost_of"`
	ThreadParent string      `json:"thread_parent_id"`
	EditedAt     *time.Time  `json:"edited_at"`
	Quoted       *quotedPost `json:"quoted,omitempty"`
}

type quotedPost struct {
	ID         string `json:"id"`
	AuthorName string `json:"author_name"`
	AuthorUser string `json:"author_username"`
	Body       string `json:"body"`
}

func pageParams(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return
}

func (a *App) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type         string    `json:"type"` // post | reel | story
		Body         string    `json:"body"`
		Visibility   string    `json:"visibility"`
		Media        []mediaIn `json:"media"`
		PollOptions  []string  `json:"poll_options"` // 2-4 options turns the post into a poll
		RepostOf     string    `json:"repost_of"`
		ThreadParent string    `json:"thread_parent_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Type == "" {
		req.Type = "post"
	}
	if req.Type != "post" && req.Type != "reel" && req.Type != "story" {
		writeErr(w, http.StatusBadRequest, "type must be post, reel or story")
		return
	}
	if req.Visibility == "" {
		req.Visibility = "public"
	}
	if req.Visibility != "public" && req.Visibility != "followers" && req.Visibility != "private" && req.Visibility != "close_friends" {
		writeErr(w, http.StatusBadRequest, "invalid visibility")
		return
	}
	if strings.TrimSpace(req.Body) == "" && len(req.Media) == 0 && len(req.PollOptions) == 0 {
		writeErr(w, http.StatusBadRequest, "post must have text, media or a poll")
		return
	}
	if len(req.Media) > 10 {
		writeErr(w, http.StatusBadRequest, "max 10 media items")
		return
	}
	if len(req.PollOptions) > 0 && (len(req.PollOptions) < 2 || len(req.PollOptions) > 4) {
		writeErr(w, http.StatusBadRequest, "polls need 2-4 options")
		return
	}
	uid := userIDFrom(r)
	// ML moderation gate: the ML service owns the policy decision. Transport
	// failures are fail-open so moderation downtime never blocks posting.
	if decision, _ := a.mlModerate(req.Body); decision == "block" {
		writeErr(w, http.StatusUnprocessableEntity, "post violates content policy")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create post")
		return
	}
	defer tx.Rollback(r.Context())
	var postID string
	var expires *time.Time
	if req.Type == "story" {
		t := time.Now().Add(24 * time.Hour)
		expires = &t
	}
	err = tx.QueryRow(r.Context(),
		`INSERT INTO posts (author_id, type, body, visibility, expires_at, repost_of, thread_parent_id)
                 VALUES ($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,'')::uuid) RETURNING id`,
		uid, req.Type, req.Body, req.Visibility, expires, req.RepostOf, req.ThreadParent).Scan(&postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create post")
		return
	}
	for i, label := range req.PollOptions {
		if strings.TrimSpace(label) == "" {
			writeErr(w, http.StatusBadRequest, "poll options cannot be empty")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO poll_options (post_id, idx, label) VALUES ($1,$2,$3)`,
			postID, i, label); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to create poll")
			return
		}
	}
	for _, tag := range extractHashtags(req.Body) {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO hashtags (tag, use_count, last_used) VALUES ($1,1,now())
                         ON CONFLICT (tag) DO UPDATE SET use_count = hashtags.use_count + 1, last_used = now()`, tag); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to index hashtag")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO post_hashtags (post_id, tag) VALUES ($1,$2) ON CONFLICT DO NOTHING`, postID, tag); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to index hashtag")
			return
		}
	}
	for i, m := range req.Media {
		if m.Kind != "image" && m.Kind != "video" && m.Kind != "audio" {
			writeErr(w, http.StatusBadRequest, "media kind must be image, video or audio")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO post_media (post_id, kind, url, thumb_url, width, height, duration_s, position)
                         VALUES ($1,$2,$3,$4,NULLIF($5,0),NULLIF($6,0),NULLIF($7,0),$8)`,
			postID, m.Kind, m.URL, m.ThumbURL, m.Width, m.Height, m.DurationS, i); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to attach media")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create post")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": postID})
}

const postSelect = `
SELECT p.id, p.author_id, u.display_name, u.username, u.avatar_url, p.type, p.body, p.visibility,
       p.like_count, p.comment_count, p.share_count, p.view_count, p.created_at,
       COALESCE(p.repost_of::text,''), COALESCE(p.thread_parent_id::text,''), p.edited_at,
       EXISTS(SELECT 1 FROM likes l WHERE l.post_id = p.id AND l.user_id = $1)
FROM posts p JOIN users u ON u.id = p.author_id`

func (a *App) scanPosts(ctx context.Context, query string, args ...any) ([]postOut, error) {
	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []postOut{}
	var ids []any
	for rows.Next() {
		var p postOut
		if err := rows.Scan(&p.ID, &p.AuthorID, &p.AuthorName, &p.AuthorUser, &p.AuthorAvatar,
			&p.Type, &p.Body, &p.Visibility, &p.LikeCount, &p.CommentCount, &p.ShareCount,
			&p.ViewCount, &p.CreatedAt, &p.RepostOf, &p.ThreadParent, &p.EditedAt, &p.LikedByMe); err != nil {
			return nil, err
		}
		p.Media = []mediaIn{}
		out = append(out, p)
		ids = append(ids, p.ID)
	}
	if len(ids) == 0 {
		return out, nil
	}
	mrows, err := a.db.Query(ctx,
		`SELECT post_id, kind, url, thumb_url, COALESCE(width,0), COALESCE(height,0), COALESCE(duration_s,0)
                 FROM post_media WHERE post_id = ANY($1) ORDER BY position`, ids)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	byPost := map[string][]mediaIn{}
	for mrows.Next() {
		var pid string
		var m mediaIn
		if err := mrows.Scan(&pid, &m.Kind, &m.URL, &m.ThumbURL, &m.Width, &m.Height, &m.DurationS); err != nil {
			return nil, err
		}
		byPost[pid] = append(byPost[pid], m)
	}
	for i := range out {
		out[i].Media = byPost[out[i].ID]
	}
	var quoteIDs []any
	for _, p := range out {
		if p.RepostOf != "" {
			quoteIDs = append(quoteIDs, p.RepostOf)
		}
	}
	if len(quoteIDs) > 0 {
		qrows, err := a.db.Query(ctx,
			`SELECT q.id, qu.display_name, qu.username, q.body
                         FROM posts q JOIN users qu ON qu.id = q.author_id WHERE q.id = ANY($1)`, quoteIDs)
		if err == nil {
			defer qrows.Close()
			byID := map[string]*quotedPost{}
			for qrows.Next() {
				var q quotedPost
				if err := qrows.Scan(&q.ID, &q.AuthorName, &q.AuthorUser, &q.Body); err == nil {
					byID[q.ID] = &q
				}
			}
			for i := range out {
				if q, ok := byID[out[i].RepostOf]; ok {
					out[i].Quoted = q
				}
			}
		}
	}
	return out, nil
}

func (a *App) handleFeed(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	limit, offset := pageParams(r)
	posts, err := a.scanPosts(r.Context(), postSelect+`
                WHERE p.deleted_at IS NULL AND p.type = 'post'
                  AND (p.visibility = 'public'
                       OR p.author_id = $1
                       OR (p.visibility = 'followers' AND EXISTS(
                             SELECT 1 FROM follows f WHERE f.follower_id = $1 AND f.followee_id = p.author_id))
                       OR (p.visibility = 'close_friends' AND EXISTS(
                             SELECT 1 FROM close_friends cf WHERE cf.user_id = p.author_id AND cf.friend_id = $1)))
                  AND NOT EXISTS(SELECT 1 FROM user_mutes m WHERE m.user_id = $1 AND m.muted_id = p.author_id)
                  AND NOT EXISTS(SELECT 1 FROM restricted_list rl WHERE rl.user_id = p.author_id AND rl.restricted_id = $1)
                  AND NOT EXISTS(SELECT 1 FROM word_filters wf WHERE wf.user_id = $1 AND p.body ILIKE '%'||wf.phrase||'%')
                ORDER BY p.created_at DESC LIMIT $2 OFFSET $3`, uid, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load feed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

func (a *App) handleReels(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	limit, offset := pageParams(r)
	posts, err := a.scanPosts(r.Context(), postSelect+`
                WHERE p.deleted_at IS NULL AND p.type = 'reel' AND p.visibility = 'public'
                  AND NOT EXISTS(SELECT 1 FROM user_mutes m WHERE m.user_id = $1 AND m.muted_id = p.author_id)
                  AND NOT EXISTS(SELECT 1 FROM restricted_list rl WHERE rl.user_id = p.author_id AND rl.restricted_id = $1)
                  AND NOT EXISTS(SELECT 1 FROM word_filters wf WHERE wf.user_id = $1 AND p.body ILIKE '%'||wf.phrase||'%')
                ORDER BY p.created_at DESC LIMIT $2 OFFSET $3`, uid, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load reels")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reels": posts})
}

func (a *App) handleStories(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	posts, err := a.scanPosts(r.Context(), postSelect+`
                WHERE p.deleted_at IS NULL AND p.type = 'story'
                  AND (p.expires_at IS NULL OR p.expires_at > now())
                  AND (p.author_id = $1 OR EXISTS(
                        SELECT 1 FROM follows f WHERE f.follower_id = $1 AND f.followee_id = p.author_id)
                       OR (p.visibility = 'close_friends' AND EXISTS(
                             SELECT 1 FROM close_friends cf WHERE cf.user_id = p.author_id AND cf.friend_id = $1))
                       OR p.visibility = 'public')
                  AND NOT EXISTS(SELECT 1 FROM user_mutes m WHERE m.user_id = $1 AND m.muted_id = p.author_id)
                ORDER BY p.created_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load stories")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stories": posts})
}

func (a *App) handleUserPosts(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	target := r.PathValue("id")
	limit, offset := pageParams(r)
	posts, err := a.scanPosts(r.Context(), postSelect+`
                WHERE p.deleted_at IS NULL AND p.author_id = $2 AND p.type <> 'story'
                ORDER BY p.created_at DESC LIMIT $3 OFFSET $4`, uid, target, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load posts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

func (a *App) handleDeletePost(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE posts SET deleted_at = now() WHERE id = $1 AND author_id = $2 AND deleted_at IS NULL`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *App) handlePostView(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`UPDATE posts SET view_count = view_count + 1 WHERE id = $1 AND deleted_at IS NULL`,
		r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Likes ----

func (a *App) handleLike(w http.ResponseWriter, r *http.Request) {
	uid, postID := userIDFrom(r), r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "like failed")
		return
	}
	defer tx.Rollback(r.Context())
	res, err := tx.Exec(r.Context(),
		`INSERT INTO likes (user_id, post_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, postID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	if res.RowsAffected() > 0 {
		if _, err := tx.Exec(r.Context(),
			`UPDATE posts SET like_count = like_count + 1 WHERE id = $1`, postID); err != nil {
			writeErr(w, http.StatusInternalServerError, "like failed")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "like failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "liked"})
}

func (a *App) handleUnlike(w http.ResponseWriter, r *http.Request) {
	uid, postID := userIDFrom(r), r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unlike failed")
		return
	}
	defer tx.Rollback(r.Context())
	res, err := tx.Exec(r.Context(), `DELETE FROM likes WHERE user_id=$1 AND post_id=$2`, uid, postID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unlike failed")
		return
	}
	if res.RowsAffected() > 0 {
		_, _ = tx.Exec(r.Context(),
			`UPDATE posts SET like_count = GREATEST(like_count - 1, 0) WHERE id = $1`, postID)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "unlike failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unliked"})
}

// ---- Comments (with @mentions) ----

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_]{3,30})`)

func (a *App) handleAddComment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "comment body required")
		return
	}
	if len(req.Body) > 2000 {
		writeErr(w, http.StatusBadRequest, "comment too long")
		return
	}
	uid, postID := userIDFrom(r), r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "comment failed")
		return
	}
	defer tx.Rollback(r.Context())
	var commentID string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO comments (post_id, author_id, parent_id, body)
                 VALUES ($1,$2,NULLIF($3,'')::uuid,$4) RETURNING id`,
		postID, uid, req.ParentID, req.Body).Scan(&commentID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE posts SET comment_count = comment_count + 1 WHERE id = $1`, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, "comment failed")
		return
	}
	// resolve @mentions
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(req.Body, -1) {
		uname := strings.ToLower(m[1])
		if seen[uname] {
			continue
		}
		seen[uname] = true
		var mentionedID string
		if err := tx.QueryRow(r.Context(),
			`SELECT id FROM users WHERE lower(username) = $1 AND status='active'`, uname).Scan(&mentionedID); err != nil {
			continue
		}
		_, _ = tx.Exec(r.Context(),
			`INSERT INTO comment_mentions (comment_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			commentID, mentionedID)
		_, _ = tx.Exec(r.Context(),
			`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'mention',$2)`,
			mentionedID, map[string]string{"comment_id": commentID, "post_id": postID, "by": uid})
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "comment failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": commentID})
}

func (a *App) handleListComments(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id, c.author_id, u.display_name, u.username, u.avatar_url, c.body, c.created_at,
                 COALESCE(c.parent_id::text,''),
                 (SELECT count(*) FROM comment_likes cl WHERE cl.comment_id = c.id),
                 EXISTS(SELECT 1 FROM comment_likes cl WHERE cl.comment_id = c.id AND cl.user_id = $4)
                 FROM comments c JOIN users u ON u.id = c.author_id
                 WHERE c.post_id = $1 AND c.deleted_at IS NULL
                 ORDER BY c.created_at ASC LIMIT $2 OFFSET $3`,
		r.PathValue("id"), limit, offset, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load comments")
		return
	}
	defer rows.Close()
	type comment struct {
		ID        string    `json:"id"`
		AuthorID  string    `json:"author_id"`
		Name      string    `json:"author_name"`
		Username  string    `json:"author_username"`
		Avatar    string    `json:"author_avatar"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
		ParentID  string    `json:"parent_id"`
		LikeCount int       `json:"like_count"`
		LikedByMe bool      `json:"liked_by_me"`
	}
	out := []comment{}
	for rows.Next() {
		var c comment
		if err := rows.Scan(&c.ID, &c.AuthorID, &c.Name, &c.Username, &c.Avatar, &c.Body, &c.CreatedAt,
			&c.ParentID, &c.LikeCount, &c.LikedByMe); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to load comments")
			return
		}
		out = append(out, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": out})
}

// ---- Follows ----

func (a *App) handleFollow(w http.ResponseWriter, r *http.Request) {
	uid, target := userIDFrom(r), r.PathValue("id")
	if uid == target {
		writeErr(w, http.StatusBadRequest, "cannot follow yourself")
		return
	}
	if a.isBlockedEither(r.Context(), uid, target) {
		writeErr(w, http.StatusForbidden, "cannot follow this user")
		return
	}
	var locked bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT profile_locked FROM users WHERE id=$1 AND status='active'`, target).Scan(&locked); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if locked {
		_, err := a.db.Exec(r.Context(),
			`INSERT INTO follow_requests (follower_id, followee_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, target)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "request failed")
			return
		}
		a.notify(r.Context(), target, "follow_request", "Follow request",
			"Someone requested to follow you", map[string]any{"by": uid})
		writeJSON(w, http.StatusOK, map[string]string{"status": "requested"})
		return
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO follows (follower_id, followee_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, target)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	a.notify(r.Context(), target, "follow", "New follower",
		"Someone started following you", map[string]any{"by": uid})
	writeJSON(w, http.StatusOK, map[string]string{"status": "following"})
}

func (a *App) handleUnfollow(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM follows WHERE follower_id=$1 AND followee_id=$2`, userIDFrom(r), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "unfollowed"})
}

func (a *App) handleGetUser(w http.ResponseWriter, r *http.Request) {
	u, err := a.getUser(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	var followers, following int
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM follows WHERE followee_id=$1`, u.ID).Scan(&followers)
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM follows WHERE follower_id=$1`, u.ID).Scan(&following)
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "followers": followers, "following": following})
}

func (a *App) handleSearchUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, map[string]any{"users": []any{}})
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT id, username, display_name, avatar_url, is_verified FROM users
                 WHERE status='active' AND (username ILIKE '%'||$1||'%' OR display_name ILIKE '%'||$1||'%')
                 LIMIT 20`, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()
	type hit struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"display_name"`
		Avatar   string `json:"avatar_url"`
		Verified bool   `json:"is_verified"`
	}
	out := []hit{}
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.ID, &h.Username, &h.Name, &h.Avatar, &h.Verified); err == nil {
			out = append(out, h)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (a *App) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
		AvatarURL   string `json:"avatar_url"`
		Locale      string `json:"locale"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	_, err := a.db.Exec(r.Context(),
		`UPDATE users SET display_name = COALESCE(NULLIF($1,''), display_name),
                 bio = COALESCE(NULLIF($2,''), bio), avatar_url = COALESCE(NULLIF($3,''), avatar_url),
                 locale = COALESCE(NULLIF($4,''), locale), updated_at = now()
                 WHERE id = $5`,
		req.DisplayName, req.Bio, req.AvatarURL, req.Locale, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	a.handleMe(w, r)
}

func (a *App) handleNotifications(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, kind, payload, read_at, created_at FROM notifications
                 WHERE user_id = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`,
		userIDFrom(r), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load notifications")
		return
	}
	defer rows.Close()
	type notif struct {
		ID        int64          `json:"id"`
		Kind      string         `json:"kind"`
		Payload   map[string]any `json:"payload"`
		ReadAt    *time.Time     `json:"read_at"`
		CreatedAt time.Time      `json:"created_at"`
	}
	out := []notif{}
	for rows.Next() {
		var n notif
		if err := rows.Scan(&n.ID, &n.Kind, &n.Payload, &n.ReadAt, &n.CreatedAt); err == nil {
			out = append(out, n)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"notifications": out})
}

// mlModerate runs the text through the ML moderation endpoint. Fail-open.
func (a *App) mlModerate(text string) (string, float64) {
	if a.cfg.MLServiceURL == "" || strings.TrimSpace(text) == "" {
		return "allow", 0
	}
	body, _ := json.Marshal(map[string]any{"text": text})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.MLServiceURL+"/moderate", bytes.NewReader(body))
	if err != nil {
		return "allow", 0
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "allow", 0
	}
	defer resp.Body.Close()
	var out struct {
		Decision string  `json:"decision"`
		Score    float64 `json:"score"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out); err != nil {
		return "allow", 0
	}
	return out.Decision, out.Score
}
