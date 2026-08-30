package main

// Facebook-style content Groups, business Pages, and Events. Group/page
// posts reuse the posts table scoped by group_id / page_id, so comments,
// likes, hashtags and notifications work unchanged.

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func makeSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlnum {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// ---- Groups ----

func (a *App) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		CoverURL    string `json:"cover_url"`
		Privacy     string `json:"privacy"` // public | private
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) < 2 || len(req.Name) > 100 {
		writeErr(w, http.StatusBadRequest, "group name must be 2-100 characters")
		return
	}
	if req.Slug == "" {
		req.Slug = makeSlug(req.Name)
	}
	if !slugRe.MatchString(req.Slug) {
		writeErr(w, http.StatusBadRequest, "slug must be lowercase letters, digits, dashes")
		return
	}
	if req.Privacy != "private" {
		req.Privacy = "public"
	}
	uid := userIDFrom(r)
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO content_groups (name, slug, description, cover_url, privacy, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		req.Name, req.Slug, req.Description, req.CoverURL, req.Privacy, uid).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "slug already taken")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO group_members (group_id, user_id, role) VALUES ($1,$2,'owner')`, id, uid); err == nil {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE content_groups SET member_count = member_count + 1 WHERE id=$1`, id)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "slug": req.Slug})
}

func (a *App) handleListGroups(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, offset := pageParams(r)
	var rows interface {
		Next() bool
		Scan(...any) error
		Close()
	}
	var err error
	if q != "" {
		rows, err = a.db.Query(r.Context(),
			`SELECT id, name, slug, description, cover_url, privacy::text, member_count, created_at
			 FROM content_groups WHERE name ILIKE '%'||$1||'%' OR description ILIKE '%'||$1||'%'
			 ORDER BY member_count DESC LIMIT $2 OFFSET $3`, q, limit, offset)
	} else {
		rows, err = a.db.Query(r.Context(),
			`SELECT g.id, g.name, g.slug, g.description, g.cover_url, g.privacy::text, g.member_count, g.created_at
			 FROM content_groups g
			 LEFT JOIN group_members m ON m.group_id=g.id AND m.user_id=$1 AND m.status='active'
			 ORDER BY (m.user_id IS NOT NULL) DESC, g.member_count DESC LIMIT $2 OFFSET $3`,
			userIDFrom(r), limit, offset)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load groups")
		return
	}
	defer rows.Close()
	groups := []map[string]any{}
	for rows.Next() {
		var id, name, slug, desc, cover, privacy string
		var members int
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &slug, &desc, &cover, &privacy, &members, &createdAt); err != nil {
			continue
		}
		groups = append(groups, map[string]any{
			"id": id, "name": name, "slug": slug, "description": desc, "cover_url": cover,
			"privacy": privacy, "member_count": members, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (a *App) groupRole(r *http.Request, groupID, uid string) string {
	var role string
	_ = a.db.QueryRow(r.Context(),
		`SELECT role FROM group_members WHERE group_id=$1 AND user_id=$2 AND status='active'`,
		groupID, uid).Scan(&role)
	return role
}

func (a *App) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := userIDFrom(r)
	var name, slug, desc, cover, privacy, createdBy string
	var members int
	var createdAt time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT name, slug, description, cover_url, privacy::text, member_count, created_by, created_at
		 FROM content_groups WHERE id=$1`, id).
		Scan(&name, &slug, &desc, &cover, &privacy, &members, &createdBy, &createdAt)
	if err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	role := a.groupRole(r, id, uid)
	if privacy == "private" && role == "" {
		writeErr(w, http.StatusForbidden, "private group")
		return
	}
	mrows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username, u.display_name, u.avatar_url, m.role
		 FROM group_members m JOIN users u ON u.id=m.user_id
		 WHERE m.group_id=$1 AND m.status='active' ORDER BY m.joined_at LIMIT 100`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load members")
		return
	}
	defer mrows.Close()
	membersList := []map[string]any{}
	for mrows.Next() {
		var mid, uname, dname, avatar, mrole string
		if err := mrows.Scan(&mid, &uname, &dname, &avatar, &mrole); err == nil {
			membersList = append(membersList, map[string]any{
				"id": mid, "username": uname, "display_name": dname, "avatar_url": avatar, "role": mrole})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "name": name, "slug": slug, "description": desc, "cover_url": cover,
		"privacy": privacy, "member_count": members, "created_by": createdBy,
		"created_at": createdAt, "my_role": role, "members": membersList,
	})
}

func (a *App) handleJoinGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := userIDFrom(r)
	var privacy string
	if err := a.db.QueryRow(r.Context(),
		`SELECT privacy::text FROM content_groups WHERE id=$1`, id).Scan(&privacy); err != nil {
		writeErr(w, http.StatusNotFound, "group not found")
		return
	}
	status := "active"
	if privacy == "private" {
		status = "pending" // owner/admin approves via /review
	}
	tag, err := a.db.Exec(r.Context(),
		`INSERT INTO group_members (group_id, user_id, status) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		id, uid, status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "join failed")
		return
	}
	if tag.RowsAffected() > 0 && status == "active" {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE content_groups SET member_count = member_count + 1 WHERE id=$1`, id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (a *App) handleLeaveGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := userIDFrom(r)
	if a.groupRole(r, id, uid) == "owner" {
		writeErr(w, http.StatusBadRequest, "owner must transfer ownership before leaving")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2 AND status='active'`, id, uid)
	if err == nil && tag.RowsAffected() > 0 {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE content_groups SET member_count = GREATEST(member_count - 1, 0) WHERE id=$1`, id)
	}
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2`, id, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "left"})
}

// handleReviewGroupMember approves or rejects a pending join (owner/admin).
func (a *App) handleReviewGroupMember(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target := r.PathValue("uid")
	role := a.groupRole(r, id, userIDFrom(r))
	if role != "owner" && role != "admin" {
		writeErr(w, http.StatusForbidden, "owner or admin required")
		return
	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Approve {
		tag, err := a.db.Exec(r.Context(),
			`UPDATE group_members SET status='active' WHERE group_id=$1 AND user_id=$2 AND status='pending'`, id, target)
		if err == nil && tag.RowsAffected() > 0 {
			_, _ = a.db.Exec(r.Context(),
				`UPDATE content_groups SET member_count = member_count + 1 WHERE id=$1`, id)
			a.notify(r.Context(), target, "group_approved", "Group request approved",
				"Your request to join the group was approved", map[string]any{"group_id": id})
		}
	} else {
		_, _ = a.db.Exec(r.Context(),
			`DELETE FROM group_members WHERE group_id=$1 AND user_id=$2 AND status='pending'`, id, target)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reviewed"})
}

func (a *App) handleSetGroupRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target := r.PathValue("uid")
	if a.groupRole(r, id, userIDFrom(r)) != "owner" {
		writeErr(w, http.StatusForbidden, "owner required")
		return
	}
	var req struct {
		Role string `json:"role"` // admin | moderator | member
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Role {
	case "admin", "moderator", "member":
	default:
		writeErr(w, http.StatusBadRequest, "invalid role")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE group_members SET role=$3 WHERE group_id=$1 AND user_id=$2 AND role <> 'owner'`,
		id, target, req.Role)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// canPostToGroup reports whether uid may read/post in the group.
func (a *App) groupAccess(r *http.Request, groupID, uid string) (canRead, canPost bool) {
	var privacy string
	if err := a.db.QueryRow(r.Context(),
		`SELECT privacy::text FROM content_groups WHERE id=$1`, groupID).Scan(&privacy); err != nil {
		return false, false
	}
	role := a.groupRole(r, groupID, uid)
	if privacy == "public" {
		return true, role != ""
	}
	return role != "", role != ""
}

func (a *App) handleGroupFeed(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	canRead, _ := a.groupAccess(r, id, userIDFrom(r))
	if !canRead {
		writeErr(w, http.StatusForbidden, "group members only")
		return
	}
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.author_id, u.display_name, u.username, u.avatar_url, p.body,
		        p.like_count, p.comment_count, p.view_count, p.created_at
		 FROM posts p JOIN users u ON u.id=p.author_id
		 WHERE p.group_id=$1 AND p.deleted_at IS NULL
		 ORDER BY p.created_at DESC LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "feed failed")
		return
	}
	defer rows.Close()
	posts := []map[string]any{}
	for rows.Next() {
		var pid, aid, name, uname, avatar, body string
		var likes, comments, views int64
		var createdAt time.Time
		if err := rows.Scan(&pid, &aid, &name, &uname, &avatar, &body, &likes, &comments, &views, &createdAt); err != nil {
			continue
		}
		posts = append(posts, map[string]any{
			"id": pid, "author_id": aid, "display_name": name, "username": uname,
			"avatar_url": avatar, "body": body, "like_count": likes,
			"comment_count": comments, "view_count": views, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

func (a *App) handleGroupPost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := userIDFrom(r)
	_, canPost := a.groupAccess(r, id, uid)
	if !canPost {
		writeErr(w, http.StatusForbidden, "join the group to post")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	if len(req.Body) > 5000 {
		writeErr(w, http.StatusBadRequest, "post too long")
		return
	}
	var pid string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO posts (author_id, type, body, visibility, group_id)
		 VALUES ($1,'post',$2,'public',$3) RETURNING id`, uid, req.Body, id).Scan(&pid); err != nil {
		writeErr(w, http.StatusInternalServerError, "post failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": pid})
}

// ---- Pages ----

func (a *App) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Category    string `json:"category"`
		Description string `json:"description"`
		AvatarURL   string `json:"avatar_url"`
		CoverURL    string `json:"cover_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) < 2 || len(req.Name) > 100 {
		writeErr(w, http.StatusBadRequest, "page name must be 2-100 characters")
		return
	}
	if req.Slug == "" {
		req.Slug = makeSlug(req.Name)
	}
	if !slugRe.MatchString(req.Slug) {
		writeErr(w, http.StatusBadRequest, "slug must be lowercase letters, digits, dashes")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO pages (name, slug, category, description, avatar_url, cover_url, owner_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		req.Name, req.Slug, req.Category, req.Description, req.AvatarURL, req.CoverURL, userIDFrom(r)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "slug already taken")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "slug": req.Slug})
}

func (a *App) handleListPages(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, name, slug, category, description, avatar_url, cover_url, follower_count, created_at
		 FROM pages
		 WHERE $1 = '' OR name ILIKE '%'||$1||'%' OR category ILIKE '%'||$1||'%'
		 ORDER BY follower_count DESC LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load pages")
		return
	}
	defer rows.Close()
	pages := []map[string]any{}
	for rows.Next() {
		var id, name, slug, cat, desc, avatar, cover string
		var followers int
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &slug, &cat, &desc, &avatar, &cover, &followers, &createdAt); err != nil {
			continue
		}
		pages = append(pages, map[string]any{
			"id": id, "name": name, "slug": slug, "category": cat, "description": desc,
			"avatar_url": avatar, "cover_url": cover, "follower_count": followers, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pages": pages})
}

func (a *App) handleGetPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name, slug, cat, desc, avatar, cover, owner string
	var followers int
	var createdAt time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT name, slug, category, description, avatar_url, cover_url, owner_id, follower_count, created_at
		 FROM pages WHERE id=$1`, id).
		Scan(&name, &slug, &cat, &desc, &avatar, &cover, &owner, &followers, &createdAt)
	if err != nil {
		writeErr(w, http.StatusNotFound, "page not found")
		return
	}
	var following bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM page_followers WHERE page_id=$1 AND user_id=$2)`,
		id, userIDFrom(r)).Scan(&following)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "name": name, "slug": slug, "category": cat, "description": desc,
		"avatar_url": avatar, "cover_url": cover, "owner_id": owner,
		"follower_count": followers, "following": following, "created_at": createdAt,
	})
}

func (a *App) handleFollowPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tag, err := a.db.Exec(r.Context(),
		`INSERT INTO page_followers (page_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		id, userIDFrom(r))
	if err == nil && tag.RowsAffected() > 0 {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE pages SET follower_count = follower_count + 1 WHERE id=$1`, id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "following"})
}

func (a *App) handleUnfollowPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM page_followers WHERE page_id=$1 AND user_id=$2`, id, userIDFrom(r))
	if err == nil && tag.RowsAffected() > 0 {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE pages SET follower_count = GREATEST(follower_count - 1, 0) WHERE id=$1`, id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unfollowed"})
}

func (a *App) handlePageFeed(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.body, p.like_count, p.comment_count, p.view_count, p.created_at
		 FROM posts p WHERE p.page_id=$1 AND p.deleted_at IS NULL
		 ORDER BY p.created_at DESC LIMIT $2 OFFSET $3`, id, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "feed failed")
		return
	}
	defer rows.Close()
	posts := []map[string]any{}
	for rows.Next() {
		var pid, body string
		var likes, comments, views int64
		var createdAt time.Time
		if err := rows.Scan(&pid, &body, &likes, &comments, &views, &createdAt); err != nil {
			continue
		}
		posts = append(posts, map[string]any{
			"id": pid, "body": body, "like_count": likes, "comment_count": comments,
			"view_count": views, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

// handlePagePost: only the page owner publishes on the page.
func (a *App) handlePagePost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	uid := userIDFrom(r)
	var owner string
	if err := a.db.QueryRow(r.Context(), `SELECT owner_id FROM pages WHERE id=$1`, id).Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, "page not found")
		return
	}
	if owner != uid {
		writeErr(w, http.StatusForbidden, "only the page owner can publish")
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Body) == "" {
		writeErr(w, http.StatusBadRequest, "body required")
		return
	}
	if len(req.Body) > 5000 {
		writeErr(w, http.StatusBadRequest, "post too long")
		return
	}
	var pid string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO posts (author_id, type, body, visibility, page_id)
		 VALUES ($1,'post',$2,'public',$3) RETURNING id`, uid, req.Body, id).Scan(&pid); err != nil {
		writeErr(w, http.StatusInternalServerError, "post failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": pid})
}

// ---- Events ----

func (a *App) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title       string     `json:"title"`
		Description string     `json:"description"`
		Location    string     `json:"location"`
		CoverURL    string     `json:"cover_url"`
		StartsAt    time.Time  `json:"starts_at"`
		EndsAt      *time.Time `json:"ends_at"`
		GroupID     string     `json:"group_id"`
		PageID      string     `json:"page_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Title) < 2 || len(req.Title) > 140 {
		writeErr(w, http.StatusBadRequest, "event title must be 2-140 characters")
		return
	}
	if req.StartsAt.Before(time.Now().Add(-time.Hour)) {
		writeErr(w, http.StatusBadRequest, "event start must not be in the past")
		return
	}
	uid := userIDFrom(r)
	if req.GroupID != "" {
		_, canPost := a.groupAccess(r, req.GroupID, uid)
		if !canPost {
			writeErr(w, http.StatusForbidden, "group membership required")
			return
		}
	}
	if req.PageID != "" {
		var owner string
		if err := a.db.QueryRow(r.Context(), `SELECT owner_id FROM pages WHERE id=$1`, req.PageID).Scan(&owner); err != nil || owner != uid {
			writeErr(w, http.StatusForbidden, "only the page owner can create page events")
			return
		}
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO events (title, description, location, cover_url, starts_at, ends_at, group_id, page_id, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,$9) RETURNING id`,
		req.Title, req.Description, req.Location, req.CoverURL, req.StartsAt, req.EndsAt,
		req.GroupID, req.PageID, uid).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "event creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleListEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT e.id, e.title, e.description, e.location, e.starts_at, e.ends_at,
		        COALESCE(e.group_id::text,''), COALESCE(e.page_id::text,''),
		        (SELECT COUNT(*) FROM event_rsvps v WHERE v.event_id=e.id AND v.response='going')
		 FROM events e
		 WHERE e.starts_at > now() - interval '1 day'
		   AND (e.group_id IS NULL OR e.group_id IN (
		        SELECT g.id FROM content_groups g WHERE g.privacy='public'
		        UNION SELECT m.group_id FROM group_members m WHERE m.user_id=$1 AND m.status='active'))
		 ORDER BY e.starts_at LIMIT $2 OFFSET $3`, userIDFrom(r), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load events")
		return
	}
	defer rows.Close()
	events := []map[string]any{}
	for rows.Next() {
		var id, title, desc, loc, groupID, pageID string
		var startsAt time.Time
		var endsAt *time.Time
		var going int64
		if err := rows.Scan(&id, &title, &desc, &loc, &startsAt, &endsAt, &groupID, &pageID, &going); err != nil {
			continue
		}
		events = append(events, map[string]any{
			"id": id, "title": title, "description": desc, "location": loc,
			"starts_at": startsAt, "ends_at": endsAt, "group_id": groupID,
			"page_id": pageID, "going_count": going,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (a *App) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var title, desc, loc, cover, createdBy string
	var startsAt time.Time
	var endsAt *time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT title, description, location, cover_url, starts_at, ends_at, created_by
		 FROM events WHERE id=$1`, id).Scan(&title, &desc, &loc, &cover, &startsAt, &endsAt, &createdBy)
	if err != nil {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	counts := map[string]int64{}
	crows, err := a.db.Query(r.Context(),
		`SELECT response, COUNT(*) FROM event_rsvps WHERE event_id=$1 GROUP BY response`, id)
	if err == nil {
		for crows.Next() {
			var resp string
			var n int64
			if err := crows.Scan(&resp, &n); err == nil {
				counts[resp] = n
			}
		}
		crows.Close()
	}
	var myResponse string
	_ = a.db.QueryRow(r.Context(),
		`SELECT response FROM event_rsvps WHERE event_id=$1 AND user_id=$2`,
		id, userIDFrom(r)).Scan(&myResponse)
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "title": title, "description": desc, "location": loc, "cover_url": cover,
		"starts_at": startsAt, "ends_at": endsAt, "created_by": createdBy,
		"rsvp_counts": counts, "my_response": myResponse,
	})
}

func (a *App) handleRSVP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Response string `json:"response"` // going | interested | declined
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Response {
	case "going", "interested", "declined":
	default:
		writeErr(w, http.StatusBadRequest, "response must be going, interested or declined")
		return
	}
	var exists bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM events WHERE id=$1)`, id).Scan(&exists); err != nil || !exists {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	uid := userIDFrom(r)
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO event_rsvps (event_id, user_id, response) VALUES ($1,$2,$3)
		 ON CONFLICT (event_id, user_id) DO UPDATE SET response=$3`, id, uid, req.Response); err != nil {
		writeErr(w, http.StatusInternalServerError, "rsvp failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Response})
}
