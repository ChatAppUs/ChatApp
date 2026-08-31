package main

// Gap pack 5: long-form articles + bio links (helpers), custom emoji,
// message translation (built-in dictionary layer), live rooms with
// viewer tracking, and the safety-mode auto-block screen.

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ---------- validation helpers ----------

// validateArticle normalizes a long-form article payload (X Articles
// parity). Returns nil when no article was supplied.
func validateArticle(raw json.RawMessage) (json.RawMessage, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var a struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle"`
		Body     string `json:"body"`
		CoverURL string `json:"cover_url"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, "article must be an object"
	}
	a.Title = capRunes(strings.TrimSpace(a.Title), 300)
	a.Subtitle = capRunes(strings.TrimSpace(a.Subtitle), 300)
	a.Body = capRunes(a.Body, 100000)
	if a.Title == "" {
		return nil, "article title required"
	}
	if strings.TrimSpace(a.Body) == "" {
		return nil, "article body required"
	}
	if a.CoverURL != "" && !strings.HasPrefix(a.CoverURL, "https://") && !strings.HasPrefix(a.CoverURL, "http://") {
		return nil, "article cover_url must be http(s)"
	}
	out, _ := json.Marshal(a)
	return out, ""
}

// sanitizeBioLinks validates the profile link list (max 5, http(s) only).
func sanitizeBioLinks(raw json.RawMessage) ([]byte, string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var links []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, "bio_links must be an array"
	}
	if len(links) > 5 {
		return nil, "max 5 bio links"
	}
	for i := range links {
		links[i].Title = capRunes(strings.TrimSpace(links[i].Title), 40)
		links[i].URL = strings.TrimSpace(links[i].URL)
		if links[i].Title == "" || (!strings.HasPrefix(links[i].URL, "https://") && !strings.HasPrefix(links[i].URL, "http://")) {
			return nil, "each bio link needs a title and an http(s) url"
		}
		if len(links[i].URL) > 300 {
			return nil, "bio link url too long"
		}
	}
	out, _ := json.Marshal(links)
	return out, ""
}

// sanitizeWaveform clamps voice-note peak buckets (client-computed, ≤128
// buckets of 0..100) so hostile payloads can't bloat rows or fanout.
func sanitizeWaveform(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("[]")
	}
	var buckets []int
	if err := json.Unmarshal(raw, &buckets); err != nil || len(buckets) > 128 {
		return json.RawMessage("[]")
	}
	for i, b := range buckets {
		if b < 0 {
			buckets[i] = 0
		} else if b > 100 {
			buckets[i] = 100
		}
	}
	out, err := json.Marshal(buckets)
	if err != nil {
		return json.RawMessage("[]")
	}
	return out
}

// ---------- safety mode (X parity) ----------

// safetyAutoBlock reports whether an incoming stranger DM should be
// auto-blocked for a recipient who enabled safety mode: senders whose
// account is <7 days old or who have 3+ reports against them.
func (a *App) safetyAutoBlock(ctx context.Context, recipientID, senderID string) bool {
	var enabled bool
	_ = a.db.QueryRow(ctx, `SELECT COALESCE(safety_mode,false) FROM users WHERE id=$1`, recipientID).Scan(&enabled)
	if !enabled {
		return false
	}
	var risky bool
	_ = a.db.QueryRow(ctx,
		`SELECT (created_at > now() - interval '7 days')
		       OR (SELECT COUNT(*) FROM reports WHERE target_type='user' AND target_id=$1) >= 3
		 FROM users WHERE id=$1`, senderID).Scan(&risky)
	return risky
}

// ---------- custom emoji (Telegram parity) ----------

var shortcodeRe = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

func (a *App) customEmojiExists(ctx context.Context, shortcode string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM custom_emoji WHERE name=$1)`, shortcode).Scan(&ok)
	return ok
}

func (a *App) handleListCustomEmoji(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT name, media_url FROM custom_emoji ORDER BY name LIMIT 500`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load custom emoji")
		return
	}
	defer rows.Close()
	type emoji struct {
		Shortcode string `json:"shortcode"`
		MediaURL  string `json:"media_url"`
	}
	out := []emoji{}
	for rows.Next() {
		var e emoji
		if err := rows.Scan(&e.Shortcode, &e.MediaURL); err == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"emoji": out})
}

func (a *App) handleAdminAddCustomEmoji(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Shortcode string `json:"shortcode"`
		MediaURL  string `json:"media_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Shortcode = strings.ToLower(strings.TrimSpace(req.Shortcode))
	req.MediaURL = strings.TrimSpace(req.MediaURL)
	if !shortcodeRe.MatchString(req.Shortcode) {
		writeErr(w, http.StatusBadRequest, "shortcode must be 2-32 chars [a-z0-9_]")
		return
	}
	if !strings.HasPrefix(req.MediaURL, "https://") && !strings.HasPrefix(req.MediaURL, "http://") && !strings.HasPrefix(req.MediaURL, "/media/") {
		writeErr(w, http.StatusBadRequest, "media_url must be http(s) or /media/")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO custom_emoji (owner_id, name, media_url) VALUES ($1,$2,$3)
		 ON CONFLICT (name) DO UPDATE SET media_url=EXCLUDED.media_url
		 RETURNING id`, userIDFrom(r), req.Shortcode, req.MediaURL).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save custom emoji")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "shortcode": req.Shortcode})
}

func (a *App) handleAdminDeleteCustomEmoji(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(r.PathValue("code"))
	res, err := a.db.Exec(r.Context(), `DELETE FROM custom_emoji WHERE name=$1`, code)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "custom emoji not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": code})
}

// ---------- message translation (imo parity) ----------

// translateDict is a compact built-in phrasebook covering the most common
// chat phrases in the app's 8 UI locales. Unknown words pass through
// unchanged — the dictionary layer is deterministic and in-process (no
// network hop), and can be swapped for the ML service without API changes.
var translateDict = map[string]map[string]string{
	"es": {"hello": "hola", "hi": "hola", "good morning": "buenos días", "good night": "buenas noches", "thank you": "gracias", "thanks": "gracias", "yes": "sí", "no": "no", "goodbye": "adiós", "bye": "adiós", "how are you": "cómo estás", "love": "amor", "friend": "amigo", "welcome": "bienvenido"},
	"fr": {"hello": "bonjour", "hi": "salut", "good morning": "bonjour", "good night": "bonne nuit", "thank you": "merci", "thanks": "merci", "yes": "oui", "no": "non", "goodbye": "au revoir", "bye": "salut", "how are you": "comment ça va", "love": "amour", "friend": "ami", "welcome": "bienvenue"},
	"de": {"hello": "hallo", "hi": "hallo", "good morning": "guten morgen", "good night": "gute nacht", "thank you": "danke", "thanks": "danke", "yes": "ja", "no": "nein", "goodbye": "auf wiedersehen", "bye": "tschüss", "how are you": "wie geht es dir", "love": "liebe", "friend": "freund", "welcome": "willkommen"},
	"pt": {"hello": "olá", "hi": "oi", "good morning": "bom dia", "good night": "boa noite", "thank you": "obrigado", "thanks": "obrigado", "yes": "sim", "no": "não", "goodbye": "tchau", "bye": "tchau", "how are you": "como vai", "love": "amor", "friend": "amigo", "welcome": "bem-vindo"},
	"ar": {"hello": "مرحبا", "hi": "مرحبا", "good morning": "صباح الخير", "good night": "تصبح على خير", "thank you": "شكرا", "thanks": "شكرا", "yes": "نعم", "no": "لا", "goodbye": "وداعا", "bye": "وداعا", "how are you": "كيف حالك", "love": "حب", "friend": "صديق", "welcome": "أهلا بك"},
	"hi": {"hello": "नमस्ते", "hi": "नमस्ते", "good morning": "सुप्रभात", "good night": "शुभ रात्रि", "thank you": "धन्यवाद", "thanks": "धन्यवाद", "yes": "हाँ", "no": "नहीं", "goodbye": "अलविदा", "bye": "अलविदा", "how are you": "कैसे हो", "love": "प्यार", "friend": "दोस्त", "welcome": "स्वागत है"},
	"ja": {"hello": "こんにちは", "hi": "やあ", "good morning": "おはよう", "good night": "おやすみ", "thank you": "ありがとう", "thanks": "ありがとう", "yes": "はい", "no": "いいえ", "goodbye": "さようなら", "bye": "じゃあね", "how are you": "元気ですか", "love": "愛", "friend": "友達", "welcome": "ようこそ"},
}

var targetLangRe = regexp.MustCompile(`^[a-z]{2}(-[a-zA-Z]{2})?$`)

// translateLocal translates whole-phrase matches first, then word-by-word.
func translateLocal(text, target string) (string, bool) {
	dict, ok := translateDict[target]
	if !ok {
		return "", false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if hit, ok := dict[lower]; ok {
		return hit, true
	}
	words := strings.Fields(text)
	out := make([]string, 0, len(words))
	anyTranslated := false
	for _, w := range words {
		trimmed := strings.Trim(strings.ToLower(w), ".,!?;:'\"")
		if hit, ok := dict[trimmed]; ok {
			out = append(out, hit)
			anyTranslated = true
		} else {
			out = append(out, w)
		}
	}
	return strings.Join(out, " "), anyTranslated || len(words) > 0
}

func (a *App) handleTranslateMessage(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	var req struct {
		TargetLang string `json:"target_lang"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.TargetLang = strings.ToLower(strings.TrimSpace(req.TargetLang))
	if !targetLangRe.MatchString(req.TargetLang) {
		writeErr(w, http.StatusBadRequest, "target_lang must be a BCP-47 code like 'es' or 'pt-br'")
		return
	}
	uid := userIDFrom(r)
	convID, ok := a.messageConvID(r.Context(), msgID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	var body string
	if err := a.db.QueryRow(r.Context(), `SELECT body FROM messages WHERE id=$1`, msgID).Scan(&body); err != nil || strings.TrimSpace(body) == "" {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	// Cache hit?
	var cached string
	if err := a.db.QueryRow(r.Context(),
		`SELECT translated_text FROM message_translations WHERE message_id=$1 AND lang=$2`,
		msgID, req.TargetLang).Scan(&cached); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"translation": map[string]any{
			"message_id": msgID, "target_lang": req.TargetLang, "translated": cached, "provider": "cache"}})
		return
	}
	translated, ok := translateLocal(body, req.TargetLang)
	if !ok {
		writeErr(w, http.StatusBadRequest, "unsupported target language")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO message_translations (message_id, lang, translated_text, engine)
		 VALUES ($1,$2,$3,'local-dict-v1')
		 ON CONFLICT (message_id, lang) DO UPDATE SET translated_text=EXCLUDED.translated_text`,
		msgID, req.TargetLang, translated)
	writeJSON(w, http.StatusOK, map[string]any{"translation": map[string]any{
		"message_id": msgID, "target_lang": req.TargetLang, "translated": translated, "provider": "local-dict-v1"}})
}

func (a *App) handleMessageTranslations(w http.ResponseWriter, r *http.Request) {
	msgID := r.PathValue("id")
	uid := userIDFrom(r)
	convID, ok := a.messageConvID(r.Context(), msgID)
	if !ok || !a.isMember(r.Context(), convID, uid) {
		writeErr(w, http.StatusNotFound, "message not found")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT lang, translated_text, engine, created_at FROM message_translations WHERE message_id=$1 ORDER BY created_at DESC`, msgID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load translations")
		return
	}
	defer rows.Close()
	type tr struct {
		Lang       string    `json:"target_lang"`
		Translated string    `json:"translated"`
		Provider   string    `json:"provider"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := []tr{}
	for rows.Next() {
		var t tr
		if err := rows.Scan(&t.Lang, &t.Translated, &t.Provider, &t.CreatedAt); err == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"translations": out})
}

// messageConvID resolves a message's conversation for membership checks.
func (a *App) messageConvID(ctx context.Context, msgID string) (string, bool) {
	var convID string
	err := a.db.QueryRow(ctx, `SELECT conversation_id FROM messages WHERE id=$1`, msgID).Scan(&convID)
	return convID, err == nil
}

// ---------- live rooms (Facebook Live / TikTok LIVE parity) ----------

type liveRoomOut struct {
	ID          string     `json:"id"`
	HostID      string     `json:"host_id"`
	HostName    string     `json:"host_display_name"`
	HostUser    string     `json:"host_username"`
	HostAvatar  string     `json:"host_avatar_url"`
	Title       string     `json:"title"`
	Category    string     `json:"category"`
	Status      string     `json:"status"`
	LikeCount   int64      `json:"like_count"`
	ViewerCount int        `json:"viewer_count"`
	PeakViewers int        `json:"peak_viewers"`
	CreatedAt   time.Time  `json:"created_at"`
	EndedAt     *time.Time `json:"ended_at"`
}

const liveRoomSelect = `SELECT lr.id::text, lr.host_id::text, u.display_name, u.username, u.avatar_url,
       lr.title, lr.category, lr.status, lr.like_count,
       (SELECT COUNT(*) FROM live_room_viewers v WHERE v.room_id = lr.id),
       lr.peak_viewers, lr.created_at, lr.ended_at
FROM live_rooms lr JOIN users u ON u.id = lr.host_id`

func scanLiveRoom(row interface{ Scan(...any) error }) (liveRoomOut, error) {
	var rmo liveRoomOut
	err := row.Scan(&rmo.ID, &rmo.HostID, &rmo.HostName, &rmo.HostUser, &rmo.HostAvatar,
		&rmo.Title, &rmo.Category, &rmo.Status, &rmo.LikeCount, &rmo.ViewerCount,
		&rmo.PeakViewers, &rmo.CreatedAt, &rmo.EndedAt)
	return rmo, err
}

// POST /api/live-rooms — open a persistent drop-in room (one live room per host).
func (a *App) handleCreateLiveRoom(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string `json:"title"`
		Category string `json:"category"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" || len(req.Title) > 120 {
		writeErr(w, http.StatusBadRequest, "title required (120 chars max)")
		return
	}
	uid := userIDFrom(r)
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO live_rooms (host_id, title, category)
		 SELECT $1, $2, $3
		 WHERE NOT EXISTS (SELECT 1 FROM live_rooms WHERE host_id=$1 AND status='live')
		 RETURNING id`, uid, strings.TrimSpace(req.Title), strings.TrimSpace(req.Category)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "you already have a live room")
		return
	}
	room, err := scanLiveRoom(a.db.QueryRow(r.Context(), liveRoomSelect+` WHERE lr.id=$1`, id))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load room")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"room": room})
}

// GET /api/live-rooms — discoverable live rooms.
func (a *App) handleListLiveRooms(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		liveRoomSelect+` WHERE lr.status='live' ORDER BY lr.created_at DESC LIMIT 50`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load rooms")
		return
	}
	defer rows.Close()
	out := []liveRoomOut{}
	for rows.Next() {
		rmo, err := scanLiveRoom(rows)
		if err == nil {
			out = append(out, rmo)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": out})
}

// GET /api/live-rooms/{id}
func (a *App) handleGetLiveRoom(w http.ResponseWriter, r *http.Request) {
	room, err := scanLiveRoom(a.db.QueryRow(r.Context(), liveRoomSelect+` WHERE lr.id=$1`, r.PathValue("id")))
	if err != nil {
		writeErr(w, http.StatusNotFound, "live room not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"room": room})
}

// POST /api/live-rooms/{id}/join — drop in as a viewer; tracks peak viewers.
func (a *App) handleLiveRoomJoin(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	uid := userIDFrom(r)
	var live bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT status='live' FROM live_rooms WHERE id=$1`, roomID).Scan(&live); err != nil || !live {
		writeErr(w, http.StatusNotFound, "live room not found or already ended")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO live_room_viewers (room_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		roomID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to join")
		return
	}
	// Peak tracking: cheapest correct spot is right after membership changed.
	a.db.Exec(r.Context(),
		`UPDATE live_rooms SET peak_viewers = GREATEST(peak_viewers,
		   (SELECT COUNT(*) FROM live_room_viewers WHERE room_id=$1)) WHERE id=$1`, roomID)
	var viewers int
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM live_room_viewers WHERE room_id=$1`, roomID).Scan(&viewers)
	writeJSON(w, http.StatusCreated, map[string]any{"id": roomID, "viewer_count": viewers})
}

// POST /api/live-rooms/{id}/leave
func (a *App) handleLiveRoomLeave(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM live_room_viewers WHERE room_id=$1 AND user_id=$2`, roomID, userIDFrom(r))
	var viewers int
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM live_room_viewers WHERE room_id=$1`, roomID).Scan(&viewers)
	writeJSON(w, http.StatusOK, map[string]any{"id": roomID, "viewer_count": viewers})
}

// POST /api/live-rooms/{id}/like — TikTok-style tap likes.
func (a *App) handleLiveRoomLike(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	var likes int64
	if err := a.db.QueryRow(r.Context(),
		`UPDATE live_rooms SET like_count = like_count + 1 WHERE id=$1 AND status='live' RETURNING like_count`,
		roomID).Scan(&likes); err != nil {
		writeErr(w, http.StatusNotFound, "live room not found or already ended")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": roomID, "like_count": likes})
}

// POST /api/live-rooms/{id}/end — host only.
func (a *App) handleEndLiveRoom(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE live_rooms SET status='ended', ended_at=now() WHERE id=$1 AND host_id=$2 AND status='live'`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "live room not found or not yours")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}
