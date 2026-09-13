package main

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Forums — master plan §27/#30 (communities) and master documentation §75
// item 24. Decentralized-style discussion spaces: a forum holds topics, a
// topic holds threaded posts, moderators pin/lock topics, and visibility
// controls who may read. Standalone or attached to a group via group_id.

var forumSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

type forumJSON struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	GroupID     *string   `json:"group_id,omitempty"`
	Title       string    `json:"title"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Visibility  string    `json:"visibility"`
	TopicCount  int       `json:"topic_count"`
	PostCount   int       `json:"post_count"`
	CreatedAt   time.Time `json:"created_at"`
}

const forumSelect = `
 SELECT f.id, f.owner_id, f.group_id, f.title, f.slug, f.description,
        f.visibility,
        (SELECT COUNT(*) FROM forum_topics t WHERE t.forum_id = f.id),
        (SELECT COUNT(*) FROM forum_posts p
           JOIN forum_topics t ON t.id = p.topic_id WHERE t.forum_id = f.id),
        f.created_at
   FROM forums f `

func scanForums(rows interface {
	Next() bool
	Scan(...any) error
	Close()
}) []forumJSON {
	out := []forumJSON{}
	for rows.Next() {
		var f forumJSON
		if err := rows.Scan(&f.ID, &f.OwnerID, &f.GroupID, &f.Title, &f.Slug,
			&f.Description, &f.Visibility, &f.TopicCount, &f.PostCount,
			&f.CreatedAt); err == nil {
			out = append(out, f)
		}
	}
	rows.Close()
	return out
}

// canModerateForum returns true when the user owns the forum or was added as a
// moderator. Authorization is always resolved server-side.
func (a *App) canModerateForum(r *http.Request, forumID, uid string) bool {
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT owner_id FROM forums WHERE id=$1`, forumID).Scan(&owner); err != nil {
		return false
	}
	if owner == uid {
		return true
	}
	var ok bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM forum_moderators WHERE forum_id=$1 AND user_id=$2)`,
		forumID, uid).Scan(&ok)
	return ok
}

// POST /api/forums — create a forum owned by the caller.
func (a *App) handleForumCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string `json:"title"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
		GroupID     string `json:"group_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 140 {
		writeErr(w, http.StatusBadRequest, "title required (max 140 chars)")
		return
	}
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if !forumSlugRe.MatchString(slug) {
		writeErr(w, http.StatusBadRequest,
			"slug must be 2-63 chars of lowercase letters, digits or hyphens")
		return
	}
	vis := strings.ToLower(strings.TrimSpace(req.Visibility))
	if vis == "" {
		vis = "public"
	}
	if vis != "public" && vis != "private" && vis != "secret" {
		writeErr(w, http.StatusBadRequest, "visibility must be public, private or secret")
		return
	}
	if len(req.Description) > 2000 {
		writeErr(w, http.StatusBadRequest, "description too long")
		return
	}
	uid := userIDFrom(r)
	var id string
	var groupID *string
	if g := strings.TrimSpace(req.GroupID); g != "" {
		groupID = &g
	}
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO forums (owner_id, group_id, title, slug, description, visibility)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		uid, groupID, title, slug, strings.TrimSpace(req.Description), vis).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			writeErr(w, http.StatusConflict, "slug already taken")
			return
		}
		writeErr(w, http.StatusBadRequest, "forum creation failed")
		return
	}
	a.audit(r.Context(), uid, "forum.create", id, map[string]any{"slug": slug})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "slug": slug})
}

// GET /api/forums — list forums visible to the caller.
func (a *App) handleForumList(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := a.db.Query(r.Context(), forumSelect+`
	  WHERE f.visibility = 'public'
	     OR f.owner_id = $1
	     OR EXISTS (SELECT 1 FROM forum_moderators m
	                 WHERE m.forum_id = f.id AND m.user_id = $1)
	     OR ($2 <> '' AND (f.title ILIKE '%'||$2||'%' OR f.description ILIKE '%'||$2||'%'))
	  ORDER BY f.created_at DESC LIMIT 100`, uid, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load forums")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"forums": scanForums(rows)})
}

// GET /api/forums/search?q= — full-text search across forums and topics.
func (a *App) handleForumSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, "q required")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT t.id, t.forum_id, f.slug, f.title, t.title, t.post_count,
		        t.last_activity_at
		   FROM forum_topics t JOIN forums f ON f.id = t.forum_id
		  WHERE f.visibility = 'public'
		    AND (t.title ILIKE '%'||$1||'%' OR t.body ILIKE '%'||$1||'%')
		  ORDER BY t.last_activity_at DESC LIMIT 50`, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "search failed")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, forumID, slug, forumTitle, title string
		var posts int
		var last time.Time
		if err := rows.Scan(&id, &forumID, &slug, &forumTitle, &title, &posts, &last); err == nil {
			out = append(out, map[string]any{
				"topic_id": id, "forum_id": forumID, "forum_slug": slug,
				"forum_title": forumTitle, "title": title,
				"post_count": posts, "last_activity_at": last,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out})
}

// GET /api/forums/{slug} — forum detail.
func (a *App) handleForumGet(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	slug := r.PathValue("slug")
	rows, err := a.db.Query(r.Context(), forumSelect+`
	  WHERE f.slug = $1
	    AND (f.visibility = 'public' OR f.owner_id = $2
	         OR EXISTS (SELECT 1 FROM forum_moderators m
	                     WHERE m.forum_id = f.id AND m.user_id = $2))`, slug, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load forum")
		return
	}
	forums := scanForums(rows)
	if len(forums) == 0 {
		writeErr(w, http.StatusNotFound, "forum not found")
		return
	}
	writeJSON(w, http.StatusOK, forums[0])
}

// POST /api/forums/{id}/topics — open a topic.
func (a *App) handleForumTopicCreate(w http.ResponseWriter, r *http.Request) {
	forumID, ok := requireUUIDPath(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 {
		writeErr(w, http.StatusBadRequest, "title required (max 200 chars)")
		return
	}
	if len(req.Body) > 20000 {
		writeErr(w, http.StatusBadRequest, "body too long")
		return
	}
	var visibility string
	if err := a.db.QueryRow(r.Context(),
		`SELECT visibility FROM forums WHERE id=$1`, forumID).Scan(&visibility); err != nil {
		writeErr(w, http.StatusNotFound, "forum not found")
		return
	}
	uid := userIDFrom(r)
	if visibility != "public" && !a.canModerateForum(r, forumID, uid) {
		// Private/secret forums only accept topics from members.
		var member bool
		_ = a.db.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM forum_moderators WHERE forum_id=$1 AND user_id=$2)`,
			forumID, uid).Scan(&member)
		if !member {
			writeErr(w, http.StatusForbidden, "forum is not public")
			return
		}
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO forum_topics (forum_id, author_id, title, body)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		forumID, uid, title, strings.TrimSpace(req.Body)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "topic creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// GET /api/forums/{id}/topics — topic list (pinned first).
func (a *App) handleForumTopicList(w http.ResponseWriter, r *http.Request) {
	forumID, ok := requireUUIDPath(w, r, "id")
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT t.id, t.title, t.body, t.pinned, t.locked, t.post_count,
		        t.created_at, t.last_activity_at,
		        COALESCE(u.username,''), t.author_id
		   FROM forum_topics t LEFT JOIN users u ON u.id = t.author_id
		  WHERE t.forum_id = $1
		  ORDER BY t.pinned DESC, t.last_activity_at DESC LIMIT 200`, forumID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load topics")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, body, author, authorID string
		var pinned, locked bool
		var posts int
		var created, last time.Time
		if err := rows.Scan(&id, &title, &body, &pinned, &locked, &posts,
			&created, &last, &author, &authorID); err == nil {
			out = append(out, map[string]any{
				"id": id, "title": title, "body": body, "pinned": pinned,
				"locked": locked, "post_count": posts, "created_at": created,
				"last_activity_at": last, "author": author, "author_id": authorID,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"topics": out})
}

// POST /api/forums/topics/{topicId}/posts — reply (threaded via parent_id).
func (a *App) handleForumPostCreate(w http.ResponseWriter, r *http.Request) {
	topicID, ok := requireUUIDPath(w, r, "topicId")
	if !ok {
		return
	}
	var req struct {
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len(body) > 20000 {
		writeErr(w, http.StatusBadRequest, "body required (max 20000 chars)")
		return
	}
	var locked bool
	var forumID string
	err := a.db.QueryRow(r.Context(),
		`SELECT t.locked, t.forum_id FROM forum_topics t WHERE t.id=$1`,
		topicID).Scan(&locked, &forumID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "topic not found")
		return
	}
	if locked && !a.canModerateForum(r, forumID, userIDFrom(r)) {
		writeErr(w, http.StatusForbidden, "topic is locked")
		return
	}
	var parent *string
	if p := strings.TrimSpace(req.ParentID); p != "" {
		var exists bool
		_ = a.db.QueryRow(r.Context(),
			`SELECT EXISTS (SELECT 1 FROM forum_posts WHERE id=$1 AND topic_id=$2)`,
			p, topicID).Scan(&exists)
		if !exists {
			writeErr(w, http.StatusBadRequest, "parent post not in this topic")
			return
		}
		parent = &p
	}
	uid := userIDFrom(r)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reply failed")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO forum_posts (topic_id, author_id, parent_id, body)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		topicID, uid, parent, body).Scan(&id); err != nil {
		writeErr(w, http.StatusBadRequest, "reply failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE forum_topics SET post_count = post_count + 1,
		        last_activity_at = now() WHERE id=$1`, topicID); err != nil {
		writeErr(w, http.StatusInternalServerError, "reply failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "reply failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// GET /api/forums/topics/{topicId}/posts — threaded posts.
func (a *App) handleForumPostList(w http.ResponseWriter, r *http.Request) {
	topicID, ok := requireUUIDPath(w, r, "topicId")
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.parent_id, p.body, p.created_at,
		        COALESCE(u.username,''), p.author_id
		   FROM forum_posts p LEFT JOIN users u ON u.id = p.author_id
		  WHERE p.topic_id = $1 ORDER BY p.created_at ASC LIMIT 500`, topicID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load posts")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, body, author, authorID string
		var parent *string
		var created time.Time
		if err := rows.Scan(&id, &parent, &body, &created, &author, &authorID); err == nil {
			out = append(out, map[string]any{
				"id": id, "parent_id": parent, "body": body,
				"created_at": created, "author": author, "author_id": authorID,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": out})
}

// PUT /api/forums/topics/{topicId}/pin — moderator pin toggle.
func (a *App) handleForumTopicPin(w http.ResponseWriter, r *http.Request) {
	a.forumTopicFlag(w, r, "pinned")
}

// PUT /api/forums/topics/{topicId}/lock — moderator lock toggle.
func (a *App) handleForumTopicLock(w http.ResponseWriter, r *http.Request) {
	a.forumTopicFlag(w, r, "locked")
}

func (a *App) forumTopicFlag(w http.ResponseWriter, r *http.Request, column string) {
	topicID, ok := requireUUIDPath(w, r, "topicId")
	if !ok {
		return
	}
	var req struct {
		Value *bool `json:"value"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var forumID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT forum_id FROM forum_topics WHERE id=$1`, topicID).Scan(&forumID); err != nil {
		writeErr(w, http.StatusNotFound, "topic not found")
		return
	}
	uid := userIDFrom(r)
	if !a.canModerateForum(r, forumID, uid) {
		writeErr(w, http.StatusForbidden, "moderator permission required")
		return
	}
	// column is a fixed internal identifier, never user input.
	var value any = true
	if req.Value != nil {
		value = *req.Value
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE forum_topics SET `+column+` = $2 WHERE id=$1`, topicID, value); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	a.audit(r.Context(), uid, "forum.topic."+column, topicID, map[string]any{"value": value})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "topic_id": topicID})
}

// DELETE /api/forums/posts/{postId} — author or moderator deletion.
func (a *App) handleForumPostDelete(w http.ResponseWriter, r *http.Request) {
	postID, ok := requireUUIDPath(w, r, "postId")
	if !ok {
		return
	}
	var authorID, forumID string
	err := a.db.QueryRow(r.Context(),
		`SELECT p.author_id, t.forum_id FROM forum_posts p
		   JOIN forum_topics t ON t.id = p.topic_id WHERE p.id=$1`,
		postID).Scan(&authorID, &forumID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "post not found")
		return
	}
	uid := userIDFrom(r)
	if authorID != uid && !a.canModerateForum(r, forumID, uid) {
		writeErr(w, http.StatusForbidden, "not allowed")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`DELETE FROM forum_posts WHERE id=$1`, postID); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
