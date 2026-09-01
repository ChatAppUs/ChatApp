package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// authnClient delegates the trust-critical crypto surface to the Rust authn
// service when AUTHN_SERVICE_URL is configured, mirroring the delegation
// pattern of the security service (a fail-open local fallback keeps the
// login plane up; every verify op runs constant-time in BOTH layers, and
// production deployments must set a stable AUTHN_SECRET).
type authnClient struct {
	url    string
	secret string
	client *http.Client

	mu    sync.Mutex
	cache map[string]*Claims // token -> verified claims (bounded)
}

type authnClaimsOut struct {
	Claims *Claims `json:"claims"`
}

func newAuthnClient(url, secret string) *authnClient {
	if url == "" || secret == "" {
		return nil
	}
	return &authnClient{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: 3 * time.Second},
		cache:  make(map[string]*Claims),
	}
}

func (c *authnClient) do(in any, path string, out any) bool {
	b, _ := json.Marshal(in)
	req, err := http.NewRequest(http.MethodPost, c.url+path, bytes.NewReader(b))
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	return json.NewDecoder(resp.Body).Decode(out) == nil
}

// passwordHash returns the delegated argon2id PHC hash, or "" with ok=false
// on failure (the caller then falls back to hashPassword).
func (c *authnClient) passwordHash(pw string) (string, bool) {
	var out struct {
		Hash string `json:"hash"`
	}
	if !c.do(map[string]string{"password": pw}, "/password/hash", &out) || out.Hash == "" {
		return "", false
	}
	return out.Hash, true
}

// passwordVerify delegates PHC verification; ok=false on transport failure.
func (c *authnClient) passwordVerify(pw, hash string) (bool, bool) {
	var out struct {
		Ok bool `json:"ok"`
	}
	if !c.do(map[string]string{"password": pw, "hash": hash}, "/password/verify", &out) {
		return false, false
	}
	return out.Ok, true
}

func (c *authnClient) jwtMint(claims Claims) (string, bool) {
	var out struct {
		Token string `json:"token"`
	}
	in := map[string]any{
		"sub": claims.Sub, "typ": claims.Type,
		"exp": claims.Exp, "iat": claims.Iat,
	}
	if claims.Scope != "" {
		in["scope"] = claims.Scope
	}
	if !c.do(in, "/jwt/mint", &out) || out.Token == "" {
		return "", false
	}
	return out.Token, true
}

func (c *authnClient) jwtVerify(token string) (*Claims, bool) {
	c.mu.Lock()
	if cl, hit := c.cache[token]; hit {
		c.mu.Unlock()
		return cl, true
	}
	if len(c.cache) >= 4096 {
		// bounded: drop an arbitrary entry before the map grows
		for k := range c.cache {
			delete(c.cache, k)
			break
		}
	}
	c.mu.Unlock()
	var out authnClaimsOut
	if !c.do(map[string]string{"token": token}, "/jwt/verify", &out) {
		return nil, false
	}
	if out.Claims == nil {
		return nil, true
	}
	c.mu.Lock()
	c.cache[token] = out.Claims
	c.mu.Unlock()
	return out.Claims, true
}

func (c *authnClient) totpGenerate() (string, bool) {
	var out struct {
		Secret string `json:"secret"`
	}
	if !c.do(map[string]any{}, "/totp/generate", &out) || out.Secret == "" {
		return "", false
	}
	return out.Secret, true
}

func (c *authnClient) totpVerify(secret, code string) (bool, bool) {
	var out struct {
		Ok bool `json:"ok"`
	}
	if !c.do(map[string]string{"secret": secret, "code": code}, "/totp/verify", &out) {
		return false, false
	}
	return out.Ok, true
}

// otpGenerate returns code, salt and hash through the Rust engine so the
// ownership boundary of the code-generation RNG sits in one hardened place.
func (c *authnClient) otpGenerate() (code, salt, hash string, ok bool) {
	var out struct {
		Code string `json:"code"`
		Salt string `json:"salt"`
		Hash string `json:"hash"`
	}
	if !c.do(map[string]any{}, "/otp/generate", &out) || out.Code == "" {
		return "", "", "", false
	}
	return out.Code, out.Salt, out.Hash, true
}

func (c *authnClient) otpHash(salt, code string) (string, bool) {
	var out struct {
		Hash string `json:"hash"`
	}
	if !c.do(map[string]string{"salt": salt, "code": code}, "/otp/hash", &out) || out.Hash == "" {
		return "", false
	}
	return out.Hash, true
}

// randomToken delegates the CSPRNG token minting (QR login, reset tokens,
// bot tokens/gen) — verification of bounded length is left to the caller.
func (c *authnClient) randomToken(n int) (string, bool) {
	if n <= 0 || n > 4096 {
		return "", false
	}
	var out struct {
		Token string `json:"token"`
	}
	if !c.do(map[string]int{"bytes": n}, "/random", &out) || out.Token == "" {
		return "", false
	}
	return out.Token, true
}
