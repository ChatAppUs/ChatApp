package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// WebAuthn/passkey ceremonies. Passkeys created with platform authenticators
// are the fingerprint / face / device-passcode unlock — the browser or OS
// gates key use behind biometrics, so no biometric data ever reaches us.

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

func (a *App) webauthnAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	for _, o := range strings.Split(a.cfg.WebAuthnOrigins, ",") {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}

func (a *App) newChallenge(r *http.Request, userID *string, kind string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	// base64url: this exact string round-trips through browser authenticators
	// (client decodes to bytes; authenticator returns base64url of the same).
	chal := base64.RawURLEncoding.EncodeToString(raw)
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO webauthn_challenges (challenge, user_id, kind, expires_at)
		 VALUES ($1,$2,$3, now() + interval '5 minutes')`, chal, userID, kind)
	return chal, err
}

func (a *App) popChallenge(r *http.Request, chal, kind string) (*string, error) {
	var userID *string
	err := a.db.QueryRow(r.Context(),
		`DELETE FROM webauthn_challenges
		 WHERE challenge=$1 AND kind=$2 AND expires_at > now()
		 RETURNING user_id`, chal, kind).Scan(&userID)
	if err != nil {
		return nil, errors.New("challenge expired or unknown")
	}
	return userID, nil
}

// ---------- registration (requires an authenticated session) ----------

func (a *App) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var username, display string
	if err := a.db.QueryRow(r.Context(),
		`SELECT username, display_name FROM users WHERE id=$1`, uid).Scan(&username, &display); err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	chal, err := a.newChallenge(r, &uid, "register")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create challenge")
		return
	}
	// Exclude already-registered credentials so the same key can't be added twice.
	rows, err := a.db.Query(r.Context(),
		`SELECT credential_id, transports FROM webauthn_credentials WHERE user_id=$1`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load credentials")
		return
	}
	defer rows.Close()
	exclude := []map[string]any{}
	for rows.Next() {
		var credID []byte
		var transports []string
		if err := rows.Scan(&credID, &transports); err == nil {
			exclude = append(exclude, map[string]any{
				"type":       "public-key",
				"id":         base64.RawURLEncoding.EncodeToString(credID),
				"transports": transports,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge": chal,
		"rp": map[string]any{
			"id":   a.cfg.WebAuthnRPID,
			"name": a.cfg.WebAuthnRPName,
		},
		"user": map[string]any{
			"id":          base64.RawURLEncoding.EncodeToString([]byte(uid)),
			"name":        username,
			"displayName": display,
		},
		"pubKeyCredParams": []map[string]any{
			{"type": "public-key", "alg": -7},   // ES256
			{"type": "public-key", "alg": -8},   // EdDSA
			{"type": "public-key", "alg": -257}, // RS256
		},
		"timeout":            300000,
		"attestation":        "none",
		"excludeCredentials": exclude,
		"authenticatorSelection": map[string]any{
			"residentKey":      "preferred",
			"userVerification": "preferred",
		},
	})
}

func (a *App) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Name     string `json:"name"`
		Response struct {
			ClientDataJSON    string   `json:"clientDataJSON"`
			AttestationObject string   `json:"attestationObject"`
			Transports        []string `json:"transports"`
		} `json:"response"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	clientData, err := b64urlDecode(req.Response.ClientDataJSON)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad clientDataJSON")
		return
	}
	var cd struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}
	if err := json.Unmarshal(clientData, &cd); err != nil {
		writeErr(w, http.StatusBadRequest, "bad clientDataJSON")
		return
	}
	if cd.Type != "webauthn.create" {
		writeErr(w, http.StatusBadRequest, "wrong ceremony type")
		return
	}
	if !a.webauthnAllowedOrigin(cd.Origin) {
		writeErr(w, http.StatusForbidden, "origin not allowed")
		return
	}
	if _, err := a.popChallenge(r, cd.Challenge, "register"); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	attObj, err := b64urlDecode(req.Response.AttestationObject)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad attestationObject")
		return
	}
	v, err := parseCBOR(attObj)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad attestationObject cbor")
		return
	}
	m, ok := v.(map[any]any)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad attestationObject")
		return
	}
	if fmt, _ := m["fmt"].(string); fmt != "none" {
		writeErr(w, http.StatusBadRequest, "only attestation 'none' accepted")
		return
	}
	authDataRaw, ok := m["authData"].([]byte)
	if !ok {
		writeErr(w, http.StatusBadRequest, "missing authData")
		return
	}
	ad, err := parseAuthData(authDataRaw, true)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "authData: "+err.Error())
		return
	}
	if err := ad.verifyRP(a.cfg.WebAuthnRPID); err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > 60 {
		name = name[:60]
	}
	transports := req.Response.Transports
	if transports == nil {
		transports = []string{}
	}
	_, err = a.db.Exec(r.Context(),
		`INSERT INTO webauthn_credentials
		   (user_id, credential_id, public_key, sign_count, transports, name, aaguid)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (credential_id) DO NOTHING`,
		uid, ad.CredentialID, ad.CredentialPublicKey, ad.SignCount, transports, name, ad.AAGUID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to store credential")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "name": name})
}

// ---------- authentication (username-less passkey login) ----------

func (a *App) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // optional body
	var uid *string
	allow := []map[string]any{}
	if req.Username != "" {
		var id string
		err := a.db.QueryRow(r.Context(),
			`SELECT id FROM users WHERE username=$1 OR lower(email)=lower($1) OR phone_e164=$1`,
			strings.TrimSpace(req.Username)).Scan(&id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "no such user")
			return
		}
		uid = &id
		rows, err := a.db.Query(r.Context(),
			`SELECT credential_id, transports FROM webauthn_credentials WHERE user_id=$1`, id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to load credentials")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var credID []byte
			var transports []string
			if err := rows.Scan(&credID, &transports); err == nil {
				allow = append(allow, map[string]any{
					"type":       "public-key",
					"id":         base64.RawURLEncoding.EncodeToString(credID),
					"transports": transports,
				})
			}
		}
		if len(allow) == 0 {
			writeErr(w, http.StatusNotFound, "no passkeys registered for this user")
			return
		}
	}
	chal, err := a.newChallenge(r, uid, "login")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create challenge")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge":        chal,
		"rpId":             a.cfg.WebAuthnRPID,
		"timeout":          300000,
		"userVerification": "preferred",
		"allowCredentials": allow,
	})
}

func (a *App) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		TOTPCode string `json:"totp_code"`
		Response struct {
			ClientDataJSON    string `json:"clientDataJSON"`
			AuthenticatorData string `json:"authenticatorData"`
			Signature         string `json:"signature"`
			UserHandle        string `json:"userHandle"`
		} `json:"response"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	credID, err := b64urlDecode(req.ID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad credential id")
		return
	}
	var userID string
	var coseKey []byte
	var storedCount int64
	err = a.db.QueryRow(r.Context(),
		`SELECT user_id, public_key, sign_count FROM webauthn_credentials WHERE credential_id=$1`,
		credID).Scan(&userID, &coseKey, &storedCount)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unknown credential")
		return
	}
	clientData, err := b64urlDecode(req.Response.ClientDataJSON)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad clientDataJSON")
		return
	}
	var cd struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}
	if err := json.Unmarshal(clientData, &cd); err != nil {
		writeErr(w, http.StatusBadRequest, "bad clientDataJSON")
		return
	}
	if cd.Type != "webauthn.get" {
		writeErr(w, http.StatusBadRequest, "wrong ceremony type")
		return
	}
	if !a.webauthnAllowedOrigin(cd.Origin) {
		writeErr(w, http.StatusForbidden, "origin not allowed")
		return
	}
	if _, err := a.popChallenge(r, cd.Challenge, "login"); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	authDataRaw, err := b64urlDecode(req.Response.AuthenticatorData)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad authenticatorData")
		return
	}
	ad, err := parseAuthData(authDataRaw, false)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "authData: "+err.Error())
		return
	}
	if err := ad.verifyRP(a.cfg.WebAuthnRPID); err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	sig, err := b64urlDecode(req.Response.Signature)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad signature")
		return
	}
	clientHash := sha256.Sum256(clientData)
	signed := append(append([]byte{}, authDataRaw...), clientHash[:]...)
	if err := verifyCOSESignature(coseKey, signed, sig); err != nil {
		writeErr(w, http.StatusUnauthorized, "signature verification failed")
		return
	}
	// Clone detection: a strictly-greater counter is required when either side
	// reports a non-zero value (many authenticators always return 0).
	if ad.SignCount != 0 || storedCount != 0 {
		if int64(ad.SignCount) <= storedCount {
			writeErr(w, http.StatusUnauthorized, "credential may be cloned (sign counter)")
			return
		}
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE webauthn_credentials SET sign_count=$2, last_used_at=now() WHERE credential_id=$1`,
		credID, ad.SignCount)

	// Respect TOTP as a second factor when enabled: passkey replaces the
	// password factor, not the TOTP factor.
	var totpEnabled bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT totp_enabled FROM users WHERE id=$1`, userID).Scan(&totpEnabled)
	if totpEnabled {
		// totp_code is accepted alongside the assertion for one-round login.
		if req.TOTPCode == "" {
			writeErr(w, http.StatusUnauthorized, "totp_required")
			return
		}
		var secret *string
		_ = a.db.QueryRow(r.Context(),
			`SELECT totp_secret FROM users WHERE id=$1`, userID).Scan(&secret)
		if secret == nil || !verifyTOTP(*secret, req.TOTPCode, time.Now()) {
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

// ---------- management ----------

func (a *App) handlePasskeyList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, name, transports, created_at, last_used_at
		 FROM webauthn_credentials WHERE user_id=$1 ORDER BY created_at`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to list passkeys")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var transports []string
		var created time.Time
		var lastUsed *time.Time
		if err := rows.Scan(&id, &name, &transports, &created, &lastUsed); err == nil {
			out = append(out, map[string]any{
				"id": id, "name": name, "transports": transports,
				"created_at": created, "last_used_at": lastUsed,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"passkeys": out})
}

func (a *App) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM webauthn_credentials WHERE id=$1 AND user_id=$2`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "passkey not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
