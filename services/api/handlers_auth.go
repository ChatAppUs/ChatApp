package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,30}$`)
	emailRe    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneRe    = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
)

type registerReq struct {
	Username     string `json:"username"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	PhoneCountry string `json:"phone_country"`
	Password     string `json:"password"`
	DisplayName  string `json:"display_name"`
	Locale       string `json:"locale"`
	ReferralCode string `json:"referral_code"`
}

func validPassword(p string) bool {
	if len(p) < 8 || len(p) > 128 {
		return false
	}
	var hasLetter, hasDigit bool
	for _, c := range p {
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			hasLetter = true
		}
	}
	return hasLetter && hasDigit
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Phone = strings.TrimSpace(req.Phone)
	if !usernameRe.MatchString(req.Username) {
		writeErr(w, http.StatusBadRequest, "username must be 3-30 chars of letters, digits, underscore")
		return
	}
	if req.Email == "" && req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "email or phone is required")
		return
	}
	if req.Email != "" && !emailRe.MatchString(req.Email) {
		writeErr(w, http.StatusBadRequest, "invalid email")
		return
	}
	if req.Phone != "" && !phoneRe.MatchString(req.Phone) {
		writeErr(w, http.StatusBadRequest, "phone must be E.164 format, e.g. +14155552671")
		return
	}
	if !validPassword(req.Password) {
		writeErr(w, http.StatusBadRequest, "password must be 8+ chars with letters and digits")
		return
	}
	// Identity spec: "Email/phone OTP verification is required" before account
	// creation. The phone (or email) must have completed OTP verification first.
	if req.Phone != "" {
		var verified bool
		_ = a.db.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM phone_verifications WHERE phone_e164=$1 AND verified_at IS NOT NULL)`,
			req.Phone).Scan(&verified)
		if !verified {
			writeErr(w, http.StatusForbidden, "phone OTP verification is required before registration")
			return
		}
	} else if req.Email != "" {
		var verified bool
		_ = a.db.QueryRow(r.Context(),
			`SELECT EXISTS(SELECT 1 FROM email_verifications WHERE email=$1 AND verified_at IS NOT NULL)`,
			req.Email).Scan(&verified)
		if !verified {
			writeErr(w, http.StatusForbidden, "email OTP verification is required before registration")
			return
		}
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		req.DisplayName = req.Username
	}
	if req.Locale == "" {
		req.Locale = "en"
	}
	// Identity spec §4.1: optional referral code. Resolve the referrer by their
	// referral code (or username) and attribute the new account to them.
	var referredBy *string
	if rc := strings.TrimSpace(req.ReferralCode); rc != "" {
		var rid string
		if err := a.db.QueryRow(r.Context(),
			`SELECT id FROM users WHERE referral_code=$1 OR username=$1`, rc).Scan(&rid); err == nil {
			referredBy = &rid
		}
	}
	hash, err := a.passwordHash(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hashing failed")
		return
	}
	var id string
	err = a.db.QueryRow(r.Context(),
		`INSERT INTO users (username, email, phone_e164, phone_country, password_hash, display_name, locale, referred_by)
		 VALUES ($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), $5, $6, $7, $8) RETURNING id`,
		req.Username, req.Email, req.Phone, req.PhoneCountry, hash, req.DisplayName, req.Locale, referredBy).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			writeErr(w, http.StatusConflict, "username, email or phone already registered")
			return
		}
		writeErr(w, http.StatusInternalServerError, "registration failed")
		return
	}
	a.bootstrapFirstAdmin(r.Context(), id)
	tokens, err := a.issueTokens(r.Context(), id, r.UserAgent(), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	tokens["user_id"] = id
	writeJSON(w, http.StatusCreated, tokens)
}

type loginReq struct {
	Identifier string `json:"identifier"` // username, email or phone
	Password   string `json:"password"`
	TOTPCode   string `json:"totp_code"` // required when 2FA is enabled
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decodeJSON(w, r, &req) {
		return
	}
	id := strings.TrimSpace(req.Identifier)
	var userID, hash, status string
	var deletionScheduled *time.Time
	var totpSecret *string
	var totpEnabled bool
	var lockedUntil *time.Time
	err := a.db.QueryRow(r.Context(),
			`SELECT id, password_hash, status, deletion_scheduled_at, totp_secret, totp_enabled,
			        failed_login_attempts, locked_until FROM users
			 WHERE username = $1 OR email = lower($1) OR phone_e164 = $1`, id).
			Scan(&userID, &hash, &status, &deletionScheduled, &totpSecret, &totpEnabled,
				new(int), &lockedUntil)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusInternalServerError, "login failed")
		return
	}
	// Identity spec: 5 consecutive failures lock the account for 48 hours.
	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		writeErr(w, http.StatusUnauthorized, "account locked for 48 hours after too many failed attempts")
		return
	}
	if errors.Is(err, pgx.ErrNoRows) || !a.passwordVerify(req.Password, hash) {
		// Record the failed attempt against the account (if it exists) and lock
		// it for 48 hours once 5 consecutive failures accumulate. Keep this
		// update atomic so concurrent attempts cannot overwrite the counter.
		if err == nil {
			_, _ = a.db.Exec(r.Context(),
				`UPDATE users
				 SET failed_login_attempts = failed_login_attempts + 1,
				     locked_until = CASE
				       WHEN failed_login_attempts + 1 >= 5 THEN now() + interval '48 hours'
				       ELSE locked_until
				     END,
				     updated_at = now()
				 WHERE id = $1`, userID)
		}
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	// Successful login resets the failure counter and clears any lockout.
	_, _ = a.db.Exec(r.Context(),
		`UPDATE users SET failed_login_attempts = 0, locked_until = NULL, updated_at = now() WHERE id = $1`,
		userID)
	if status == "suspended" && deletionScheduled != nil && deletionScheduled.After(time.Now()) {
		_, _ = a.db.Exec(r.Context(), `UPDATE users SET status='active', deletion_requested_at=NULL, deletion_scheduled_at=NULL, updated_at=now() WHERE id=$1`, userID)
		status = "active"
	}
	if status != "active" {
		writeErr(w, http.StatusForbidden, "account is "+status)
		return
	}
	if totpEnabled && totpSecret != nil {
		if req.TOTPCode == "" {
			writeErr(w, http.StatusUnauthorized, "totp_required")
			return
		}
		ok := a.checkTOTP(*totpSecret, req.TOTPCode)
		if !ok {
			// One-time recovery/scratch codes unlock the account when the
			// authenticator app is unavailable. Each code hashes once.

			ok = a.verifyRecoveryCode(userID, req.TOTPCode)
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid 2FA code")
			return
		}
	}
	tokens, err := a.issueTokens(r.Context(), userID, r.UserAgent(), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	tokens["user_id"] = userID
	writeJSON(w, http.StatusOK, tokens)
}

func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decodeJSON(w, r, &req) || req.RefreshToken == "" {
		writeErr(w, http.StatusBadRequest, "refresh_token required")
		return
	}
	var sessionID, userID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id, user_id FROM sessions
		 WHERE refresh_hash = $1 AND revoked_at IS NULL AND expires_at > now()`,
		sha256hex(req.RefreshToken)).Scan(&sessionID, &userID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	// rotate refresh token
	_, _ = a.db.Exec(r.Context(), `UPDATE sessions SET revoked_at = now() WHERE id = $1`, sessionID)
	tokens, err := a.issueTokens(r.Context(), userID, r.UserAgent(), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if decodeJSON(w, r, &req) && req.RefreshToken != "" {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE sessions SET revoked_at = now() WHERE refresh_hash = $1`, sha256hex(req.RefreshToken))
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (a *App) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email      string `json:"email"`
		Identifier string `json:"identifier"` // username, email or phone (unified smart input)
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" && req.Identifier != "" {
		// Unified auth input: backend resolves username/email/phone to the
		// account's verified email before sending a reset link. No account
		// existence is revealed in the response..
		id := strings.TrimSpace(req.Identifier)
		var found string
		switch {
		case phoneRe.MatchString(id):
			if err := a.db.QueryRow(r.Context(), `SELECT email FROM users WHERE phone_e164=$1`, id).Scan(&found); err == nil {
				email = found
			}
		case strings.Contains(id, "@"):
			if err := a.db.QueryRow(r.Context(), `SELECT email FROM users WHERE lower(email)=lower($1)`, id).Scan(&found); err == nil {
				email = found
			}
		default:
			if err := a.db.QueryRow(r.Context(), `SELECT email FROM users WHERE username=$1`, id).Scan(&found); err == nil {
				email = found
			}
		}
		if email == "" {
			// Do not reveal account existence..
			writeJSON(w, http.StatusOK, map[string]string{"status": "if the email exists, a reset link was sent"})
			return
		}
	}
	var userID string
	err := a.db.QueryRow(r.Context(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		// Do not reveal account existence.
		writeJSON(w, http.StatusOK, map[string]string{"status": "if the email exists, a reset link was sent"})
		return
	}
	token, err := a.randomNum(32)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	_, err = a.db.Exec(r.Context(),
		`INSERT INTO password_resets (user_id, token_hash, expires_at) VALUES ($1,$2,$3)`,
		userID, sha256hex(token), time.Now().Add(time.Hour))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reset creation failed")
		return
	}
	resp := map[string]string{"status": "if the email exists, a reset link was sent"}
	if a.smtp.Configured() {
		if err := a.smtp.Send(email, "ChatApp password reset",
			"Use this token to reset your password (valid 1 hour): "+token); err != nil {
			writeErr(w, http.StatusBadGateway, "failed to send reset email")
			return
		}
	} else if a.cfg.AppEnv == "development" {
		resp["dev_reset_token"] = token
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !validPassword(req.NewPassword) {
		writeErr(w, http.StatusBadRequest, "password must be 8+ chars with letters and digits")
		return
	}
	var resetID, userID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id, user_id FROM password_resets
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()`,
		sha256hex(req.Token)).Scan(&resetID, &userID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid or expired reset token")
		return
	}
	hash, err := a.passwordHash(req.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "hashing failed")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reset failed")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "reset failed")
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE password_resets SET used_at=now() WHERE id=$1`, resetID); err != nil {
		writeErr(w, http.StatusInternalServerError, "reset failed")
		return
	}
	// revoke all sessions so stolen tokens die with the old password
	if _, err := tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, "reset failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "reset failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "password updated"})
}

// ---- Phone verification ----

func (a *App) handlePhoneSendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if !decodeJSON(w, r, &req) || !phoneRe.MatchString(req.Phone) {
		writeErr(w, http.StatusBadRequest, "valid E.164 phone required")
		return
	}
	devCode, err := a.otp.SendCode(req.Phone)
	if errors.Is(err, errOTPThrottled) {
		writeErr(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, "failed to send verification code")
		return
	}
	resp := map[string]string{"status": "code_sent"}
	if devCode != "" && a.cfg.AppEnv == "development" {
		resp["dev_code"] = devCode
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handlePhoneCheckCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	ok, err := a.otp.CheckCode(req.Phone, req.Code)
	if err != nil || !ok {
		writeErr(w, http.StatusUnauthorized, "invalid verification code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

// ---- Email OTP verification (Identity spec: email OTP required before register) ----

func (a *App) handleEmailSendCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &req) || !emailRe.MatchString(strings.ToLower(strings.TrimSpace(req.Email))) {
		writeErr(w, http.StatusBadRequest, "valid email required")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	// Resend cooldown: at most one code per 60s per email.
	var recent bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM email_verifications WHERE email=$1 AND created_at > now() - interval '60 seconds')`,
		email).Scan(&recent)
	if recent {
		writeErr(w, http.StatusTooManyRequests, "please wait before requesting another code")
		return
	}
	code, salt, hash, err := a.otpMake()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate code")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO email_verifications (email, code_hash, salt, expires_at) VALUES ($1,$2,$3, now() + interval '10 minutes')`,
		email, hash, salt); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store code")
		return
	}
	resp := map[string]string{"status": "code_sent"}
	if a.smtp.Configured() {
		if err := a.smtp.Send(email, "ChatApp email verification",
			"Your ChatApp verification code is: "+code); err != nil {
			writeErr(w, http.StatusBadGateway, "failed to send verification email")
			return
		}
	} else if a.cfg.AppEnv == "development" {
		resp["dev_code"] = code
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleEmailCheckCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	var id, wantHash, salt string
	err := a.db.QueryRow(r.Context(),
		`SELECT id, code_hash, COALESCE(salt,'') FROM email_verifications
		 WHERE email=$1 AND verified_at IS NULL AND expires_at > now() AND attempts < 5
		 ORDER BY created_at DESC LIMIT 1`, email).Scan(&id, &wantHash, &salt)
	if err != nil {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE email_verifications SET attempts = attempts + 1 WHERE email=$1 AND verified_at IS NULL`, email)
		writeErr(w, http.StatusUnauthorized, "invalid verification code")
		return
	}
	if subtle.ConstantTimeCompare([]byte(a.otpHashOf(salt, req.Code)), []byte(wantHash)) != 1 {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE email_verifications SET attempts = attempts + 1 WHERE id=$1`, id)
		writeErr(w, http.StatusUnauthorized, "invalid verification code")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE email_verifications SET verified_at=now() WHERE id=$1`, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	u, err := a.getUser(r.Context(), userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

type publicUser struct {
	ID           string          `json:"id"`
	Username     string          `json:"username"`
	DisplayName  string          `json:"display_name"`
	Bio          string          `json:"bio"`
	BioLinks     json.RawMessage `json:"bio_links"`
	AvatarURL    string          `json:"avatar_url"`
	Locale       string          `json:"locale"`
	IsCreator    bool            `json:"is_creator"`
	IsVerified   bool            `json:"is_verified"`
	IsMerchant   bool            `json:"is_merchant"`
	MerchantTier int             `json:"merchant_tier"`
	IsPremium    bool            `json:"is_premium"`
	PinnedPostID string          `json:"pinned_post_id"`
	KYCStatus    string          `json:"kyc_status,omitempty"`
	CreatedAt    string          `json:"created_at"`
	// Client-facing wellbeing/lock flags (Telegram app lock, screen time).
	AppLock     bool `json:"app_lock_enabled"`
	ScreenLimit int  `json:"screen_time_limit_minutes"`
}

func (a *App) getUser(ctx context.Context, id string) (*publicUser, error) {
	var u publicUser
	var createdAt time.Time
	var pinned *string
	err := a.db.QueryRow(ctx,
		`SELECT id, username, display_name, bio, avatar_url, locale, is_creator, is_verified, kyc_status, created_at,
		        pinned_post_id, COALESCE(is_premium, false), COALESCE(bio_links::text,'[]'),
		        COALESCE(app_lock_enabled, false), COALESCE(screen_time_limit_minutes, 0)
		 FROM users WHERE id = $1 AND status = 'active'`, id).
		Scan(&u.ID, &u.Username, &u.DisplayName, &u.Bio, &u.AvatarURL, &u.Locale,
			&u.IsCreator, &u.IsVerified, &u.KYCStatus, &createdAt, &pinned, &u.IsPremium, &u.BioLinks,
			&u.AppLock, &u.ScreenLimit)
	if err != nil {
		return nil, err
	}
	if pinned != nil {
		u.PinnedPostID = *pinned
	}
	_ = a.db.QueryRow(ctx,
		`SELECT status='verified', COALESCE(tier,0) FROM p2p_merchants WHERE user_id=$1`, id).
		Scan(&u.IsMerchant, &u.MerchantTier)
	u.CreatedAt = createdAt.Format(time.RFC3339)
	return &u, nil
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}
