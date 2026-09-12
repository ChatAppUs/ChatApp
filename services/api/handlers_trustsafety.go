package main

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Content-level trust & safety.
//
// The auth surface already has per-IP token-bucket rate limiting. This layer
// adds per-account content abuse defense on the write paths (posts, comments,
// messages) that the auth limiters never touch:
//
//   - duplicate detection: identical body within a short window is rejected
//     (copy-paste spam / retry storms).
//   - link spam: a per-user cap on how many distinct external links can be
//     posted in a window, blunting URL-bombing.
//
// All checks are fail-open on DB errors (a transient DB hiccup must never
// block legitimate posting), matching the existing moderation philosophy.

// contentWriteAllowed enforces the per-user content write budget.
// Returns (allowed bool, reason string).
func (a *App) contentWriteAllowed(ctx context.Context, userID, body string) (bool, string) {
	if strings.TrimSpace(body) == "" {
		return true, ""
	}
	// Duplicate detection: identical body posted within the last 60s.
	var dup bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM content_abuse_marks
		   WHERE user_id=$1 AND body_hash=md5($2) AND created_at > now() - interval '60 seconds'
		 )`, userID, body).Scan(&dup)
	if dup {
		return false, "duplicate content — slow down"
	}
	// Link spam: cap distinct external links per user per 10 minutes.
	links := extractLinks(body)
	if len(links) > 0 {
		var recent int
		_ = a.db.QueryRow(ctx,
			`SELECT count(*) FROM content_abuse_marks
			 WHERE user_id=$1 AND link_count>0 AND created_at > now() - interval '10 minutes'`,
			userID).Scan(&recent)
		if recent+len(links) > 20 {
			return false, "too many links — slow down"
		}
	}
	// Record the mark (idempotent-ish; a new row per write).
	_, _ = a.db.Exec(ctx,
		`INSERT INTO content_abuse_marks (user_id, body_hash, link_count) VALUES ($1, md5($2), $3)`,
		userID, body, len(links))
	return true, ""
}

// extractLinks returns the distinct http(s) URLs in a body.
func extractLinks(body string) []string {
	seen := map[string]bool{}
	var out []string
	re := regexp.MustCompile(`https?://[^ \t\n\r"'<>]+`)
	for _, m := range re.FindAllString(body, -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// ---- Admin: view the content-abuse log ----

func (a *App) handleAdminContentAbuse(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, user_id, body_hash, link_count, created_at
		 FROM content_abuse_marks ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load log")
		return
	}
	defer rows.Close()
	type entry struct {
		ID        int64     `json:"id"`
		UserID    string    `json:"user_id"`
		BodyHash  string    `json:"body_hash"`
		LinkCount int       `json:"link_count"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.UserID, &e.BodyHash, &e.LinkCount, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}
