package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Daily email digest: once every 24h per opted-in user with an email
// address, sends a summary of unread notifications through our configured
// SMTP relay. Idempotent via the email_digests sentinel table; the worker
// is a no-op until SMTP is configured.

func (a *App) startDigestWorker() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			a.sendDigests(context.Background())
		}
	}()
}

func (a *App) sendDigests(ctx context.Context) {
	if !a.smtp.Configured() {
		return
	}
	rows, err := a.db.Query(ctx,
		`SELECT u.id, u.email::text,
                        (SELECT count(*) FROM notifications n
                          WHERE n.user_id=u.id AND n.read_at IS NULL),
                        (SELECT count(*) FROM message_requests mr
                          WHERE mr.recipient_id=u.id AND mr.status='pending'),
                        (SELECT count(*) FROM follows f
                          WHERE f.followee_id=u.id AND f.created_at > now() - interval '24 hours')
                 FROM users u
                 LEFT JOIN email_digests d ON d.user_id = u.id
                 WHERE u.status='active' AND u.digest_enabled AND u.email IS NOT NULL
                   AND (d.sent_at IS NULL OR d.sent_at < now() - interval '24 hours')
                 LIMIT 200`)
	if err != nil {
		return
	}
	defer rows.Close()
	type rec struct {
		id, email                    string
		unread, requests, newFollows int
	}
	var due []rec
	for rows.Next() {
		var r rec
		if rows.Scan(&r.id, &r.email, &r.unread, &r.requests, &r.newFollows) == nil {
			due = append(due, r)
		}
	}
	for _, r := range due {
		if r.unread == 0 && r.requests == 0 && r.newFollows == 0 {
			// Nothing to report; still mark sent so we don't rescan hourly.
			_, _ = a.db.Exec(ctx,
				`INSERT INTO email_digests (user_id, sent_at) VALUES ($1, now())
                                 ON CONFLICT (user_id) DO UPDATE SET sent_at=now()`, r.id)
			continue
		}
		var b strings.Builder
		b.WriteString("Here's what you missed on ChatApp:\n\n")
		if r.unread > 0 {
			fmt.Fprintf(&b, "- %d unread notification(s)\n", r.unread)
		}
		if r.requests > 0 {
			fmt.Fprintf(&b, "- %d pending message request(s)\n", r.requests)
		}
		if r.newFollows > 0 {
			fmt.Fprintf(&b, "- %d new follower(s) in the last 24 hours\n", r.newFollows)
		}
		b.WriteString("\nOpen the app to catch up. To stop these emails, disable the digest in Settings.\n")
		if err := a.smtp.Send(r.email, "Your ChatApp daily digest", b.String()); err != nil {
			continue // leave sentinel untouched so we retry next tick
		}
		_, _ = a.db.Exec(ctx,
			`INSERT INTO email_digests (user_id, sent_at) VALUES ($1, now())
                         ON CONFLICT (user_id) DO UPDATE SET sent_at=now()`, r.id)
	}
}

// PUT /api/me/digest {enabled: bool} — digest opt-in/out.
func (a *App) handleSetDigest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET digest_enabled=$2 WHERE id=$1`, userIDFrom(r), req.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"digest_enabled": req.Enabled})
}
