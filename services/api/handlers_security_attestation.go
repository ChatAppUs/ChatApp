package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func (a *App) handleSecurityAttestation(w http.ResponseWriter, r *http.Request) {
	var req struct{ SelfieURL string `json:"selfie_url"` }
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.SelfieURL) == "" {
		writeErr(w, http.StatusBadRequest, "fresh selfie_url required")
		return
	}
	uid := userIDFrom(r)
	var docURL, fullName, docType, docNumber string
	if err := a.db.QueryRow(r.Context(), `
		SELECT doc_image_url, full_name, doc_type, doc_number
		FROM kyc_submissions WHERE user_id=$1 AND status='verified'
		ORDER BY reviewed_at DESC NULLS LAST, created_at DESC LIMIT 1`, uid).
		Scan(&docURL, &fullName, &docType, &docNumber); err != nil || docURL == "" {
		writeErr(w, http.StatusForbidden, "verified KYC document is required")
		return
	}
	score, rawChecks := a.mlKYCVerify(r.Context(), kycVerifyRequest{
		FullName: fullName, DocType: docType, DocNumber: docNumber,
		DocImageURL: docURL, SelfieURL: req.SelfieURL,
	})
	var checks map[string]any
	_ = json.Unmarshal(rawChecks, &checks)
	if score < 0.75 || checks["face_match_ok"] != true || checks["selfie_decodable"] != true {
		writeErr(w, http.StatusUnauthorized, "face-match or liveness verification failed")
		return
	}
	if _, err := a.db.Exec(r.Context(), `
		INSERT INTO security_attestations (user_id,purpose,score,checks,expires_at)
		VALUES ($1,'credential_change',$2,$3,now()+interval '5 minutes')`,
		uid, score, rawChecks); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store security attestation")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "attested", "expires_in_seconds": 300, "score": score})
}

func (a *App) securityAttested(ctx context.Context, uid string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM security_attestations WHERE user_id=$1 AND purpose='credential_change' AND expires_at>now())`, uid).Scan(&ok)
	return ok
}
