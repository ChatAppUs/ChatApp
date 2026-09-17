package main

// Identity, Authentication & Account Security spec gaps:
//   §3.1 item 2  — account-existence check with redirect to Sign Up
//   §3.1 item 5  — trusted-device 30-day passwordless login
//   §5           — reset-password verification bundle (email OTP / phone OTP / 2FA)
//   §6           — dedicated 2FA reset flow (OTP email+phone → KYC face/liveness → new 2FA)
//   §7.3         — random-instruction liveness challenges for KYC

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// cryptoRandIntn returns a uniform random int in [0, n) for challenge selection.
func cryptoRandIntn(n int) int {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int(binary.BigEndian.Uint64(b[:]) % uint64(n))
}

func subtleConstantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ---- §3.1 item 2: account-existence probe ----
// The unified smart input checks the identifier before showing the password
// step; an unregistered identifier is redirected to Sign Up. Rate-limited at
// the route layer to blunt enumeration (the spec's §3.1 flow intentionally
// surfaces existence to the real user, same as Telegram/WhatsApp).

type identifierCheckReq struct {
	Identifier string `json:"identifier"`
}

func classifyIdentifier(id string) string {
	switch {
	case phoneRe.MatchString(id):
		return "phone"
	case strings.Contains(id, "@"):
		return "email"
	default:
		return "username"
	}
}

func (a *App) handleIdentifierCheck(w http.ResponseWriter, r *http.Request) {
	var req identifierCheckReq
	if !decodeJSON(w, r, &req) {
		return
	}
	id := strings.TrimSpace(req.Identifier)
	if id == "" {
		writeErr(w, http.StatusBadRequest, "identifier required")
		return
	}
	mode := classifyIdentifier(id)
	var exists bool
	var err error
	switch mode {
	case "phone":
		err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE phone_e164=$1)`, id).Scan(&exists)
	case "email":
		err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1))`, id).Scan(&exists)
	default:
		err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE username=$1)`, id).Scan(&exists)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"exists": exists, "mode": mode})
}

// ---- §3.1 item 5: trusted-device 30-day passwordless login ----

const deviceTrustTTL = 30 * 24 * time.Hour

// handleTrustedDeviceEnroll mints a 30-day device trust token for the current
// authenticated session. The raw token is returned once; only its SHA-256
// hash is persisted.
func (a *App) handleTrustedDeviceEnroll(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	token, err := a.randomNum(32)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	var name string
	var req struct {
		DeviceName string `json:"device_name"`
	}
	if decodeJSON(w, r, &req) {
		name = strings.TrimSpace(req.DeviceName)
	}
	if len(name) > 120 {
		name = name[:120]
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO device_trusts (user_id, token_hash, device_name, ip, user_agent, expires_at)
		 VALUES ($1,$2,$3,$4,$5, now() + interval '30 days')`,
		uid, sha256hex(token), name, clientIP(r), r.UserAgent()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enroll device")
		return
	}
	a.logSecurityEvent(r.Context(), "device_trust_enrolled", uid, clientIP(r), r.UserAgent(), name)
	writeJSON(w, http.StatusCreated, map[string]any{
		"device_token":       token,
		"expires_in_seconds": int(deviceTrustTTL.Seconds()),
	})
}

// handleTrustedDeviceLogin exchanges a valid trust token for a fresh session
// without a password. TOTP 2FA (or a one-time recovery code) is still
// enforced when the user opted into a second factor.
func (a *App) handleTrustedDeviceLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceToken string `json:"device_token"`
		TOTPCode    string `json:"totp_code"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.DeviceToken) == "" {
		writeErr(w, http.StatusBadRequest, "device_token required")
		return
	}
	uid, status, deletionScheduled, totpEnabled, totpSecret, err := a.consumeDeviceTrust(r.Context(), req.DeviceToken)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid or expired device token")
		return
	}
	if status == "suspended" && deletionScheduled != nil && deletionScheduled.After(time.Now()) {
		// Login during the deletion grace period cancels the pending deletion.
		if _, err := a.db.Exec(r.Context(), `UPDATE users SET status='active', deletion_requested_at=NULL, deletion_scheduled_at=NULL, updated_at=now() WHERE id=$1`, uid); err == nil {
			status = "active"
		}
	}
	if status != "active" {
		writeErr(w, http.StatusForbidden, "account is "+status)
		return
	}
	if totpEnabled {
		if req.TOTPCode == "" {
			writeErr(w, http.StatusUnauthorized, "totp_required")
			return
		}
		ok := totpSecret != nil && a.checkTOTP(*totpSecret, req.TOTPCode)
		if !ok {
			ok = a.verifyRecoveryCode(uid, req.TOTPCode)
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid 2FA code")
			return
		}
	}
	tokens, err := a.issueTokens(r.Context(), uid, r.UserAgent(), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	a.logSecurityEvent(r.Context(), "login_trusted_device", uid, clientIP(r), r.UserAgent(), "")
	if isNew, _ := a.isNewDevice(r.Context(), uid, clientIP(r), r.UserAgent()); isNew {
		a.sendNewDeviceNotification(r.Context(), uid, clientIP(r), r.UserAgent())
	}
	tokens["user_id"] = uid
	writeJSON(w, http.StatusOK, tokens)
}

// consumeDeviceTrust validates a trust token atomically: the row must be
// unrevoked, unexpired, and owned by an active account; last_used_at is
// bumped on success.
func (a *App) consumeDeviceTrust(ctx context.Context, token string) (uid, status string, deletionScheduled *time.Time, totpEnabled bool, totpSecret *string, err error) {
	err = a.db.QueryRow(ctx, `
		UPDATE device_trusts SET last_used_at=now()
		 WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > now()
		 RETURNING user_id`,
		sha256hex(token)).Scan(&uid)
	if err != nil {
		return "", "", nil, false, nil, err
	}
	err = a.db.QueryRow(ctx, `
		SELECT status, deletion_scheduled_at, totp_enabled, totp_secret
		 FROM users WHERE id=$1`, uid).
		Scan(&status, &deletionScheduled, &totpEnabled, &totpSecret)
	if err != nil {
		return "", "", nil, false, nil, err
	}
	return uid, status, deletionScheduled, totpEnabled, totpSecret, nil
}

// handleTrustedDeviceRevoke lets the user drop every trusted device (e.g.
// after losing a laptop). Revocation is also wired into password reset and
// credential changes so stolen trust tokens die with the credentials.
func (a *App) handleTrustedDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	tag, err := a.db.Exec(r.Context(),
		`UPDATE device_trusts SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to revoke trusted devices")
		return
	}
	a.logSecurityEvent(r.Context(), "device_trusts_revoked", uid, clientIP(r), r.UserAgent(), "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "count": tag.RowsAffected()})
}

// revokeDeviceTrustsTx is the in-transaction variant used by password reset
// and credential changes.
func revokeDeviceTrustsTx(ctx context.Context, tx pgx.Tx, uid string) error {
	_, err := tx.Exec(ctx, `UPDATE device_trusts SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid)
	return err
}

// ---- §5: reset-password verification bundle ----
// Alternatives to the emailed reset link: after the unified-input existence
// check, the user may prove control of the account with an email OTP, a phone
// OTP, or (when 2FA is on) the authenticator / a recovery code. Success mints
// the same single-use reset token consumed by POST /api/auth/reset-password.

func (a *App) handleResetVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Identifier string `json:"identifier"`
		Method     string `json:"method"` // email_otp | phone_otp | totp
		Code       string `json:"code"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Code) == "" {
		writeErr(w, http.StatusBadRequest, "identifier and code required")
		return
	}
	id := strings.TrimSpace(req.Identifier)
	mode := classifyIdentifier(id)
	var userID, email, phone string
	var q string
	switch mode {
	case "phone":
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE phone_e164=$1`
	case "email":
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE lower(email)=lower($1)`
	default:
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE username=$1`
	}
	if err := a.db.QueryRow(r.Context(), q, id).Scan(&userID, &email, &phone); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid verification")
		return
	}
	switch req.Method {
	case "email_otp":
		if email == "" {
			writeErr(w, http.StatusBadRequest, "account has no email to verify")
			return
		}
		var vid, wantHash, salt string
		err := a.db.QueryRow(r.Context(), `
			SELECT id, code_hash, COALESCE(salt,'') FROM email_verifications
			 WHERE email=$1 AND verified_at IS NULL AND expires_at > now() AND attempts < 5
			 ORDER BY created_at DESC LIMIT 1`, email).Scan(&vid, &wantHash, &salt)
		if err != nil || subtleConstantTimeEqual(a.otpHashOf(salt, strings.TrimSpace(req.Code)), wantHash) == false {
			if vid != "" {
				_, _ = a.db.Exec(r.Context(), `UPDATE email_verifications SET attempts=attempts+1 WHERE id=$1`, vid)
			}
			writeErr(w, http.StatusUnauthorized, "invalid verification code")
			return
		}
		if _, err := a.db.Exec(r.Context(), `UPDATE email_verifications SET verified_at=now() WHERE id=$1 AND verified_at IS NULL`, vid); err != nil {
			writeErr(w, http.StatusInternalServerError, "verification failed")
			return
		}
	case "phone_otp":
		if phone == "" {
			writeErr(w, http.StatusBadRequest, "account has no phone to verify")
			return
		}
		ok, err := a.otp.CheckCode(phone, strings.TrimSpace(req.Code))
		if err != nil || !ok {
			writeErr(w, http.StatusUnauthorized, "invalid verification code")
			return
		}
	case "totp":
		var totpEnabled bool
		var totpSecret *string
		if err := a.db.QueryRow(r.Context(), `SELECT totp_enabled, totp_secret FROM users WHERE id=$1`, userID).
			Scan(&totpEnabled, &totpSecret); err != nil {
			writeErr(w, http.StatusInternalServerError, "verification failed")
			return
		}
		if !totpEnabled {
			writeErr(w, http.StatusBadRequest, "2FA is not enabled on this account")
			return
		}
		valid := totpSecret != nil && a.checkTOTP(*totpSecret, req.Code)
		if !valid {
			valid = a.verifyRecoveryCode(userID, req.Code)
		}
		if !valid {
			writeErr(w, http.StatusUnauthorized, "invalid 2FA code")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "method must be email_otp, phone_otp, or totp")
		return
	}
	token, err := a.randomNum(32)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO password_resets (user_id, token_hash, expires_at) VALUES ($1,$2, now() + interval '1 hour')`,
		userID, sha256hex(token)); err != nil {
		writeErr(w, http.StatusInternalServerError, "reset creation failed")
		return
	}
	a.logSecurityEvent(r.Context(), "reset_verified_"+req.Method, userID, clientIP(r), r.UserAgent(), "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified", "reset_token": token})
}

// ---- §6: dedicated 2FA reset flow ----
// begin: unified-input identifier → OTP to BOTH the account email and phone.
// verify: both OTPs + KYC face-match/liveness attestation.
// complete: old 2FA removed, sessions/device-trusts/recovery-codes revoked,
// and a fresh pending TOTP secret is returned for the new setup.

func (a *App) handle2FAResetBegin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Identifier string `json:"identifier"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := strings.TrimSpace(req.Identifier)
	var userID, email, phone string
	var q string
	switch classifyIdentifier(id) {
	case "phone":
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE phone_e164=$1`
	case "email":
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE lower(email)=lower($1)`
	default:
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,'') FROM users WHERE username=$1`
	}
	if err := a.db.QueryRow(r.Context(), q, id).Scan(&userID, &email, &phone); err != nil {
		// Do not reveal account existence.
		writeJSON(w, http.StatusOK, map[string]string{"status": "if the account exists, codes were sent"})
		return
	}
	var totpEnabled bool
	_ = a.db.QueryRow(r.Context(), `SELECT totp_enabled FROM users WHERE id=$1`, userID).Scan(&totpEnabled)
	if !totpEnabled {
		writeJSON(w, http.StatusOK, map[string]string{"status": "if the account exists, codes were sent"})
		return
	}
	if email == "" && phone == "" {
		writeErr(w, http.StatusUnprocessableEntity, "account has no verified contact channel for 2FA reset")
		return
	}
	out := map[string]string{"status": "if the account exists, codes were sent"}
	sent := false
	if email != "" {
		code, salt, hash, err := a.otpMake()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to generate code")
			return
		}
		if _, err := a.db.Exec(r.Context(), `
			INSERT INTO twofa_reset_challenges (user_id, kind, destination, code_hash, salt, expires_at)
			VALUES ($1,'email',$2,$3,$4, now() + interval '10 minutes')`, userID, email, hash, salt); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to store challenge")
			return
		}
		if a.smtp.Configured() {
			if err := a.smtp.Send(email, "ChatApp 2FA reset", "Your ChatApp 2FA reset code is: "+code); err != nil {
				writeErr(w, http.StatusBadGateway, "failed to send email code")
				return
			}
			sent = true
		} else if a.cfg.AppEnv == "development" {
			out["dev_email_code"] = code
			sent = true
		}
	}
	if phone != "" {
		code, salt, hash, err := a.otpMake()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to generate code")
			return
		}
		if _, err := a.db.Exec(r.Context(), `
			INSERT INTO twofa_reset_challenges (user_id, kind, destination, code_hash, salt, expires_at)
			VALUES ($1,'phone',$2,$3,$4, now() + interval '10 minutes')`, userID, phone, hash, salt); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to store challenge")
			return
		}
		message := "Your ChatApp 2FA reset code is: " + code
		if err := a.otp.gateway.Deliver(r.Context(), phone, message); err != nil {
			if !sent && a.cfg.AppEnv != "development" {
				writeErr(w, http.StatusBadGateway, "failed to send phone code")
				return
			}
		} else {
			sent = true
		}
		if a.cfg.AppEnv == "development" {
			out["dev_phone_code"] = code
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) verify2FAResetCode(w http.ResponseWriter, r *http.Request, uid, kind, code string) bool {
	var id, want, salt string
	err := a.db.QueryRow(r.Context(), `
		SELECT id, code_hash, COALESCE(salt,'') FROM twofa_reset_challenges
		 WHERE user_id=$1 AND kind=$2 AND verified_at IS NULL AND expires_at > now() AND attempts < 5
		 ORDER BY created_at DESC LIMIT 1`, uid, kind).Scan(&id, &want, &salt)
	if err != nil || subtleConstantTimeEqual(a.otpHashOf(salt, strings.TrimSpace(code)), want) == false {
		if id != "" {
			_, _ = a.db.Exec(r.Context(), `UPDATE twofa_reset_challenges SET attempts=attempts+1 WHERE id=$1`, id)
		}
		writeErr(w, http.StatusUnauthorized, "invalid or expired "+kind+" code")
		return false
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE twofa_reset_challenges SET verified_at=now()
		 WHERE id=$1 AND verified_at IS NULL AND expires_at > now() AND attempts < 5`, id)
	if err != nil || tag.RowsAffected() != 1 {
		writeErr(w, http.StatusUnauthorized, "invalid or expired "+kind+" code")
		return false
	}
	return true
}

func (a *App) handle2FAResetVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Identifier string `json:"identifier"`
		EmailCode  string `json:"email_code"`
		PhoneCode  string `json:"phone_code"`
		SelfieURL  string `json:"selfie_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := strings.TrimSpace(req.Identifier)
	var uid, email, phone, kycStatus string
	var q string
	switch classifyIdentifier(id) {
	case "phone":
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,''), COALESCE(kyc_status,'') FROM users WHERE phone_e164=$1`
	case "email":
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,''), COALESCE(kyc_status,'') FROM users WHERE lower(email)=lower($1)`
	default:
		q = `SELECT id, COALESCE(email::text,''), COALESCE(phone_e164,''), COALESCE(kyc_status,'') FROM users WHERE username=$1`
	}
	if err := a.db.QueryRow(r.Context(), q, id).Scan(&uid, &email, &phone, &kycStatus); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid reset request")
		return
	}
	if email != "" {
		if req.EmailCode == "" || !a.verify2FAResetCode(w, r, uid, "email", req.EmailCode) {
			return
		}
	}
	if phone != "" {
		if req.PhoneCode == "" || !a.verify2FAResetCode(w, r, uid, "phone", req.PhoneCode) {
			return
		}
	}
	if email == "" && phone == "" {
		writeErr(w, http.StatusBadRequest, "no verified contact channel to verify against")
		return
	}
	// §6: KYC face match with the verified document + liveness.
	if kycStatus != "verified" {
		writeErr(w, http.StatusForbidden, "verified KYC is required to reset 2FA")
		return
	}
	if strings.TrimSpace(req.SelfieURL) == "" {
		writeErr(w, http.StatusBadRequest, "fresh selfie_url required for face-match liveness")
		return
	}
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
		INSERT INTO security_attestations (user_id, purpose, score, checks, expires_at)
		VALUES ($1,'2fa_reset',$2,$3, now() + interval '10 minutes')`, uid, score, rawChecks); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store attestation")
		return
	}
	a.logSecurityEvent(r.Context(), "2fa_reset_verified", uid, clientIP(r), r.UserAgent(), "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "verified", "expires_in_seconds": 600, "score": score})
}

func (a *App) handle2FAResetComplete(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "2FA reset failed")
		return
	}
	defer tx.Rollback(r.Context())
	// A fresh face-match/liveness attestation for exactly this purpose is the
	// authorization token; claim it so it cannot be replayed.
	if !claimAttestationPurpose(r.Context(), tx, uid, "2fa_reset") {
		writeErr(w, http.StatusForbidden, "complete the 2FA reset verification first")
		return
	}
	// §6: remove the old 2FA outright.
	if _, err := tx.Exec(r.Context(),
		`UPDATE users SET totp_enabled=false, totp_secret=NULL, updated_at=now() WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to remove old 2FA")
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM recovery_codes WHERE user_id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to revoke recovery codes")
		return
	}
	// Kill every live session and trusted device so the old 2FA protects nothing.
	if _, err := tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	if err := revokeDeviceTrustsTx(r.Context(), tx, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to revoke trusted devices")
		return
	}
	// Mint the new pending secret immediately; the user enables it with a
	// code from the new authenticator (POST /api/auth/2fa/enable).
	secret, err := generateTOTPSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate new 2FA secret")
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE users SET totp_secret=$1, updated_at=now() WHERE id=$2`, secret, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store new 2FA secret")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "2FA reset failed")
		return
	}
	a.logSecurityEvent(r.Context(), "2fa_reset_completed", uid, clientIP(r), r.UserAgent(), "")
	a.notifyKind(uid, "2fa_reset", map[string]string{})
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "old_2fa_removed",
		"secret": secret,
		"note":   "add the new secret to your authenticator app, then confirm with POST /api/auth/2fa/enable",
	})
}

// claimAttestationPurpose claims a fresh security attestation for a purpose,
// generalizing claimSecurityAttestation (credential_change).
func claimAttestationPurpose(ctx context.Context, tx pgx.Tx, uid, purpose string) bool {
	var id string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM security_attestations
		 WHERE user_id=$1 AND purpose=$2 AND expires_at>now()
		 ORDER BY verified_at DESC LIMIT 1 FOR UPDATE`, uid, purpose).Scan(&id); err != nil {
		return false
	}
	_, err := tx.Exec(ctx, `UPDATE security_attestations SET expires_at=now() WHERE id=$1`, id)
	return err == nil
}

// ---- §7.3: random instruction-based liveness for KYC ----

var livenessInstructions = []string{
	"turn your head slowly to the left",
	"turn your head slowly to the right",
	"smile broadly",
	"blink twice",
	"tilt your head to the left",
	"tilt your head to the right",
}

// handleKYCLivenessChallenge mints a random instruction the user must follow
// during the live selfie capture. The challenge is single-use and short-lived.
func (a *App) handleKYCLivenessChallenge(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	instruction := livenessInstructions[cryptoRandIntn(len(livenessInstructions))]
	nonce, err := a.randomNum(24)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "challenge generation failed")
		return
	}
	var challengeID string
	if err := a.db.QueryRow(r.Context(), `
		INSERT INTO kyc_liveness_challenges (user_id, instruction, nonce, expires_at)
		VALUES ($1,$2,$3, now() + interval '5 minutes') RETURNING id`,
		uid, instruction, nonce).Scan(&challengeID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store challenge")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id":       challengeID,
		"instruction":        instruction,
		"expires_in_seconds": 300,
	})
}
