package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// ---- Password hashing (argon2id, PHC string format) ----

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---- JWT (HS256) ----

type Claims struct {
	Sub   string `json:"sub"`
	Type  string `json:"typ"`             // "access" | "refresh"
	Scope string `json:"scope,omitempty"` // "user" (default) | "admin"
	Exp   int64  `json:"exp"`
	Iat   int64  `json:"iat"`
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func signJWT(secret []byte, claims Claims) (string, error) {
	header := b64url([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	body := header + "." + b64url(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(body))
	return body + "." + b64url(mac.Sum(nil)), nil
}

func parseJWT(secret []byte, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}
	body := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(body))
	expected := mac.Sum(nil)
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, expected) {
		return nil, errors.New("bad signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, err
	}
	if time.Now().Unix() >= c.Exp {
		return nil, errors.New("token expired")
	}
	return &c, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// ---- authn delegation layer (Rust P0 surface) ----
//
// When AUTHN_SERVICE_URL is configured the Rust authn service owns the
// trust-critical crypto (RUST_CONVERSION_PLAN P0 surface). Local
// implementations stay available as the fail-open fallback so the login
// plane survives an unreachable service, exactly like checkTOTP's
// delegation.
func (a *App) passwordHash(pw string) (string, error) {
	if a.authn != nil {
		if h, ok := a.authn.passwordHash(pw); ok {
			return h, nil
		}
	}
	return hashPassword(pw)
}

func (a *App) passwordVerify(pw, hash string) bool {
	if a.authn != nil {
		if ok, done := a.authn.passwordVerify(pw, hash); done {
			return ok
		}
	}
	return verifyPassword(pw, hash)
}

func (a *App) mintClaims(claims Claims) (string, error) {
	if a.authn != nil {
		if tok, ok := a.authn.jwtMint(claims); ok {
			return tok, nil
		}
	}
	return signJWT(a.cfg.JWTSecret, claims)
}

func (a *App) parseClaims(token string) (*Claims, error) {
	if a.authn != nil {
		if cl, ok := a.authn.jwtVerify(token); ok {
			if cl == nil {
				return nil, errors.New("bad token")
			}
			return cl, nil
		}
	}
	return parseJWT(a.cfg.JWTSecret, token)
}

func (a *App) randomNum(n int) (string, error) {
	if a.authn != nil {
		if tok, ok := a.authn.randomToken(n); ok {
			return tok, nil
		}
	}
	return randomToken(n)
}

// ---- Auth middleware ----

type ctxKey string

const ctxUserID ctxKey = "userID"

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := a.parseClaims(strings.TrimPrefix(h, "Bearer "))
		if err != nil || claims.Type != "access" || claims.Scope == "admin" {
			// Admin-scoped tokens are never accepted on user routes; the
			// admin system is a fully separate plane.
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, claims.Sub)
		next(w, r.WithContext(ctx))
	}
}

func userIDFrom(r *http.Request) string {
	if v, ok := r.Context().Value(ctxUserID).(string); ok {
		return v
	}
	return ""
}

// bootstrapFirstAdmin grants superadmin to the very first account on a fresh
// deployment (standard bootstrap: Mastodon, Discourse). After that, roles are
// granted only by existing superadmins.
func (a *App) bootstrapFirstAdmin(ctx context.Context, userID string) {
	var anyAdmin bool
	if err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_roles)`).Scan(&anyAdmin); err != nil || anyAdmin {
		return
	}
	_, _ = a.db.Exec(ctx,
		`INSERT INTO admin_roles (user_id, role, granted_by) VALUES ($1,'superadmin',$1) ON CONFLICT DO NOTHING`,
		userID)
}

func (a *App) issueTokens(ctx context.Context, userID, userAgent, ip string) (map[string]any, error) {
	now := time.Now()
	access, err := a.mintClaims(Claims{
		Sub: userID, Type: "access",
		Iat: now.Unix(), Exp: now.Add(a.cfg.AccessTokenTTL).Unix(),
	})
	if err != nil {
		return nil, err
	}
	refresh, err := a.randomNum(32)
	if err != nil {
		return nil, err
	}
	_, err = a.db.Exec(ctx,
		`INSERT INTO sessions (user_id, refresh_hash, user_agent, ip, expires_at)
		 VALUES ($1,$2,$3,NULLIF($4,'')::inet,$5)`,
		userID, sha256hex(refresh), userAgent, ip, now.Add(a.cfg.RefreshTokenTTL))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"expires_in":    int(a.cfg.AccessTokenTTL.Seconds()),
	}, nil
}
