package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- TOTP two-factor authentication (RFC 6238, RFC 4648 base32) ----

func generateTOTPSecret() (string, error) {
	buf := make([]byte, 20) // 160-bit secret
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func totpCode(secret []byte, counter uint64) string {
	mac := hmac.New(sha1.New, secret)
	binary.Write(mac, binary.BigEndian, counter)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := int64(sum[offset]&0x7f)<<24 |
		int64(sum[offset+1])<<16 |
		int64(sum[offset+2])<<8 |
		int64(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000)
}

func (a *App) checkTOTP(secretB32 string, code string) bool {
	if a.authn != nil {
		if ok, done := a.authn.totpVerify(secretB32, code); done {
			return ok
		}
		return false
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secretB32))
	if err != nil || len(secret) == 0 {
		return false
	}
	counter := uint64(time.Now().Unix() / 30)
	for drift := int64(-1); drift <= 1; drift++ {
		c := int64(counter) + drift
		if c < 0 {
			continue
		}
		if totpCode(secret, uint64(c)) == code {
			return true
		}
	}
	return false
}

// ---- 2FA setup, enable, disable ----

func (a *App) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var enabled bool
	var secret *string
	if err := a.db.QueryRow(r.Context(),
		`SELECT totp_enabled, totp_secret FROM users WHERE id=$1`, uid).Scan(&enabled, &secret); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load 2FA state")
		return
	}
	if enabled {
		writeErr(w, http.StatusConflict, "2FA already enabled")
		return
	}
	totpSecret, err := generateTOTPSecret()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate 2FA secret")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET totp_secret=$1, updated_at=now() WHERE id=$2`, totpSecret, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store 2FA secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": totpSecret,
		"note":   "store this secret in your authenticator app before enabling",
	})
}

func (a *App) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) || req.Code == "" {
		writeErr(w, http.StatusBadRequest, "code required")
		return
	}
	uid := userIDFrom(r)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enable 2FA")
		return
	}
	defer tx.Rollback(r.Context())
	var secret *string
	var enabled bool
	if err := tx.QueryRow(r.Context(),
		`SELECT totp_secret, totp_enabled FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&secret, &enabled); err != nil || secret == nil || *secret == "" {
		writeErr(w, http.StatusPreconditionFailed, "run 2FA setup first")
		return
	}
	if enabled {
		writeErr(w, http.StatusConflict, "2FA already enabled")
		return
	}
	if !a.checkTOTP(*secret, req.Code) {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE users SET totp_enabled=true, updated_at=now() WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enable 2FA")
		return
	}
	raw, err := a.mintRecoveryCodesTx(r.Context(), tx, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate recovery codes")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enable 2FA")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "enabled", "recovery_codes": raw,
		"note": "recovery codes are shown once; store them safely"})
}

func (a *App) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to disable 2FA")
		return
	}
	defer tx.Rollback(r.Context())
	var secret string
	var enabled bool
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(totp_secret,''), totp_enabled FROM users WHERE id=$1 FOR UPDATE`, uid).Scan(&secret, &enabled); err != nil {
		writeErr(w, http.StatusBadRequest, "2FA not configured")
		return
	}
	if enabled && !a.checkTOTP(secret, req.Code) {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	if _, err := tx.Exec(r.Context(), `DELETE FROM recovery_codes WHERE user_id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to disable 2FA")
		return
	}
	if _, err := tx.Exec(r.Context(), `
		UPDATE users
		SET totp_secret=NULL,
		    totp_enabled=false,
		    withdrawal_freeze_until = GREATEST(COALESCE(withdrawal_freeze_until, now()), now() + interval '48 hours'),
		    updated_at=now()
		WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to apply security cooldown")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to disable 2FA")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// handleRecoveryReissue mints fresh recovery codes for a user who has
// already enabled 2FA, then shows the codes exactly once. A 30-day cooldown
// prevents abuse; the user must verify their current password + TOTP code
// before new codes are issued.
func (a *App) handleRecoveryReissue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if !decodeJSON(w, r, &req) || req.Password == "" || req.TOTPCode == "" {
		writeErr(w, http.StatusBadRequest, "password and totp_code required")
		return
	}
	uid := userIDFrom(r)

	// Authorisation: verify password and active 2FA.
	var hash, totpSecret string
	var totpEnabled bool
	var reissuedAt *time.Time
	if err := a.db.QueryRow(r.Context(),
		`SELECT password_hash, COALESCE(totp_secret,''), totp_enabled, recovery_reissued_at
		 FROM users WHERE id=$1`, uid).Scan(&hash, &totpSecret, &totpEnabled, &reissuedAt); err != nil {
		writeErr(w, http.StatusUnauthorized, "account not found")
		return
	}
	if !totpEnabled || totpSecret == "" {
		writeErr(w, http.StatusPreconditionFailed, "2FA is not enabled; enable it first to get recovery codes")
		return
	}
	if reissuedAt != nil && reissuedAt.After(time.Now().Add(-30*24*time.Hour)) {
		writeErr(w, http.StatusTooManyRequests, "recovery codes were already reissued within the last 30 days")
		return
	}
	if !a.passwordVerify(req.Password, hash) {
		writeErr(w, http.StatusUnauthorized, "invalid password")
		a.logSecurityEvent(r.Context(), "recovery_reissue_failed", uid, clientIP(r), r.UserAgent(), "bad password")
		return
	}
	if !a.checkTOTP(totpSecret, req.TOTPCode) {
		writeErr(w, http.StatusUnauthorized, "invalid TOTP code")
		a.logSecurityEvent(r.Context(), "recovery_reissue_failed", uid, clientIP(r), r.UserAgent(), "bad totp")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	// Set cooldown so the codes cannot be reissued again for 30 days.
	if _, err := tx.Exec(r.Context(),
		`UPDATE users SET recovery_reissued_at=now(), updated_at=now() WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to set reissue cooldown")
		return
	}
	raw, err := a.mintRecoveryCodesTx(r.Context(), tx, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate recovery codes")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to issue recovery codes")
		return
	}
	a.logSecurityEvent(r.Context(), "2fa_recovery_reissued", uid, clientIP(r), r.UserAgent(), "")
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "reissued",
		"recovery_codes": raw,
		"note":           "recovery codes are shown once; store them safely. You cannot reissue again for 30 days.",
	})
}

// ---- End-to-end encryption: identity key relay ----
// Clients generate an ECDH P-256 keypair locally (WebCrypto / platform
// keystore), publish only the public key here, and derive per-conversation
// AES-GCM keys client-side. The server stores and relays public keys and
// opaque ciphertext only — plaintext never touches the backend.

func (a *App) handleE2EPublishKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IdentityKey string `json:"identity_key"` // base64 SPKI public key
	}
	if !decodeJSON(w, r, &req) || req.IdentityKey == "" {
		writeErr(w, http.StatusBadRequest, "identity_key required")
		return
	}
	if len(req.IdentityKey) > 512 {
		writeErr(w, http.StatusBadRequest, "identity_key too long")
		return
	}
	uid := userIDFrom(r)
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO e2e_identity_keys (user_id, identity_key, updated_at)
		 VALUES ($1,$2,now())
		 ON CONFLICT (user_id) DO UPDATE SET identity_key=$2, updated_at=now()`,
		uid, req.IdentityKey); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to publish identity key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

func (a *App) handleE2EGetKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &req) || len(req.IDs) == 0 || len(req.IDs) > 100 {
		writeErr(w, http.StatusBadRequest, "ids array required (max 100)")
		return
	}
	keys := make(map[string]string)
	for _, id := range req.IDs {
		var key string
		err := a.db.QueryRow(r.Context(),
			`SELECT identity_key FROM e2e_identity_keys WHERE user_id=$1`, id).Scan(&key)
		if err == nil {
			keys[id] = key
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// ---- Recovery codes (one-time backup) ----

func (a *App) scrubRecoveryCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(code)))
}

func b32NoPad(b []byte) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	var out strings.Builder
	acc := uint32(0)
	bits := uint(0)
	for _, v := range b {
		acc = (acc << 8) | uint32(v)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out.WriteByte(alphabet[(acc>>bits)&31])
		}
	}
	if bits > 0 {
		out.WriteByte(alphabet[(acc<<(5-bits))&31])
	}
	return out.String()
}

func (a *App) mintRecoveryCodesTx(ctx context.Context, tx pgx.Tx, uid string) ([]string, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id=$1`, uid); err != nil {
		return nil, err
	}
	raw := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		buf := make([]byte, 10)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		code := strings.ToLower(fmt.Sprintf("%s-%s-%s", b32NoPad(buf[0:4]), b32NoPad(buf[4:8]), b32NoPad(buf[8:10])))
		if _, err := tx.Exec(ctx,
			`INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1,$2)`,
			uid, sha256hex(a.scrubRecoveryCode(code))); err != nil {
			return nil, err
		}
		raw = append(raw, code)
	}
	return raw, nil
}

// verifyRecoveryCode consumes a one-time scratch code: removes the matched hash.
// verifyRecoveryCode atomically consumes a one-time scratch code from the
// recovery_codes table (issued by gap-pack-9 mint route) and reports success.
func (a *App) verifyRecoveryCode(uid string, code string) bool {
	scrubbed := a.scrubRecoveryCode(code)
	if len(scrubbed) == 0 {
		return false
	}
	target := sha256hex(scrubbed)
	res, err := a.db.Exec(context.Background(),
		`UPDATE recovery_codes SET used_at=now() WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL`, uid, target)
	if err != nil {
		return false
	}
	return res.RowsAffected() == 1
}

// ---- Credential change challenge (requires 48h attestation) ----

func (a *App) handleCredentialChangeChallenge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
	}
	if !decodeJSON(w, r, &req) || (req.Kind != "email" && req.Kind != "phone") {
		writeErr(w, http.StatusBadRequest, "kind must be email or phone")
		return
	}
	uid := userIDFrom(r)
	// Check withdrawal freeze
	if a.isWithdrawalFrozen(r.Context(), uid) {
		writeErr(w, http.StatusForbidden, "credential changes are frozen for 48 hours after a security-sensitive change")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE credential_change_challenges SET expires_at=now()
		 WHERE user_id=$1 AND kind=$2 AND verified_at IS NULL`, uid, req.Kind); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to expire previous challenges")
		return
	}
	code, salt, hash, err := a.otpMake()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate challenge")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO credential_change_challenges (user_id, kind, code_hash, salt, expires_at)
		 VALUES ($1,$2,$3,$4,now()+interval '10 minutes')`,
		uid, req.Kind, hash, salt); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store challenge")
		return
	}
	if req.Kind == "email" {
		var email string
		if err := a.db.QueryRow(r.Context(), `SELECT COALESCE(email::text,'') FROM users WHERE id=$1`, uid).Scan(&email); err != nil || email == "" {
			writeErr(w, http.StatusBadRequest, "no email on file")
			return
		}
		if !a.smtp.Configured() {
			if a.cfg.AppEnv == "development" {
				writeJSON(w, http.StatusOK, map[string]string{"status": "challenge_sent", "dev_code": code})
				return
			}
			writeErr(w, http.StatusBadGateway, "email not configured")
			return
		}
		_ = a.smtp.Send(email, "ChatApp credential change verification",
			fmt.Sprintf("Your ChatApp credential change verification code is %s. It expires in 10 minutes.", code))
	}
	if req.Kind == "phone" {
		var phone string
		if err := a.db.QueryRow(r.Context(), `SELECT COALESCE(phone_e164,'') FROM users WHERE id=$1`, uid).Scan(&phone); err != nil || phone == "" {
			writeErr(w, http.StatusBadRequest, "no phone on file")
			return
		}
		if err := a.otp.gateway.Deliver(r.Context(), phone,
			fmt.Sprintf("Your ChatApp credential change verification code is: %s", code)); err != nil {
			writeErr(w, http.StatusBadGateway, "failed to send code")
			return
		}
	}
	out := map[string]string{"status": "challenge_sent"}
	if a.cfg.AppEnv == "development" {
		out["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleCredentialChangeVerify(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind != "email" && kind != "phone" {
		writeErr(w, http.StatusBadRequest, "invalid challenge kind")
		return
	}
	var req struct{ Code string }
	if !decodeJSON(w, r, &req) || req.Code == "" {
		writeErr(w, http.StatusBadRequest, "code required")
		return
	}
	uid := userIDFrom(r)
	var id, wantHash, salt string
	err := a.db.QueryRow(r.Context(),
		`SELECT id, code_hash, salt FROM credential_change_challenges
		 WHERE user_id=$1 AND kind=$2 AND verified_at IS NULL AND expires_at > now() AND attempts < 5
		 ORDER BY created_at DESC LIMIT 1`, uid, kind).Scan(&id, &wantHash, &salt)
	if err != nil || a.otpHashOf(salt, req.Code) != wantHash {
		if id != "" {
			_, _ = a.db.Exec(r.Context(), `UPDATE credential_change_challenges SET attempts=attempts+1 WHERE id=$1`, id)
		}
		writeErr(w, http.StatusUnauthorized, "invalid or expired code")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE credential_change_challenges SET verified_at=now() WHERE id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record verification")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": kind + "_verified"})
}

func (a *App) handleCredentialChangeApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email,omitempty"`
		Phone string `json:"phone,omitempty"`
	}
	if !decodeJSON(w, r, &req) || (req.Email == "" && req.Phone == "") {
		writeErr(w, http.StatusBadRequest, "email or phone required")
		return
	}
	uid := userIDFrom(r)
	if a.isWithdrawalFrozen(r.Context(), uid) {
		writeErr(w, http.StatusForbidden, "credential changes are frozen for 48 hours after a recent security change")
		return
	}
	// Require verified OTP challenges for the fields being changed.
	if req.Email != "" {
		if !a.credentialChangeVerified(r.Context(), uid, "email") {
			writeErr(w, http.StatusForbidden, "email OTP verification required")
			return
		}
	}
	if req.Phone != "" {
		if !a.credentialChangeVerified(r.Context(), uid, "phone") {
			writeErr(w, http.StatusForbidden, "phone OTP verification required")
			return
		}
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to apply credential change")
		return
	}
	defer tx.Rollback(r.Context())
	if req.Email != "" {
		if _, err := tx.Exec(r.Context(),
			`UPDATE users SET email=$1, withdrawal_freeze_until=GREATEST(COALESCE(withdrawal_freeze_until,now()),now()+interval '48 hours'), updated_at=now()
			 WHERE id=$2`, strings.ToLower(strings.TrimSpace(req.Email)), uid); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to update email")
			return
		}
	}
	if req.Phone != "" {
		if _, err := tx.Exec(r.Context(),
			`UPDATE users SET phone_e164=$1, withdrawal_freeze_until=GREATEST(COALESCE(withdrawal_freeze_until,now()),now()+interval '48 hours'), updated_at=now()
			 WHERE id=$2`, strings.TrimSpace(req.Phone), uid); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to update phone")
			return
		}
	}
	// Revoke all sessions to force re-auth with new credentials.
	if _, err := tx.Exec(r.Context(),
		`UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to apply credential change")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "credentials_updated"})
}

// freezeWithdrawals records the security cooldown required after a sensitive
// account change (identity spec §8). The greatest deadline wins, so concurrent
// changes cannot shorten an existing freeze.
func freezeWithdrawalsTx(ctx context.Context, tx pgx.Tx, uid string) error {
	_, err := tx.Exec(ctx, `
		UPDATE users
		SET withdrawal_freeze_until = GREATEST(COALESCE(withdrawal_freeze_until, now()), now() + interval '48 hours'),
		    updated_at = now()
		WHERE id=$1`, uid)
	return err
}

func (a *App) freezeWithdrawals(ctx context.Context, uid string) error {
	_, err := a.db.Exec(ctx, `
		UPDATE users
		SET withdrawal_freeze_until = GREATEST(COALESCE(withdrawal_freeze_until, now()), now() + interval '48 hours'),
		    updated_at = now()
		WHERE id=$1`, uid)
	return err
}

func (a *App) isWithdrawalFrozen(ctx context.Context, uid string) bool {
	var until *time.Time
	_ = a.db.QueryRow(ctx,
		`SELECT withdrawal_freeze_until FROM users WHERE id=$1`, uid).Scan(&until)
	return until != nil && until.After(time.Now())
}

func (a *App) credentialChangeVerified(ctx context.Context, uid, kind string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM credential_change_challenges
		 WHERE user_id=$1 AND kind=$2 AND verified_at > now()-interval '10 minutes')`,
		uid, kind).Scan(&ok)
	return ok
}

func (a *App) handleRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	if _, err := a.db.Exec(r.Context(),
		`UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to revoke sessions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "all_sessions_revoked"})
}

// ---- KYC ID attestation (face-match liveness) ----
