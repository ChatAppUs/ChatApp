package main

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	encoded, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if !verifyPassword("correct horse battery staple", encoded) {
		t.Fatal("valid password rejected")
	}
	if verifyPassword("wrong password", encoded) {
		t.Fatal("invalid password accepted")
	}
}

func TestJWTRoundTrip(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-minimum!!")
	token, err := signJWT(secret, Claims{
		Sub:  "user-123",
		Type: "access",
		Exp:  time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}
	claims, err := parseJWT(secret, token)
	if err != nil {
		t.Fatalf("parseJWT: %v", err)
	}
	if claims.Sub != "user-123" {
		t.Fatalf("sub mismatch: %q", claims.Sub)
	}
	if claims.Type != "access" {
		t.Fatalf("type mismatch: %q", claims.Type)
	}
}

func TestJWTRejectsTampering(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-minimum!!")
	token, err := signJWT(secret, Claims{Sub: "u1", Exp: time.Now().Add(time.Hour).Unix()})
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}
	parts := strings.Split(token, ".")
	// Tamper with the payload.
	parts[1] = b64url([]byte(`{"sub":"attacker","exp":9999999999}`))
	if _, err := parseJWT(secret, strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered token accepted")
	}
	// Wrong secret.
	if _, err := parseJWT([]byte("different-secret-32-bytes-minimum!!"), token); err == nil {
		t.Fatal("token accepted under wrong secret")
	}
}

func TestJWTRejectsExpired(t *testing.T) {
	secret := []byte("test-secret-key-32-bytes-minimum!!")
	token, err := signJWT(secret, Claims{Sub: "u1", Exp: time.Now().Add(-time.Hour).Unix()})
	if err != nil {
		t.Fatalf("signJWT: %v", err)
	}
	if _, err := parseJWT(secret, token); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestTOTPMatchesRFC6238Vector(t *testing.T) {
	// RFC 6238 Appendix B test vector (SHA-1, 8 digits, seed ASCII "12345678901234567890").
	// Our implementation uses base32 secrets and 6 digits; verify against the
	// RFC timestamp 59s by computing HMAC-SHA1 directly through totpCode.
	// Seed "12345678901234567890" base32-encoded:
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := totpCode(secret, 59/30)
	if err != nil {
		t.Fatalf("totpCode: %v", err)
	}
	// RFC vector at T=59 (8 digits): 94287082 → last 6 digits.
	if code != "287082" {
		t.Fatalf("RFC 6238 vector mismatch: got %s want 287082", code)
	}
}

func TestTOTPVerifyWindow(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generateTOTPSecret: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	code, err := totpCode(secret, uint64(now.Unix())/30)
	if err != nil {
		t.Fatalf("totpCode: %v", err)
	}
	if !verifyTOTP(secret, code, now) {
		t.Fatal("current-window code rejected")
	}
	// One step back must also pass (window tolerance).
	prev, err := totpCode(secret, uint64(now.Unix())/30-1)
	if err != nil {
		t.Fatalf("totpCode: %v", err)
	}
	if !verifyTOTP(secret, prev, now) {
		t.Fatal("previous-window code rejected")
	}
	// Ten steps back must fail.
	old, err := totpCode(secret, uint64(now.Unix())/30-10)
	if err != nil {
		t.Fatalf("totpCode: %v", err)
	}
	if verifyTOTP(secret, old, now) {
		t.Fatal("stale code accepted")
	}
}

func TestExtractHashtags(t *testing.T) {
	cases := []struct {
		body string
		want []string
	}{
		{"hello #world", []string{"world"}},
		{"#one #two #one", []string{"one", "two"}},
		{"no tags here", nil},
		{"#UPPER case", []string{"upper"}},
		{"email a#b not a tag", nil},
		{"#multi_word #with123", []string{"multi_word", "with123"}},
	}
	for _, c := range cases {
		got := extractHashtags(c.body)
		if len(got) != len(c.want) {
			t.Fatalf("%q: got %v want %v", c.body, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q: got %v want %v", c.body, got, c.want)
			}
		}
	}
}

func TestRandomTokenUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tok, err := randomToken(32)
		if err != nil {
			t.Fatalf("randomToken: %v", err)
		}
		if len(tok) < 40 {
			t.Fatalf("token too short: %d", len(tok))
		}
		if seen[tok] {
			t.Fatal("duplicate token generated")
		}
		seen[tok] = true
	}
}
