package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ---- TOTP two-factor authentication (RFC 6238, RFC 4648 base32) ----

func generateTOTPSecret() (string, error) {
	buf := make([]byte, 20) // 160-bit secret
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// generateTOTP prefers the delegated Rust implementation so the
// secret-generation RNG sits inside the authn boundary when available.
func (a *App) generateTOTP() (string, error) {
	if a.authn != nil {
		if s, ok := a.authn.totpGenerate(); ok {
			return s, nil
		}
	}
	return generateTOTPSecret()
}

func totpCode(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1000000), nil
}

func verifyTOTP(secret, code string, at time.Time) bool {
	if len(code) != 6 {
		return false
	}
	counter := uint64(at.Unix()) / 30
	for _, drift := range []int64{0, -1, 1} { // tolerate one step of clock drift
		expected, err := totpCode(secret, uint64(int64(counter)+drift))
		if err != nil {
			return false
		}
		if hmac.Equal([]byte(expected), []byte(code)) {
			return true
		}
	}
	return false
}

// checkTOTP verifies a TOTP code. When the Rust authn service is configured
// it owns this crypto (P0 delegation per RUST_CONVERSION_PLAN), followed by
// the older security-service delegation, then the local RFC 6238
// implementation — the login plane stays up in every chain of failures.
func (a *App) checkTOTP(secret, code string) bool {
	if len(code) != 6 {
		return false
	}
	if a.authn != nil {
		if remote, ok := a.authn.totpVerify(secret, code); ok {
			return remote
		}
	}
	if a.cfg.SecuritySvcURL != "" {
		if remote, ok := a.totpRemote(secret, code); ok {
			return remote
		}
	}
	return verifyTOTP(secret, code, time.Now())
}

func (a *App) totpRemote(secret, code string) (bool, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"secret": secret, "code": code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.cfg.SecuritySvcURL+"/totp/verify", bytes.NewReader(body))
	if err != nil {
		return false, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var out struct {
		Valid bool `json:"valid"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<10)).Decode(&out); err != nil {
		return false, false
	}
	return out.Valid, true
}

func (a *App) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	secret, err := a.generateTOTP()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	var username string
	_ = a.db.QueryRow(r.Context(), `SELECT username FROM users WHERE id=$1`, uid).Scan(&username)
	// Store as pending secret; enabled only after code verification.
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET totp_secret=$1, totp_enabled=false, updated_at=now() WHERE id=$2`, secret, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store secret")
		return
	}
	uri := fmt.Sprintf("otpauth://totp/ChatApp:%s?secret=%s&issuer=ChatApp&digits=6&period=30", username, secret)
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "otpauth_url": uri})
}

func (a *App) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	var secret string
	if err := a.db.QueryRow(r.Context(),
		`SELECT COALESCE(totp_secret,'') FROM users WHERE id=$1`, uid).Scan(&secret); err != nil || secret == "" {
		writeErr(w, http.StatusBadRequest, "run 2FA setup first")
		return
	}
	if !a.checkTOTP(secret, req.Code) {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET totp_enabled=true, updated_at=now() WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enable 2FA")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (a *App) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	var secret string
	var enabled bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT COALESCE(totp_secret,''), totp_enabled FROM users WHERE id=$1`, uid).Scan(&secret, &enabled); err != nil {
		writeErr(w, http.StatusBadRequest, "2FA not configured")
		return
	}
	if enabled && !a.checkTOTP(secret, req.Code) {
		writeErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE users SET totp_secret=NULL, totp_enabled=false, updated_at=now() WHERE id=$1`, uid)
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
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
		writeErr(w, http.StatusBadRequest, "identity_key too large")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET e2e_identity_key=$1, e2e_key_updated_at=now() WHERE id=$2`,
		req.IdentityKey, userIDFrom(r)); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}

func (a *App) handleE2EGetKeys(w http.ResponseWriter, r *http.Request) {
	ids := strings.Split(r.URL.Query().Get("user_ids"), ",")
	if len(ids) == 0 || ids[0] == "" || len(ids) > 100 {
		writeErr(w, http.StatusBadRequest, "user_ids required (max 100)")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT id, e2e_identity_key FROM users WHERE id = ANY($1) AND e2e_identity_key IS NOT NULL`, ids)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load keys")
		return
	}
	defer rows.Close()
	keys := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err == nil {
			keys[id] = key
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}
