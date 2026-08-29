package main

import (
	"net/http"
	"time"
)

// Telegram-style QR login: a signed-out device (e.g. desktop web) displays a
// QR code encoding `chatapp://login?token=...`; a signed-in device scans it
// and approves; the signed-out device polls and receives session tokens.

const qrLoginTTL = 2 * time.Minute

// POST /api/auth/qr/new — unauthenticated. Returns the token to encode.
func (a *App) handleQRLoginNew(w http.ResponseWriter, r *http.Request) {
	token, err := randomToken(32)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO qr_login_tokens (token, expires_at, created_ip)
		 VALUES ($1, now() + interval '2 minutes', NULLIF($2,'')::inet)`,
		token, clientIP(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      token,
		"expires_in": int(qrLoginTTL.Seconds()),
		"url":        "chatapp://login?token=" + token,
	})
}

// GET /api/auth/qr/{token} — polled by the signed-out device.
// Returns status; when approved, also returns session tokens (once).
func (a *App) handleQRLoginStatus(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	var status string
	var userID *string
	var expires time.Time
	err := a.db.QueryRow(r.Context(),
		`SELECT status, user_id, expires_at FROM qr_login_tokens WHERE token=$1`,
		token).Scan(&status, &userID, &expires)
	if err != nil {
		writeErr(w, http.StatusNotFound, "unknown token")
		return
	}
	if time.Now().After(expires) && status == "pending" {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE qr_login_tokens SET status='expired' WHERE token=$1`, token)
		writeJSON(w, http.StatusOK, map[string]any{"status": "expired"})
		return
	}
	if status != "approved" || userID == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": status})
		return
	}
	// One-shot consumption: only the first poll after approval gets tokens.
	res, err := a.db.Exec(r.Context(),
		`UPDATE qr_login_tokens SET status='consumed'
		 WHERE token=$1 AND status='approved'`, token)
	if err != nil || res.RowsAffected() == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"status": "consumed"})
		return
	}
	tokens, err := a.issueTokens(r.Context(), *userID, r.UserAgent(), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	tokens["status"] = "approved"
	writeJSON(w, http.StatusOK, tokens)
}

// POST /api/auth/qr/{token}/approve — called by the signed-in (scanning) device.
func (a *App) handleQRLoginApprove(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	res, err := a.db.Exec(r.Context(),
		`UPDATE qr_login_tokens SET status='approved', user_id=$2, approved_ip=NULLIF($3,'')::inet
		 WHERE token=$1 AND status='pending' AND expires_at > now()`,
		token, userIDFrom(r), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to approve")
		return
	}
	if res.RowsAffected() == 0 {
		writeErr(w, http.StatusGone, "token expired or already used")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/auth/qr/{token}/reject — scanning device declines the login.
func (a *App) handleQRLoginReject(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`UPDATE qr_login_tokens SET status='expired'
		 WHERE token=$1 AND status='pending'`, r.PathValue("token"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
