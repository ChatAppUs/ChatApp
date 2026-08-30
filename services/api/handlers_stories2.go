package main

// Stories extras: permanent highlight collections and the close-friends list
// (used by the 'close_friends' post/story audience).

import (
	"net/http"
	"strings"
	"time"
)

// ---- highlights ----

func (a *App) handleCreateHighlight(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string `json:"title"`
		CoverURL string `json:"cover_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Title) == 0 || len(req.Title) > 60 {
		writeErr(w, http.StatusBadRequest, "title must be 1-60 characters")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO story_highlights (user_id, title, cover_url) VALUES ($1,$2,$3) RETURNING id`,
		userIDFrom(r), req.Title, req.CoverURL).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "highlight creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) listHighlights(w http.ResponseWriter, r *http.Request, ownerID string) {
	rows, err := a.db.Query(r.Context(),
		`SELECT h.id, h.title, h.cover_url, h.created_at,
		        (SELECT COUNT(*) FROM story_highlight_items i WHERE i.highlight_id=h.id)
		 FROM story_highlights h WHERE h.user_id=$1 ORDER BY h.created_at DESC`, ownerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load highlights")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title, cover string
		var createdAt time.Time
		var count int64
		if err := rows.Scan(&id, &title, &cover, &createdAt, &count); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "title": title, "cover_url": cover, "created_at": createdAt, "story_count": count,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"highlights": out})
}

func (a *App) handleMyHighlights(w http.ResponseWriter, r *http.Request) {
	a.listHighlights(w, r, userIDFrom(r))
}

// handleUserHighlights: highlights of stories still visible to the viewer.
func (a *App) handleUserHighlights(w http.ResponseWriter, r *http.Request) {
	ownerID := r.PathValue("id")
	uid := userIDFrom(r)
	if ownerID != uid {
		if a.isBlockedEither(r.Context(), uid, ownerID) {
			writeErr(w, http.StatusForbidden, "unavailable")
			return
		}
		// Non-owners only see highlights when they may view the profile:
		// public profiles, or followers of follower-locked profiles.
		var locked bool
		_ = a.db.QueryRow(r.Context(), `SELECT profile_locked FROM users WHERE id=$1`, ownerID).Scan(&locked)
		if locked {
			var following bool
			_ = a.db.QueryRow(r.Context(),
				`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id=$1 AND followee_id=$2)`,
				uid, ownerID).Scan(&following)
			if !following {
				writeErr(w, http.StatusForbidden, "profile is private")
				return
			}
		}
	}
	a.listHighlights(w, r, ownerID)
}

func (a *App) handleDeleteHighlight(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM story_highlights WHERE id=$1 AND user_id=$2`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "highlight not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *App) handleAddHighlightItem(w http.ResponseWriter, r *http.Request) {
	hlID := r.PathValue("id")
	storyID := r.PathValue("storyId")
	uid := userIDFrom(r)
	var owns bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM story_highlights WHERE id=$1 AND user_id=$2)`,
		hlID, uid).Scan(&owns); err != nil || !owns {
		writeErr(w, http.StatusNotFound, "highlight not found")
		return
	}
	// Only the author's own stories can be pinned into their highlights.
	var storyOK bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM posts WHERE id=$1 AND author_id=$2 AND type='story')`,
		storyID, uid).Scan(&storyOK); err != nil || !storyOK {
		writeErr(w, http.StatusBadRequest, "only your own stories can be highlighted")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO story_highlight_items (highlight_id, story_id, position)
		 VALUES ($1,$2, COALESCE((SELECT MAX(position)+1 FROM story_highlight_items WHERE highlight_id=$1), 0))
		 ON CONFLICT DO NOTHING`, hlID, storyID); err != nil {
		writeErr(w, http.StatusInternalServerError, "add failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (a *App) handleRemoveHighlightItem(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM story_highlight_items i USING story_highlights h
		 WHERE i.highlight_id=h.id AND h.user_id=$1 AND i.highlight_id=$2 AND i.story_id=$3`,
		userIDFrom(r), r.PathValue("id"), r.PathValue("storyId"))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "item not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ---- close friends ----

func (a *App) handleAddCloseFriend(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("id")
	uid := userIDFrom(r)
	if target == uid {
		writeErr(w, http.StatusBadRequest, "cannot add yourself")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO close_friends (user_id, friend_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		uid, target); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add close friend")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (a *App) handleRemoveCloseFriend(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM close_friends WHERE user_id=$1 AND friend_id=$2`, userIDFrom(r), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (a *App) handleListCloseFriends(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT u.id, u.username, u.display_name, u.avatar_url, cf.created_at
		 FROM close_friends cf JOIN users u ON u.id=cf.friend_id
		 WHERE cf.user_id=$1 ORDER BY cf.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load close friends")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uname, dname, avatar string
		var createdAt time.Time
		if err := rows.Scan(&id, &uname, &dname, &avatar, &createdAt); err == nil {
			out = append(out, map[string]any{
				"id": id, "username": uname, "display_name": dname, "avatar_url": avatar, "added_at": createdAt})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"close_friends": out})
}
