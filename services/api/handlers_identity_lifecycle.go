package main

import (
	"context"
	"net/http"
	"time"
)

// handleDeletionStatus reports the current deletion grace-period state.
func (a *App) handleDeletionStatus(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var status string
	var scheduled *time.Time
	if err := a.db.QueryRow(r.Context(),
		`SELECT status, deletion_scheduled_at FROM users WHERE id=$1`, uid).
		Scan(&status, &scheduled); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load deletion status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         status,
		"pending":        scheduled != nil && scheduled.After(time.Now()),
		"scheduled_at":   scheduled,
		"grace_period_days": 30,
	})
}

// handleRequestDeletion requires a current password, explicit asset
// confirmation, and an approved KYC identity before starting the 30-day grace
// period. The account remains recoverable until the scheduled timestamp.
func (a *App) handleRequestDeletion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password       string `json:"password"`
		AssetsWithdrawn bool   `json:"assets_withdrawn"`
	}
	if !decodeJSON(w, r, &req) || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "password required")
		return
	}
	if !req.AssetsWithdrawn {
		writeErr(w, http.StatusBadRequest, "confirm that all assets have been withdrawn")
		return
	}
	uid := userIDFrom(r)
	var hash, kyc, status string
	if err := a.db.QueryRow(r.Context(),
		`SELECT password_hash, kyc_status, status FROM users WHERE id=$1`, uid).
		Scan(&hash, &kyc, &status); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load account")
		return
	}
	if !a.passwordVerify(req.Password, hash) {
		writeErr(w, http.StatusUnauthorized, "invalid password")
		return
	}
	if kyc != "verified" {
		writeErr(w, http.StatusForbidden, "approved KYC verification is required before account deletion")
		return
	}
	if status == "deleted" {
		writeErr(w, http.StatusGone, "account permanently deleted")
		return
	}
	var scheduled time.Time
	if err := a.db.QueryRow(r.Context(), `
		UPDATE users
		SET status='suspended', deletion_requested_at=now(),
		    deletion_scheduled_at=now() + interval '30 days', updated_at=now()
		WHERE id=$1 AND status <> 'deleted'
		RETURNING deletion_scheduled_at`, uid).Scan(&scheduled); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to schedule account deletion")
		return
	}
	_, _ = a.db.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "deletion_pending", "scheduled_at": scheduled,
		"grace_period_days": 30,
	})
}

// handleCancelDeletion is intentionally authenticated. A login during the
// grace period uses the same operation after credentials are accepted.
func (a *App) handleCancelDeletion(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	res, err := a.db.Exec(r.Context(), `
		UPDATE users SET status='active', deletion_requested_at=NULL,
		       deletion_scheduled_at=NULL, updated_at=now()
		WHERE id=$1 AND status='suspended' AND deletion_scheduled_at > now()`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to cancel deletion")
		return
	}
	if res.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, "no cancellable deletion request")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deletion_cancelled"})
}

// startDeletionWorker permanently removes accounts after the grace period.
// Foreign keys use ON DELETE CASCADE/SET NULL, so the cleanup is atomic at the
// identity root and does not leave recoverable credentials behind.
func (a *App) startDeletionWorker() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			_, _ = a.db.Exec(context.Background(), `
				DELETE FROM users
				WHERE status='suspended' AND deletion_scheduled_at IS NOT NULL
				  AND deletion_scheduled_at <= now()`)
		}
	}()
}
