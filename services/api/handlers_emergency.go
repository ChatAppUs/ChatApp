package main

// handlers_emergency.go — panic button / emergency data wipe implementation.
//
// Anonymous.md §1 "Winning Features" requires:
//   "Panic button / instant local data wipe"
//
// Two tiers:
//   1. SOFT WIPE (POST /api/me/emergency/soft-wipe) — logs out all sessions,
//      revokes all refresh tokens, disables the account, and queues a 48h
//      recovery window. The user receives a recovery link by email.
//   2. HARD WIPE (POST /api/me/emergency/hard-wipe) — permanently deletes
//      all user content, keys, and account data. Irreversible.
//
// Both require the account password or a pre-configured panic code.
// A hard wipe also requires the 2FA code when 2FA is enabled.
// Rate-limited: 1 emergency action per 10 minutes.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"
)

// handleEmergencySoftWipe logs out all sessions and disables the account.
// POST /api/me/emergency/soft-wipe  { password }
func (a *App) handleEmergencySoftWipe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Password string `json:"password"`
		PanicCode string `json:"panic_code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	if !a.rateLimitEmergency(uid) {
		writeErr(w, http.StatusTooManyRequests, "emergency actions are rate-limited; wait 10 minutes")
		return
	}

	// Verify identity: password OR pre-configured panic code.
	if !a.verifyEmergencyCredentials(r.Context(), uid, req.Password, req.PanicCode) {
		writeErr(w, http.StatusForbidden, "invalid credentials for emergency action")
		return
	}

	// 1. Revoke all refresh tokens
	if _, err := a.db.Exec(r.Context(),
		`DELETE FROM refresh_tokens WHERE user_id = $1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}

	// 2. Disable account (soft-disable with recovery window)
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET status = 'disabled', disabled_at = now(), 
		 disabled_reason = 'emergency_soft_wipe' WHERE id = $1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to disable account")
		return
	}

	// 3. Queue recovery notification
	a.queueEmergencyRecovery(uid)

	// 4. Broadcast session termination
	a.fanoutToMembers(r.Context(), uid, []byte(`{"type":"session_terminated","reason":"emergency_soft_wipe"}`), uid)

	// 5. Log security event
	a.logSecurityEvent(r.Context(), uid, "emergency_soft_wipe",
		"all sessions revoked, account disabled, recovery queued")

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "soft_wipe_complete",
		"message": "Account disabled. All sessions terminated. A recovery link has been sent to your email.",
		"recovery_window_hours": 48,
	})
}

// handleEmergencyHardWipe permanently deletes the account and all data.
// POST /api/me/emergency/hard-wipe  { password, totp_code?, confirmation: "DELETE" }
func (a *App) handleEmergencyHardWipe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Password     string `json:"password"`
		PanicCode    string `json:"panic_code"`
		TOTPCode     string `json:"totp_code"`
		Confirmation string `json:"confirmation"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// Require explicit confirmation string
	if req.Confirmation != "DELETE MY ACCOUNT PERMANENTLY" {
		writeErr(w, http.StatusBadRequest,
			`confirmation must be "DELETE MY ACCOUNT PERMANENTLY"`)
		return
	}

	if !a.rateLimitEmergency(uid) {
		writeErr(w, http.StatusTooManyRequests, "emergency actions are rate-limited; wait 10 minutes")
		return
	}

	// Verify identity
	if !a.verifyEmergencyCredentials(r.Context(), uid, req.Password, req.PanicCode) {
		writeErr(w, http.StatusForbidden, "invalid credentials for emergency action")
		return
	}

	// Require 2FA if enabled
	if a.is2FAEnabled(r.Context(), uid) {
		if req.TOTPCode == "" {
			writeErr(w, http.StatusForbidden, "2FA code required for hard wipe")
			return
		}
		var secret string
		if err := a.db.QueryRow(r.Context(),
			`SELECT totp_secret FROM users WHERE id = $1`, uid).Scan(&secret); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to verify 2FA")
			return
		}
		if !a.checkTOTP(secret, req.TOTPCode) {
			writeErr(w, http.StatusForbidden, "invalid 2FA code")
			return
		}
	}

	// 1. Log security event FIRST (before data deletion)
	a.logSecurityEvent(r.Context(), uid, "emergency_hard_wipe",
		"permanent account deletion initiated")

	// 2. Wipe messages
	a.db.Exec(r.Context(), `DELETE FROM messages WHERE sender_id = $1 OR recipient_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM conversation_members WHERE user_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM group_members WHERE user_id = $1`, uid)

	// 3. Wipe social content
	a.db.Exec(r.Context(), `DELETE FROM posts WHERE user_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM comments WHERE user_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM reactions WHERE user_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM stories WHERE user_id = $1`, uid)

	// 4. Wipe profile and account
	a.db.Exec(r.Context(), `DELETE FROM user_sessions WHERE user_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM refresh_tokens WHERE user_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM wallet_transactions WHERE user_id = $1`, uid)
	a.db.Exec(r.Context(), `DELETE FROM mesh_devices WHERE user_id = $1`, uid)

	// 5. Finally, delete the user record
	if _, err := a.db.Exec(r.Context(),
		`DELETE FROM users WHERE id = $1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "hard wipe failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "hard_wipe_complete",
		"message": "Account and all associated data have been permanently deleted.",
	})
}

// handleEmergencyPanicCode configures a panic code for quick emergency access.
// PUT /api/me/emergency/panic-code  { panic_code, password }
func (a *App) handleEmergencyPanicCode(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		PanicCode string `json:"panic_code"`
		Password  string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.PanicCode) < 6 || len(req.PanicCode) > 64 {
		writeErr(w, http.StatusBadRequest, "panic code must be 6-64 characters")
		return
	}

	// Verify password
	var phash string
	if err := a.db.QueryRow(r.Context(),
		`SELECT password_hash FROM users WHERE id = $1`, uid).Scan(&phash); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to verify")
		return
	}
	if !a.checkPassword(phash, req.Password) {
		writeErr(w, http.StatusForbidden, "invalid password")
		return
	}

	// Store hashed panic code
	panicHash := hashPassword(req.PanicCode) // re-use password hashing
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET panic_code_hash = $2 WHERE id = $1`, uid, panicHash); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to set panic code")
		return
	}
	a.logSecurityEvent(r.Context(), uid, "panic_code_set", "emergency panic code configured")
	writeJSON(w, http.StatusOK, map[string]string{"status": "panic_code_set"})
}

// handleGetPanicCodeStatus returns whether a panic code is configured.
// GET /api/me/emergency/panic-code
func (a *App) handleGetPanicCodeStatus(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var panicHash *string
	if err := a.db.QueryRow(r.Context(),
		`SELECT panic_code_hash FROM users WHERE id = $1`, uid).Scan(&panicHash); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": panicHash != nil && *panicHash != "",
	})
}

// ---- helpers ----

var emergencyWindow = map[string]time.Time{}
var emergencyMu sync.Mutex // TODO: use Redis for multi-instance

func (a *App) rateLimitEmergency(uid string) bool {
	emergencyMu.Lock()
	defer emergencyMu.Unlock()
	if last, ok := emergencyWindow[uid]; ok && time.Since(last) < 10*time.Minute {
		return false
	}
	emergencyWindow[uid] = time.Now()
	return true
}

func (a *App) verifyEmergencyCredentials(ctx context.Context, uid, password, panicCode string) bool {
	// Try panic code first
	if panicCode != "" {
		var panicHash string
		if err := a.db.QueryRow(ctx,
			`SELECT panic_code_hash FROM users WHERE id = $1`, uid).Scan(&panicHash); err == nil {
			if a.checkPassword(panicHash, panicCode) {
				return true
			}
		}
	}
	// Fall back to password
	if password != "" {
		var phash string
		if err := a.db.QueryRow(ctx,
			`SELECT password_hash FROM users WHERE id = $1`, uid).Scan(&phash); err == nil {
			return a.checkPassword(phash, password)
		}
	}
	return false
}

func (a *App) is2FAEnabled(ctx context.Context, uid string) bool {
	var enabled bool
	a.db.QueryRow(ctx, `SELECT totp_enabled FROM users WHERE id = $1`, uid).Scan(&enabled)
	return enabled
}

func (a *App) queueEmergencyRecovery(uid string) {
	var email string
	ctx := context.Background()
	if err := a.db.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, uid).Scan(&email); err != nil {
		return
	}
	// Queue recovery email (handled by email worker)
	a.db.Exec(ctx, `INSERT INTO email_queue (recipient, subject, body, priority) 
		VALUES ($1, $2, $3, 'high')`,
		email,
		"Emergency Account Recovery",
		"Your ChatApp account has been disabled via emergency soft wipe. "+
			"You have 48 hours to recover it by logging in with your password. "+
			"If no action is taken, the account will remain disabled.")
}

func (a *App) logSecurityEvent(ctx context.Context, uid, eventType, detail string) {
	a.db.Exec(ctx,
		`INSERT INTO security_events (user_id, event_type, detail, created_at) VALUES ($1, $2, $3, now())`,
		uid, eventType, detail)
}

// ensure imports don't get stripped
var _ = json.Marshal
var _ = subtle.ConstantTimeCompare
