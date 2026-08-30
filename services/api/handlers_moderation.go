package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Media moderation: the API asks the ML service to hash-check post media
// against the blocked list before a post goes live; admins manage the block
// list and the moderation log.

// mlModerateMedia screens media URLs against blocked hashes. Fail-open on
// transport errors (media moderation downtime never blocks posting); a
// "block" decision from the ML service is enforced.
func (a *App) mlModerateMedia(ctx context.Context, urls []string) string {
	if a.cfg.MLServiceURL == "" || len(urls) == 0 {
		return "allow"
	}
	var shaList, dhashList []string
	rows, err := a.db.Query(ctx, `SELECT sha256, dhash FROM blocked_media_hashes`)
	if err != nil {
		return "allow"
	}
	defer rows.Close()
	for rows.Next() {
		var s, d string
		if rows.Scan(&s, &d) == nil {
			if s != "" {
				shaList = append(shaList, s)
			}
			if d != "" {
				dhashList = append(dhashList, d)
			}
		}
	}
	if len(shaList) == 0 && len(dhashList) == 0 {
		return "allow"
	}
	body, _ := json.Marshal(map[string]any{
		"media_urls": urls, "blocked_sha256": shaList, "blocked_dhash": dhashList,
	})
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "POST",
		a.cfg.MLServiceURL+"/moderate-media", bytes.NewReader(body))
	if err != nil {
		return "allow"
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "allow"
	}
	defer resp.Body.Close()
	var out struct {
		Decision string `json:"decision"`
		Results  []struct {
			URL    string `json:"url"`
			SHA256 string `json:"sha256"`
			DHash  string `json:"dhash"`
			Match  string `json:"match"`
		} `json:"results"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return "allow"
	}
	for _, r := range out.Results {
		decision := "allow"
		if r.Match != "" && !strings.HasPrefix(r.Match, "fetch_failed") {
			decision = "block"
		}
		_, _ = a.db.Exec(ctx,
			`INSERT INTO media_moderation (media_url, sha256, dhash, decision, reason)
                         VALUES ($1,$2,$3,$4,$5)`, r.URL, r.SHA256, r.DHash, decision, r.Match)
	}
	return out.Decision
}

// Admin: manage the blocked-media hash list.
func (a *App) handleAdminBlockHash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SHA256 string `json:"sha256"`
		DHash  string `json:"dhash"`
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.SHA256 = strings.ToLower(strings.TrimSpace(req.SHA256))
	req.DHash = strings.ToLower(strings.TrimSpace(req.DHash))
	if (req.SHA256 == "" && req.DHash == "") ||
		(req.SHA256 != "" && len(req.SHA256) != 64) ||
		(req.DHash != "" && len(req.DHash) != 16) {
		writeErr(w, http.StatusBadRequest, "provide a 64-hex sha256 and/or 16-hex dhash")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO blocked_media_hashes (sha256, dhash, reason) VALUES ($1,$2,$3) RETURNING id`,
		req.SHA256, req.DHash, req.Reason).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "hash already blocked")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "block_media_hash", id, map[string]any{"reason": req.Reason})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleAdminListBlockedHashes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, COALESCE(sha256,''), COALESCE(dhash,''), reason, created_at
		 FROM blocked_media_hashes ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	type entry struct {
		ID        string    `json:"id"`
		SHA256    string    `json:"sha256"`
		DHash     string    `json:"dhash"`
		Reason    string    `json:"reason"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.SHA256, &e.DHash, &e.Reason, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hashes": out})
}

func (a *App) handleAdminUnblockHash(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM blocked_media_hashes WHERE id=$1`, r.PathValue("id"))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "hash not found")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "unblock_media_hash", r.PathValue("id"), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unblocked"})
}

func (a *App) handleAdminMediaModeration(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	decision := r.URL.Query().Get("decision")
	q := `SELECT id, media_url, sha256, dhash, decision, reason, created_at
              FROM media_moderation`
	args := []any{}
	if decision == "allow" || decision == "review" || decision == "block" {
		q += ` WHERE decision=$1`
		args = append(args, decision)
	}
	q += ` ORDER BY created_at DESC LIMIT ` + strconv.Itoa(limit) + ` OFFSET ` + strconv.Itoa(offset)
	rows, err := a.db.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load log")
		return
	}
	defer rows.Close()
	type entry struct {
		ID        string    `json:"id"`
		MediaURL  string    `json:"media_url"`
		SHA256    string    `json:"sha256"`
		DHash     string    `json:"dhash"`
		Decision  string    `json:"decision"`
		Reason    string    `json:"reason"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.MediaURL, &e.SHA256, &e.DHash, &e.Decision, &e.Reason, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}
