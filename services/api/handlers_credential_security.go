package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

var credentialKinds = map[string]bool{
	"current_email": true, "new_email": true, "current_phone": true, "new_phone": true,
}

func (a *App) handleCredentialChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind        string `json:"kind"`
		Destination string `json:"destination"`
	}
	if !decodeJSON(w, r, &req) || !credentialKinds[req.Kind] {
		writeErr(w, http.StatusBadRequest, "invalid credential challenge kind")
		return
	}
	uid := userIDFrom(r)
	var email, phone string
	if err := a.db.QueryRow(r.Context(), `SELECT COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE id=$1`, uid).Scan(&email, &phone); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load account")
		return
	}
	destination := strings.TrimSpace(req.Destination)
	switch req.Kind {
	case "current_email":
		destination = email
	case "current_phone":
		destination = phone
	case "new_email":
		destination = strings.ToLower(destination)
		if !emailRe.MatchString(destination) {
			writeErr(w, http.StatusBadRequest, "invalid email")
			return
		}
	case "new_phone":
		if !phoneRe.MatchString(destination) {
			writeErr(w, http.StatusBadRequest, "phone must be E.164 format")
			return
		}
	}
	if destination == "" {
		writeErr(w, http.StatusBadRequest, "destination is not configured")
		return
	}
	if req.Kind == "new_email" || req.Kind == "new_phone" {
		var exists bool
		column := "email"
		if req.Kind == "new_phone" {
			column = "phone_e164"
		}
		if err := a.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE "+column+"=$1)", destination).Scan(&exists); err != nil {
			writeErr(w, http.StatusInternalServerError, "availability check failed")
			return
		}
		if exists {
			writeErr(w, http.StatusConflict, "contact is already registered")
			return
		}
	}
	code, salt, hash, err := a.otpMake()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "challenge generation failed")
		return
	}
	_, err = a.db.Exec(r.Context(), `UPDATE credential_change_challenges SET expires_at=now() WHERE user_id=$1 AND kind=$2 AND verified_at IS NULL`, uid, req.Kind)
	if err == nil {
		_, err = a.db.Exec(r.Context(), `INSERT INTO credential_change_challenges (user_id,kind,destination,code_hash,salt,expires_at) VALUES ($1,$2,$3,$4,$5,now()+interval '10 minutes')`, uid, req.Kind, destination, hash, salt)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "challenge storage failed")
		return
	}
	message := fmt.Sprintf("Your ChatApp security verification code is: %s", code)
	if strings.HasSuffix(req.Kind, "email") {
		if !a.smtp.Configured() {
			if a.cfg.AppEnv == "development" {
				writeJSON(w, http.StatusOK, map[string]string{"status": "challenge_created", "dev_code": code})
				return
			}
			writeErr(w, http.StatusBadGateway, "email delivery is not configured")
			return
		}
		if err := a.smtp.Send(destination, "ChatApp security verification", message); err != nil {
			writeErr(w, http.StatusBadGateway, "failed to send email verification")
			return
		}
	} else if err := a.otp.gateway.Deliver(r.Context(), destination, message); err != nil {
		writeErr(w, http.StatusBadGateway, "failed to send phone verification")
		return
	}
	out := map[string]string{"status": "challenge_created"}
	if a.cfg.AppEnv == "development" {
		out["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCredentialChallengeVerify(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !credentialKinds[kind] {
		writeErr(w, http.StatusBadRequest, "invalid challenge kind")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Code) == "" {
		writeErr(w, http.StatusBadRequest, "code required")
		return
	}
	var id, want, salt string
	err := a.db.QueryRow(r.Context(), `SELECT id, code_hash, salt FROM credential_change_challenges WHERE user_id=$1 AND kind=$2 AND verified_at IS NULL AND expires_at>now() AND attempts<5 ORDER BY created_at DESC LIMIT 1`, userIDFrom(r), kind).Scan(&id, &want, &salt)
	if err != nil || a.otpHashOf(salt, strings.TrimSpace(req.Code)) != want {
		if id != "" {
			_, _ = a.db.Exec(r.Context(), `UPDATE credential_change_challenges SET attempts=attempts+1 WHERE id=$1`, id)
		}
		writeErr(w, http.StatusUnauthorized, "invalid or expired verification code")
		return
	}
	res, err := a.db.Exec(r.Context(), `
		UPDATE credential_change_challenges
		SET verified_at=now()
		WHERE id=$1 AND verified_at IS NULL AND expires_at>now() AND attempts<5`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "verification storage failed")
		return
	}
	if res.RowsAffected() != 1 {
		writeErr(w, http.StatusUnauthorized, "invalid or expired verification code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": kind + "_verified"})
}

func claimCredentialVerification(ctx context.Context, tx pgx.Tx, uid, kind, destination string) bool {
	var id string
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM credential_change_challenges
		WHERE user_id=$1 AND kind=$2 AND destination=$3
		  AND verified_at>now()-interval '10 minutes'
		  AND expires_at>now()
		ORDER BY created_at DESC
		LIMIT 1
		FOR UPDATE`, uid, kind, destination).Scan(&id); err != nil {
		return false
	}
	_, err := tx.Exec(ctx, `UPDATE credential_change_challenges SET expires_at=now() WHERE id=$1`, id)
	return err == nil
}

func (a *App) handleCredentialChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Operation       string `json:"operation"`
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		NewEmail        string `json:"new_email"`
		NewPhone        string `json:"new_phone"`
	}
	if !decodeJSON(w, r, &req) || (req.Operation != "password" && req.Operation != "email" && req.Operation != "phone") {
		writeErr(w, http.StatusBadRequest, "operation must be password, email, or phone")
		return
	}
	if req.Operation == "email" {
		req.NewEmail = strings.ToLower(strings.TrimSpace(req.NewEmail))
		if !emailRe.MatchString(req.NewEmail) {
			writeErr(w, http.StatusBadRequest, "invalid email")
			return
		}
	}
	if req.Operation == "phone" {
		req.NewPhone = strings.TrimSpace(req.NewPhone)
		if !phoneRe.MatchString(req.NewPhone) {
			writeErr(w, http.StatusBadRequest, "phone must be E.164 format")
			return
		}
	}
	if req.Operation == "password" && !validPassword(req.NewPassword) {
		writeErr(w, http.StatusBadRequest, "password must be 8+ chars with letters and digits")
		return
	}

	uid := userIDFrom(r)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "security update failed")
		return
	}
	defer tx.Rollback(r.Context())

	var hash, email, phone string
	if err := tx.QueryRow(r.Context(), `
		SELECT password_hash,COALESCE(email::text,''),COALESCE(phone_e164,'')
		FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&hash, &email, &phone); err != nil || !a.passwordVerify(req.CurrentPassword, hash) {
		writeErr(w, http.StatusUnauthorized, "current password is invalid")
		return
	}
	if !claimSecurityAttestation(r.Context(), tx, uid) {
		writeErr(w, http.StatusForbidden, "fresh face-match/liveness attestation required")
		return
	}

	switch req.Operation {
	case "email":
		if !claimCredentialVerification(r.Context(), tx, uid, "current_email", email) ||
			!claimCredentialVerification(r.Context(), tx, uid, "new_email", req.NewEmail) {
			writeErr(w, http.StatusForbidden, "current and new email verification required")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE users SET email=$2,updated_at=now() WHERE id=$1`, uid, req.NewEmail); err != nil {
			writeErr(w, http.StatusConflict, "email update failed")
			return
		}
	case "phone":
		if !claimCredentialVerification(r.Context(), tx, uid, "current_phone", phone) ||
			!claimCredentialVerification(r.Context(), tx, uid, "new_phone", req.NewPhone) {
			writeErr(w, http.StatusForbidden, "current and new phone verification required")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE users SET phone_e164=$2,updated_at=now() WHERE id=$1`, uid, req.NewPhone); err != nil {
			writeErr(w, http.StatusConflict, "phone update failed")
			return
		}
	case "password":
		kind, destination := "current_email", email
		if destination == "" {
			kind, destination = "current_phone", phone
		}
		if !claimCredentialVerification(r.Context(), tx, uid, kind, destination) {
			writeErr(w, http.StatusForbidden, "current contact verification required")
			return
		}
		newHash, err := a.passwordHash(req.NewPassword)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "password hashing failed")
			return
		}
		if _, err := tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,updated_at=now() WHERE id=$1`, uid, newHash); err != nil {
			writeErr(w, http.StatusInternalServerError, "password update failed")
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "session revocation failed")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE users
		SET withdrawal_freeze_until = GREATEST(COALESCE(withdrawal_freeze_until, now()), now() + interval '48 hours'), updated_at=now()
		WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "security cooldown failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "security update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "security_updated"})
}
