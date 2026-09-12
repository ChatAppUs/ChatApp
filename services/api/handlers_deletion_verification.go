package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var deletionLivenessInstructions = []string{
	"turn your head left", "turn your head right", "blink twice", "smile",
}

func (a *App) createDeletionChallenge(ctx context.Context, uid, kind string) (string, string, error) {
	code, salt, hash, err := a.otpMake()
	if err != nil {
		return "", "", err
	}
	instruction := ""
	if kind == "liveness" {
		instruction = deletionLivenessInstructions[time.Now().UnixNano()%int64(len(deletionLivenessInstructions))]
	}
	_, err = a.db.Exec(ctx, `
		UPDATE deletion_verification_challenges SET expires_at=now()
		WHERE user_id=$1 AND kind=$2 AND verified_at IS NULL`, uid, kind)
	if err != nil {
		return "", "", err
	}
	_, err = a.db.Exec(ctx, `
		INSERT INTO deletion_verification_challenges
		(user_id, kind, code_hash, salt, instruction, expires_at)
		VALUES ($1,$2,$3,$4,$5,now()+interval '10 minutes')`,
		uid, kind, hash, salt, instruction)
	return code, instruction, err
}

// handleDeletionChallenge starts a server-owned challenge. Email and phone
// codes are delivered through configured gateways. Development responses may
// expose a code only when the existing development mode explicitly permits it.
func (a *App) handleDeletionChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind      string `json:"kind"`
		SelfieURL string `json:"selfie_url"`
	}
	if !decodeJSON(w, r, &req) || (req.Kind != "email" && req.Kind != "phone" && req.Kind != "liveness") {
		writeErr(w, http.StatusBadRequest, "kind must be email, phone, or liveness")
		return
	}
	uid := userIDFrom(r)
	var email, phone string
	if err := a.db.QueryRow(r.Context(), `SELECT COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE id=$1`, uid).Scan(&email, &phone); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load account contacts")
		return
	}
	if req.Kind == "email" && email == "" || req.Kind == "phone" && phone == "" {
		writeErr(w, http.StatusBadRequest, "requested contact is not configured")
		return
	}
	code, instruction, err := a.createDeletionChallenge(r.Context(), uid, req.Kind)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create verification challenge")
		return
	}
	if req.Kind == "email" {
		if !a.smtp.Configured() {
			if a.cfg.AppEnv == "development" {
				writeJSON(w, http.StatusOK, map[string]string{"status": "challenge_created", "dev_code": code})
				return
			}
			writeErr(w, http.StatusBadGateway, "email delivery is not configured")
			return
		}
		if err := a.smtp.Send(email, "ChatApp account deletion verification", fmt.Sprintf("Your account deletion verification code is %s. It expires in 10 minutes.", code)); err != nil {
			writeErr(w, http.StatusBadGateway, "failed to send email verification")
			return
		}
	}
	if req.Kind == "phone" {
		if err := a.otp.gateway.Deliver(r.Context(), phone, fmt.Sprintf("Your ChatApp account deletion verification code is: %s", code)); err != nil {
			writeErr(w, http.StatusBadGateway, "failed to send phone verification")
			return
		}
	}
	if req.Kind == "liveness" {
		if strings.TrimSpace(req.SelfieURL) == "" {
			writeErr(w, http.StatusBadRequest, "fresh selfie_url required for liveness verification")
			return
		}
		var docURL, fullName, docType, docNumber string
		if err := a.db.QueryRow(r.Context(), `
			SELECT doc_image_url, full_name, doc_type, doc_number
			FROM kyc_submissions WHERE user_id=$1 AND status='verified'
			ORDER BY reviewed_at DESC NULLS LAST, created_at DESC LIMIT 1`, uid).
			Scan(&docURL, &fullName, &docType, &docNumber); err != nil || docURL == "" {
			writeErr(w, http.StatusForbidden, "verified KYC document is required for liveness verification")
			return
		}
		score, rawChecks := a.mlKYCVerify(r.Context(), kycVerifyRequest{
			FullName: fullName, DocType: docType, DocNumber: docNumber,
			DocImageURL: docURL, SelfieURL: req.SelfieURL,
		})
		var checks map[string]any
		_ = json.Unmarshal(rawChecks, &checks)
		if score < 0.75 || checks["face_match_ok"] != true || checks["selfie_decodable"] != true {
			writeErr(w, http.StatusUnauthorized, "liveness or face-match verification failed")
			return
		}
		// The ML service is the server-side verifier. Store a short-lived
		// verified challenge; no client-supplied boolean can bypass this gate.
		_, _, err := a.createDeletionChallenge(r.Context(), uid, "liveness")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to record liveness verification")
			return
		}
		if _, err := a.db.Exec(r.Context(), `
			UPDATE deletion_verification_challenges SET verified_at=now()
			WHERE id=(SELECT id FROM deletion_verification_challenges
			          WHERE user_id=$1 AND kind='liveness' AND verified_at IS NULL
			          ORDER BY created_at DESC LIMIT 1)`, uid); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to record liveness verification")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "liveness_verified", "score": score})
		return
	}
	out := map[string]string{"status": "challenge_created"}
	if req.Kind == "liveness" {
		out["instruction"] = instruction
	}
	if a.cfg.AppEnv == "development" {
		out["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleDeletionChallengeVerify(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "email" && kind != "phone" && kind != "liveness" {
		writeErr(w, http.StatusBadRequest, "invalid challenge kind")
		return
	}
	var req struct{ Code string `json:"code"` }
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Code) == "" {
		writeErr(w, http.StatusBadRequest, "code required")
		return
	}
	uid := userIDFrom(r)
	var id, wantHash, salt string
	err := a.db.QueryRow(r.Context(), `
		SELECT id, code_hash, salt FROM deletion_verification_challenges
		WHERE user_id=$1 AND kind=$2 AND verified_at IS NULL AND expires_at > now() AND attempts < 5
		ORDER BY created_at DESC LIMIT 1`, uid, kind).Scan(&id, &wantHash, &salt)
	if err != nil || a.otpHashOf(salt, strings.TrimSpace(req.Code)) != wantHash {
		if id != "" {
			_, _ = a.db.Exec(r.Context(), `UPDATE deletion_verification_challenges SET attempts=attempts+1 WHERE id=$1`, id)
		}
		writeErr(w, http.StatusUnauthorized, "invalid or expired verification code")
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE deletion_verification_challenges SET verified_at=now() WHERE id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record verification")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": kind + "_verified"})
}

func (a *App) deletionChallengeVerified(ctx context.Context, uid, kind string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM deletion_verification_challenges WHERE user_id=$1 AND kind=$2 AND verified_at > now()-interval '10 minutes')`, uid, kind).Scan(&ok)
	return ok
}
