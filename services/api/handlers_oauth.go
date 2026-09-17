package main

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Google sign-in: the client obtains an ID token from Google Identity
// Services; we verify it locally against Google's JWKS (no round-trip per
// login) and then find-or-create the account.

const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

var googleIssuers = map[string]bool{
	"accounts.google.com":         true,
	"https://accounts.google.com": true,
}

type jwksCache struct {
	mu    sync.RWMutex
	keys  map[string]*rsa.PublicKey // kid -> key
	fetch time.Time
}

var googleKeys = &jwksCache{keys: map[string]*rsa.PublicKey{}}

func (c *jwksCache) get(kid string) (*rsa.PublicKey, error) {
	return c.getWithURL(kid, googleJWKSURL)
}

// getWithURL fetches (and caches) the JWKS at the given URL; Google and Apple
// share the cache implementation but pin different key endpoints.
func (c *jwksCache) getWithURL(kid, jwksURL string) (*rsa.PublicKey, error) {
	c.mu.RLock()
	key, ok := c.keys[kid]
	stale := time.Since(c.fetch) > 6*time.Hour
	c.mu.RUnlock()
	if ok && !stale {
		return key, nil
	}
	if err := c.refreshWithURL(jwksURL); err != nil {
		if ok { // serve stale key rather than fail logins during an IdP outage
			return key, nil
		}
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, errors.New("unknown signing key")
}

func (c *jwksCache) refresh() error {
	return c.refreshWithURL(googleJWKSURL)
}

func (c *jwksCache) refreshWithURL(jwksURL string) error {
	req, err := http.NewRequest(http.MethodGet, jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("jwks fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []struct {
			Kid string   `json:"kid"`
			Kty string   `json:"kty"`
			Alg string   `json:"alg"`
			Use string   `json:"use"`
			N   string   `json:"n"`
			E   string   `json:"e"`
			X5c []string `json:"x5c"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("jwks decode: %w", err)
	}
	fresh := map[string]*rsa.PublicKey{}
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		var pub *rsa.PublicKey
		if len(k.X5c) > 0 {
			der, err := base64.StdEncoding.DecodeString(k.X5c[0])
			if err != nil {
				continue
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				continue
			}
			if rsaPub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
				pub = rsaPub
			}
		} else if k.N != "" && k.E != "" {
			nb, err1 := base64.RawURLEncoding.DecodeString(k.N)
			eb, err2 := base64.RawURLEncoding.DecodeString(k.E)
			if err1 != nil || err2 != nil {
				continue
			}
			e := 0
			for _, b := range eb {
				e = e<<8 | int(b)
			}
			pub = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
		}
		if pub != nil {
			fresh[k.Kid] = pub
		}
	}
	if len(fresh) == 0 {
		return errors.New("jwks contained no usable RSA keys")
	}
	c.mu.Lock()
	c.keys = fresh
	c.fetch = time.Now()
	c.mu.Unlock()
	return nil
}

type googleIDClaims struct {
	Iss           string `json:"iss"`
	Aud           string `json:"aud"`
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Exp           int64  `json:"exp"`
	Iat           int64  `json:"iat"`
}

func verifyGoogleIDToken(idToken, clientID string) (*googleIDClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed id_token")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("bad header encoding")
	}
	if err := json.Unmarshal(hb, &header); err != nil {
		return nil, errors.New("bad header")
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unexpected alg %q", header.Alg)
	}
	key, err := googleKeys.get(header.Kid)
	if err != nil {
		return nil, err
	}
	signed := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("bad signature encoding")
	}
	digest := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, errors.New("invalid id_token signature")
	}
	cb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("bad claims encoding")
	}
	var claims googleIDClaims
	if err := json.Unmarshal(cb, &claims); err != nil {
		return nil, errors.New("bad claims")
	}
	if !googleIssuers[claims.Iss] {
		return nil, errors.New("bad issuer")
	}
	if clientID != "" && claims.Aud != clientID {
		return nil, errors.New("token not issued for this app")
	}
	if time.Now().Unix() > claims.Exp {
		return nil, errors.New("id_token expired")
	}
	if claims.Sub == "" {
		return nil, errors.New("missing subject")
	}
	return &claims, nil
}

// ---- Apple sign-in (Identity spec §3.2 / §4.2) ----
// The Apple client (web JS or native ASAuthorization) delivers an RS256
// id_token; we verify it against Apple's JWKS and reuse the shared
// find-or-create + 2FA enforcement path.

const appleJWKSURL = "https://appleid.apple.com/auth/keys"

var appleIssuers = map[string]bool{
	"appleid.apple.com":         true,
	"https://appleid.apple.com": true,
}

var appleKeys = &jwksCache{keys: map[string]*rsa.PublicKey{}}

// verifyAppleIDToken validates an Apple identity token the same way
// verifyGoogleIDToken does, but against Apple's JWKS and issuer set.
func verifyAppleIDToken(idToken, clientID string) (*googleIDClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed id_token")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("bad header encoding")
	}
	if err := json.Unmarshal(hb, &header); err != nil {
		return nil, errors.New("bad header")
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unexpected alg %q", header.Alg)
	}
	key, err := appleKeys.getWithURL(header.Kid, appleJWKSURL)
	if err != nil {
		return nil, err
	}
	signed := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("bad signature encoding")
	}
	digest := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, errors.New("invalid id_token signature")
	}
	cb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("bad claims encoding")
	}
	var claims googleIDClaims
	if err := json.Unmarshal(cb, &claims); err != nil {
		return nil, errors.New("bad claims")
	}
	if !appleIssuers[claims.Iss] {
		return nil, errors.New("bad issuer")
	}
	if clientID != "" && claims.Aud != clientID {
		return nil, errors.New("token not issued for this app")
	}
	if time.Now().Unix() > claims.Exp {
		return nil, errors.New("id_token expired")
	}
	if claims.Sub == "" {
		return nil, errors.New("missing subject")
	}
	return &claims, nil
}

// oauthFindOrCreate is the shared federated-login tail: link by provider_sub,
// then by verified email, else provision a new account with an unguessable
// password. TOTP 2FA is enforced on every path.
func (a *App) oauthFindOrCreate(w http.ResponseWriter, r *http.Request, provider string, claims *googleIDClaims, totpCode string) {
	ctx := r.Context()

	var userID string
	err := a.db.QueryRow(ctx,
		`SELECT user_id FROM oauth_accounts WHERE provider=$1 AND provider_sub=$2`,
		provider, claims.Sub).Scan(&userID)
	if err == nil {
		a.finishOAuthLogin(w, r, userID, totpCode)
		return
	}

	if claims.Email != "" && claims.EmailVerified {
		err = a.db.QueryRow(ctx,
			`SELECT id FROM users WHERE lower(email)=lower($1)`, claims.Email).Scan(&userID)
		if err == nil {
			if _, err := a.db.Exec(ctx,
				`INSERT INTO oauth_accounts (user_id, provider, provider_sub, email)
				 VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
				userID, provider, claims.Sub, claims.Email); err != nil {
				writeErr(w, http.StatusInternalServerError, "failed to link account")
				return
			}
			a.finishOAuthLogin(w, r, userID, totpCode)
			return
		}
	}

	base := strings.ToLower(strings.Split(claims.Email, "@")[0])
	var b strings.Builder
	for _, c := range base {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		}
	}
	base = b.String()
	if len(base) < 3 {
		base = "user" + base
	}
	if len(base) > 24 {
		base = base[:24]
	}
	username := base
	for i := 0; ; i++ {
		var exists bool
		_ = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username=$1)`, username).Scan(&exists)
		if !exists {
			break
		}
		username = fmt.Sprintf("%s%d", base, i+1)
	}
	display := claims.Name
	if display == "" {
		display = username
	}
	// Random, unguessable password — the account is usable via the federated
	// provider/passkey; the user can set a password later in settings.
	randomPW, _ := a.randomNum(24)
	hash, err := a.passwordHash(randomPW)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create account")
		return
	}
	email := claims.Email
	var emailArg *string
	if email != "" {
		emailArg = &email
	}
	err = a.db.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash, display_name)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		username, emailArg, hash, display).Scan(&userID)
	if err != nil {
		writeErr(w, http.StatusConflict, "could not create account (email may already exist unverified)")
		return
	}
	a.bootstrapFirstAdmin(ctx, userID)
	if _, err := a.db.Exec(ctx,
		`INSERT INTO oauth_accounts (user_id, provider, provider_sub, email)
		 VALUES ($1,$2,$3,$4)`, userID, provider, claims.Sub, claims.Email); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to link account")
		return
	}
	if claims.Picture != "" {
		_, _ = a.db.Exec(ctx, `UPDATE users SET avatar_url=$2 WHERE id=$1 AND avatar_url=''`,
			userID, claims.Picture)
	}
	a.finishOAuthLogin(w, r, userID, totpCode)
}

func (a *App) handleAppleAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDToken  string `json:"id_token"`
		TOTPCode string `json:"totp_code"`
	}
	if !decodeJSON(w, r, &req) || req.IDToken == "" {
		writeErr(w, http.StatusBadRequest, "id_token required")
		return
	}
	if a.cfg.AppleClientID == "" {
		writeErr(w, http.StatusServiceUnavailable, "apple sign-in not configured")
		return
	}
	claims, err := verifyAppleIDToken(req.IDToken, a.cfg.AppleClientID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "apple verification failed: "+err.Error())
		return
	}
	a.oauthFindOrCreate(w, r, "apple", claims, req.TOTPCode)
}

func (a *App) handleGoogleAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDToken  string `json:"id_token"`
		TOTPCode string `json:"totp_code"`
	}
	if !decodeJSON(w, r, &req) || req.IDToken == "" {
		writeErr(w, http.StatusBadRequest, "id_token required")
		return
	}
	if a.cfg.GoogleClientID == "" {
		writeErr(w, http.StatusServiceUnavailable, "google sign-in not configured")
		return
	}
	claims, err := verifyGoogleIDToken(req.IDToken, a.cfg.GoogleClientID)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "google verification failed: "+err.Error())
		return
	}
	a.oauthFindOrCreate(w, r, "google", claims, req.TOTPCode)
}

func (a *App) finishOAuthLogin(w http.ResponseWriter, r *http.Request, userID, totpCode string) {
	// Respect TOTP 2FA: linked Google sign-in proves identity but the user
	// opted into a second factor, so require it here too.
	var totpEnabled bool
	var totpSecret *string
	_ = a.db.QueryRow(r.Context(),
		`SELECT totp_enabled, totp_secret FROM users WHERE id=$1`, userID).Scan(&totpEnabled, &totpSecret)
	if totpEnabled {
		if totpCode == "" {
			writeErr(w, http.StatusUnauthorized, "totp_required")
			return
		}
		ok := totpSecret != nil && a.checkTOTP(*totpSecret, totpCode)
		if !ok {
			ok = a.verifyRecoveryCode(userID, totpCode)
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid totp code")
			return
		}
	}
	tokens, err := a.issueTokens(r.Context(), userID, r.UserAgent(), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}
