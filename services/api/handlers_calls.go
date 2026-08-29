package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ---- Call/meeting/live orchestration over our own SFU ----
//
// The API mints short-lived HMAC tickets for the ChatApp SFU and hands the
// client our own STUN/TURN coordinates. Media and signaling never touch a
// third-party service.

func (a *App) sfuTicket(roomID, userID, role string) (string, error) {
	payload := fmt.Sprintf("%s|%s|%s|%d", roomID, userID, role,
		time.Now().Add(2*time.Hour).Unix())
	mac := hmac.New(sha256.New, []byte(a.cfg.SFUSecret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		hex.EncodeToString(mac.Sum(nil)), nil
}

// TURN REST credentials (RFC draft-uberti style): username "expiry:uid",
// password = base64(hmac-sha1(TURN_SECRET, username)).
func (a *App) turnCredentials(userID string) (string, string) {
	username := fmt.Sprintf("%d:%s", time.Now().Add(6*time.Hour).Unix(), userID)
	mac := hmac.New(sha1.New, []byte(a.cfg.TURNSecret))
	mac.Write([]byte(username))
	return username, base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) iceServers(userID string) []map[string]any {
	host := a.cfg.SFUHost
	user, pass := a.turnCredentials(userID)
	return []map[string]any{
		{"urls": []string{"stun:" + host + ":3478"}},
		{"urls": []string{"turn:" + host + ":3478"}, "username": user, "credential": pass},
	}
}

func (a *App) registerSFURoom(roomID, convID, mode string) error {
	body, _ := json.Marshal(map[string]string{
		"room_id": roomID, "conversation_id": convID, "mode": mode,
	})
	req, err := http.NewRequest("POST", a.cfg.SFUInternalURL+"/internal/rooms", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.SFUSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("sfu rejected room: %d", resp.StatusCode)
	}
	return nil
}

// POST /api/calls/rooms — start (or join) a meeting or live broadcast for a
// conversation. mode: "meeting" (default) | "live". In live mode the creator
// becomes the publisher.
func (a *App) handleCreateCallRoom(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		ConversationID string `json:"conversation_id"`
		Mode           string `json:"mode"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Mode != "live" {
		req.Mode = "meeting"
	}
	if !a.isMember(r.Context(), req.ConversationID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	roomID := req.ConversationID + "-" + req.Mode
	if err := a.registerSFURoom(roomID, req.ConversationID, req.Mode); err != nil {
		writeErr(w, http.StatusBadGateway, "media service unavailable")
		return
	}
	role := "publisher"
	ticket, err := a.sfuTicket(roomID, uid, role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ticket failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":     roomID,
		"mode":        req.Mode,
		"role":        role,
		"ticket":      ticket,
		"sfu_url":     a.cfg.SFUPublicURL,
		"ice_servers": a.iceServers(uid),
	})
}

// POST /api/calls/rooms/{roomId}/join — mint a ticket for an existing room.
// Live rooms hand out subscriber tickets to everyone except the broadcaster.
func (a *App) handleJoinCallRoom(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	roomID := r.PathValue("roomId")
	parts := strings.Split(roomID, "-")
	if len(parts) < 2 {
		writeErr(w, http.StatusBadRequest, "invalid room")
		return
	}
	mode := parts[len(parts)-1]
	convID := strings.Join(parts[:len(parts)-1], "-")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member")
		return
	}
	role := "publisher"
	if mode == "live" {
		role = "subscriber"
	}
	ticket, err := a.sfuTicket(roomID, uid, role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ticket failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":     roomID,
		"mode":        mode,
		"role":        role,
		"ticket":      ticket,
		"sfu_url":     a.cfg.SFUPublicURL,
		"ice_servers": a.iceServers(uid),
	})
}

// GET /api/live — active live broadcasts the user may watch (member of the
// conversation, or the conversation is a public channel).
func (a *App) handleLiveNow(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	req, err := http.NewRequest("GET", a.cfg.SFUInternalURL+"/internal/live", nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "live unavailable")
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.SFUSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "media service unavailable")
		return
	}
	defer resp.Body.Close()
	var body struct {
		Rooms []struct {
			RoomID  string `json:"room_id"`
			ConvID  string `json:"conversation_id"`
			Viewers int    `json:"viewers"`
		} `json:"rooms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadGateway, "media service error")
		return
	}
	type live struct {
		RoomID  string `json:"room_id"`
		ConvID  string `json:"conversation_id"`
		Title   string `json:"title"`
		Viewers int    `json:"viewers"`
	}
	out := []live{}
	for _, rm := range body.Rooms {
		var title string
		var isChannel bool
		if err := a.db.QueryRow(r.Context(),
			`SELECT COALESCE(title,''), is_channel FROM conversations WHERE id=$1`,
			rm.ConvID).Scan(&title, &isChannel); err != nil {
			continue
		}
		if !isChannel && !a.isMember(r.Context(), rm.ConvID, uid) {
			continue
		}
		out = append(out, live{RoomID: rm.RoomID, ConvID: rm.ConvID, Title: title, Viewers: rm.Viewers})
	}
	writeJSON(w, http.StatusOK, map[string]any{"live": out})
}
