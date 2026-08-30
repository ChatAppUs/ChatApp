package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// Chat personalization: per-conversation themes (a named gradient/color key
// every client renders natively) and per-chat nicknames for any member, both
// Messenger-style — any member can set them, everyone sees them.

var themeKeyRe = regexp.MustCompile(`^[a-z0-9_-]{1,40}$`)

// PUT /api/conversations/{id}/theme — set the conversation theme. An empty
// theme resets to the client default.
func (a *App) handleSetChatTheme(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Theme string `json:"theme"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, convID := userIDFrom(r), r.PathValue("id")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member of this conversation")
		return
	}
	theme := strings.ToLower(strings.TrimSpace(req.Theme))
	if theme != "" && !themeKeyRe.MatchString(theme) {
		writeErr(w, http.StatusBadRequest, "theme must be 1-40 chars of a-z 0-9 _ -")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE conversations SET theme=$2 WHERE id=$1`, convID, theme); err != nil {
		writeErr(w, http.StatusInternalServerError, "theme update failed")
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"type": "conversation_theme", "conversation_id": convID, "theme": theme,
	})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "theme": theme})
}

// PUT /api/conversations/{id}/nicknames/{userId} — set a member's nickname in
// this conversation. Empty nickname clears it.
func (a *App) handleSetNickname(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nickname string `json:"nickname"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, convID, target := userIDFrom(r), r.PathValue("id"), r.PathValue("userId")
	if !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusForbidden, "not a member of this conversation")
		return
	}
	nick := strings.TrimSpace(req.Nickname)
	if len(nick) > 50 {
		writeErr(w, http.StatusBadRequest, "nickname too long")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE conversation_members SET nickname=$3
		 WHERE conversation_id=$1 AND user_id=$2`, convID, target, nick)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "member not found")
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"type": "nickname_changed", "conversation_id": convID, "user_id": target,
	})
	a.fanoutToMembers(r.Context(), convID, payload, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "nickname": nick})
}
