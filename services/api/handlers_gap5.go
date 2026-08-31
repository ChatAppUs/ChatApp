package main

// Gap pack 5: persistent drop-in call rooms (Messenger Rooms parity: stable
// shareable links that any authenticated user can join).

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

// ---------- Persistent drop-in rooms ----------

func dropinSlug() string {
	buf := make([]byte, 9)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	// URL-safe, no padding: 12 chars, ~72 bits of entropy.
	return base64.RawURLEncoding.EncodeToString(buf)
}

// POST /api/rooms — create a persistent drop-in room with a shareable link.
func (a *App) handleCreateDropinRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Title) > 80 {
		writeErr(w, http.StatusBadRequest, "title too long")
		return
	}
	uid := userIDFrom(r)
	var id, slug string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO dropin_rooms (slug, title, host_id) VALUES ($1,$2,$3) RETURNING id, slug`,
		dropinSlug(), req.Title, uid).Scan(&id, &slug)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create room")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "slug": slug, "title": req.Title, "link": "/room/" + slug,
	})
}

// GET /api/rooms/{slug} — public preview before joining.
func (a *App) handleGetDropinRoom(w http.ResponseWriter, r *http.Request) {
	var title, hostName string
	var endedAt *time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT d.title, COALESCE(u.display_name, u.username::text), d.ended_at
		 FROM dropin_rooms d JOIN users u ON u.id = d.host_id WHERE d.slug=$1`,
		r.PathValue("slug")).Scan(&title, &hostName, &endedAt)
	if err != nil {
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slug": r.PathValue("slug"), "title": title, "host": hostName, "ended": endedAt != nil,
	})
}

// POST /api/rooms/{slug}/join — mint an SFU ticket for the room. Any
// authenticated user may join; the SFU room is registered lazily on first
// join so idle rooms cost nothing on the media plane.
func (a *App) handleJoinDropinRoom(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	slug := r.PathValue("slug")
	var endedAt *time.Time
	if err := a.db.QueryRow(r.Context(),
		`SELECT ended_at FROM dropin_rooms WHERE slug=$1`, slug).Scan(&endedAt); err != nil {
		writeErr(w, http.StatusNotFound, "room not found")
		return
	}
	if endedAt != nil {
		writeErr(w, http.StatusGone, "room has ended")
		return
	}
	roomID := "dropin-" + slug
	if err := a.registerSFURoom(roomID, "", "meeting"); err != nil {
		writeErr(w, http.StatusBadGateway, "media service unavailable")
		return
	}
	ticket, err := a.sfuTicket(roomID, uid, "publisher")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ticket failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id": roomID, "mode": "meeting", "role": "publisher",
		"ticket": ticket, "sfu_url": a.cfg.SFUPublicURL, "ice_servers": a.iceServers(uid),
	})
}

// POST /api/rooms/{slug}/end — host ends the room; the link stops working.
func (a *App) handleEndDropinRoom(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE dropin_rooms SET ended_at=now() WHERE slug=$1 AND host_id=$2 AND ended_at IS NULL`,
		r.PathValue("slug"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusForbidden, "only the host can end the room")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}

// GET /api/me/rooms — rooms I host (open first).
func (a *App) handleListMyDropinRooms(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT slug, title, created_at, ended_at FROM dropin_rooms
		 WHERE host_id=$1 ORDER BY ended_at IS NOT NULL, created_at DESC LIMIT 50`,
		userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	type room struct {
		Slug      string     `json:"slug"`
		Title     string     `json:"title"`
		Link      string     `json:"link"`
		CreatedAt time.Time  `json:"created_at"`
		EndedAt   *time.Time `json:"ended_at"`
	}
	out := []room{}
	for rows.Next() {
		var rm room
		if err := rows.Scan(&rm.Slug, &rm.Title, &rm.CreatedAt, &rm.EndedAt); err == nil {
			rm.Link = "/room/" + rm.Slug
			out = append(out, rm)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": out})
}
