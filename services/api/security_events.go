package main

import (
	"context"
	"log"
	"net/http"
)

// logSecurityEvent inserts a security event row. Errors are logged via the
// Go log package but are deliberately swallowed at the call site so a
// logging failure never blocks the user-facing operation.
func (a *App) logSecurityEvent(ctx context.Context, event, userID, ip, userAgent, details string) {
	_, err := a.db.Exec(ctx,
		`INSERT INTO security_events (user_id, event, ip_address, user_agent, details, created_at)
		 VALUES ($1, $2, NULLIF($3,'')::inet, NULLIF($4,''), $5, now())`,
		userID, event, ip, userAgent, details)
	if err != nil {
		log.Printf("security_event: %s for %s: %v", event, userID, err)
	}
}

// isNewDevice checks whether the user has previously logged in from this
// IP + user-agent combination (case-insensitive UA, exact IP). Used to
// gate the new-device login notification.
func (a *App) isNewDevice(ctx context.Context, userID, ip, userAgent string) (bool, error) {
	var exists bool
	err := a.db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM security_events
			WHERE user_id = $1 AND event = 'login_success'
			  AND ip_address IS NOT NULL AND host(ip_address) = host($2::inet)
			  AND lower(user_agent) = lower($3)
			LIMIT 1
		)`, userID, ip, userAgent).Scan(&exists)
	return !exists, err
}

// sendNewDeviceNotification sends an email alert when a login occurs from an
// unrecognized device. Errors are logged but never returned to the caller.
func (a *App) sendNewDeviceNotification(ctx context.Context, userID, ip, userAgent string) {
	if !a.smtp.Configured() {
		return
	}
	var email string
	if err := a.db.QueryRow(ctx,
		`SELECT COALESCE(email::text,'') FROM users WHERE id=$1`, userID).Scan(&email); err != nil || email == "" {
		return
	}
	ua := userAgent
	if len(ua) > 120 {
		ua = ua[:120]
	}
	_ = a.smtp.Send(email,
		"ChatApp — new device login",
		"Your ChatApp account was signed into from a new device.\n\n"+
			"IP address: "+ip+"\n"+
			"Device: "+ua+"\n\n"+
			"If this was you, no action is needed. If you do not recognise this "+
			"login, change your password immediately and enable two-factor "+
			"authentication in Settings.")
}

// handleSecurityEvents returns the authenticated user's recent security
// event log (last 50 entries, most recent first).
func (a *App) handleSecurityEvents(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT event, host(ip_address) AS ip, user_agent, details,
		        to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS created_at
		 FROM security_events WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load security events")
		return
	}
	defer rows.Close()
	type entry struct {
		Event     string `json:"event"`
		IP        string `json:"ip"`
		UserAgent string `json:"user_agent"`
		Details   string `json:"details"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]entry, 0, 50)
	for rows.Next() {
		var e entry
		var ip, ua, details *string
		if err := rows.Scan(&e.Event, &ip, &ua, &details, &e.CreatedAt); err != nil {
			continue
		}
		if ip != nil {
			e.IP = *ip
		}
		if ua != nil {
			e.UserAgent = *ua
		}
		if details != nil {
			e.Details = *details
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}
