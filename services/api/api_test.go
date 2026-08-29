package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
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

func TestCBORRoundTrip(t *testing.T) {
	// {1: 2, "k": [1, "x"], -1: h'0102'} hand-encoded CBOR
	in := []byte{0xa3, 0x01, 0x02, 0x61, 0x6b, 0x82, 0x01, 0x61, 0x78, 0x20, 0x42, 0x01, 0x02}
	v, err := parseCBOR(in)
	if err != nil {
		t.Fatalf("parseCBOR: %v", err)
	}
	m, ok := v.(map[any]any)
	if !ok {
		t.Fatal("expected map")
	}
	if n, _ := m[uint64(1)].(uint64); n != 2 {
		t.Fatalf("m[1] = %v", m[uint64(1)])
	}
	arr, ok := m["k"].([]any)
	if !ok || len(arr) != 2 || arr[1] != "x" {
		t.Fatalf("m[k] = %v", m["k"])
	}
	if b, _ := m[int64(-1)].([]byte); len(b) != 2 || b[0] != 1 {
		t.Fatalf("m[-1] = %v", m[int64(-1)])
	}
}

func TestCBORRejectsTrailing(t *testing.T) {
	if _, err := parseCBOR([]byte{0x01, 0x02}); err == nil {
		t.Fatal("expected trailing-bytes error")
	}
}

// COSE-encode a P-256 public key: {1:2, 3:-7, -1:1, -2:x, -3:y}
func coseEncodeP256(x, y []byte) []byte {
	out := []byte{0xa5}
	out = append(out, 0x01, 0x02)       // 1: 2 (kty EC2)
	out = append(out, 0x03, 0x26)       // 3: -7 (ES256)
	out = append(out, 0x20, 0x01)       // -1: 1 (P-256)
	out = append(out, 0x21, 0x58, 0x20) // -2: bytes(32)
	out = append(out, x...)
	out = append(out, 0x22, 0x58, 0x20) // -3: bytes(32)
	out = append(out, y...)
	return out
}

func TestCOSEES256Verify(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	x := priv.PublicKey.X.Bytes()
	y := priv.PublicKey.Y.Bytes()
	x = append(make([]byte, 32-len(x)), x...)
	y = append(make([]byte, 32-len(y)), y...)
	cose := coseEncodeP256(x, y)

	data := []byte("authenticatorData||clientDataHash")
	digest := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := verifyCOSESignature(cose, data, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := verifyCOSESignature(cose, []byte("tampered"), sig); err == nil {
		t.Fatal("tampered data accepted")
	}
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ox := other.PublicKey.X.Bytes()
	oy := other.PublicKey.Y.Bytes()
	ox = append(make([]byte, 32-len(ox)), ox...)
	oy = append(make([]byte, 32-len(oy)), oy...)
	if err := verifyCOSESignature(coseEncodeP256(ox, oy), data, sig); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestRendezvousPick(t *testing.T) {
	nodes := []ClusterNode{
		{NodeID: "a", Region: "us", Weight: 100, Capacity: 100, Load: 10},
		{NodeID: "b", Region: "eu", Weight: 100, Capacity: 100, Load: 10},
		{NodeID: "c", Region: "ap", Weight: 100, Capacity: 100, Load: 10},
	}
	// determinism: same key always maps to same node
	first := rendezvousPick(nodes, "conv-123")
	for i := 0; i < 50; i++ {
		if rendezvousPick(nodes, "conv-123") != first {
			t.Fatal("non-deterministic pick")
		}
	}
	// distribution: keys spread across all nodes
	seen := map[string]int{}
	for i := 0; i < 300; i++ {
		n := rendezvousPick(nodes, string(rune(i))+":"+strings.Repeat("k", i%7))
		if n == nil {
			t.Fatal("nil pick")
		}
		seen[n.NodeID]++
	}
	if len(seen) < 2 {
		t.Fatalf("poor distribution: %v", seen)
	}
	// minimal disruption: removing one node remaps roughly 1/3 of keys
	remapped := 0
	for i := 0; i < 300; i++ {
		key := string(rune(i)) + ":" + strings.Repeat("k", i%7)
		before := rendezvousPick(nodes, key)
		after := rendezvousPick(nodes[:2], key)
		if before != nil && after != nil && before.NodeID != after.NodeID {
			remapped++
		}
	}
	if remapped > 200 {
		t.Fatalf("excessive remapping: %d/300", remapped)
	}
	// zero spare capacity is skipped
	full := []ClusterNode{{NodeID: "x", Weight: 100, Capacity: 10, Load: 10}}
	if rendezvousPick(full, "k") != nil {
		t.Fatal("full node picked")
	}
}

func TestPickRegionNode(t *testing.T) {
	nodes := []ClusterNode{
		{NodeID: "us-1", Region: "us", Capacity: 100, Load: 90},
		{NodeID: "us-2", Region: "us", Capacity: 100, Load: 5},
		{NodeID: "eu-1", Region: "eu", Capacity: 100, Load: 1},
	}
	if n := pickRegionNode(nodes, "us"); n == nil || n.NodeID != "us-2" {
		t.Fatalf("expected least-loaded us node, got %+v", n)
	}
	// unknown region falls back to global least-loaded
	if n := pickRegionNode(nodes, "zz"); n == nil || n.NodeID != "eu-1" {
		t.Fatalf("expected global fallback, got %+v", n)
	}
	if pickRegionNode(nil, "us") != nil {
		t.Fatal("empty pool should be nil")
	}
}

func TestParseAuthData(t *testing.T) {
	rpHash := sha256.Sum256([]byte("localhost"))
	buf := append([]byte{}, rpHash[:]...)
	buf = append(buf, 0x01|0x40)     // UP + AT flags
	buf = append(buf, 0, 0, 0, 0x2a) // signCount = 42
	buf = append(buf, make([]byte, 16)...)
	credID := []byte{0xde, 0xad, 0xbe, 0xef}
	buf = append(buf, 0x00, byte(len(credID)))
	buf = append(buf, credID...)
	cose := coseEncodeP256(make([]byte, 32), make([]byte, 32))
	buf = append(buf, cose...)

	ad, err := parseAuthData(buf, true)
	if err != nil {
		t.Fatalf("parseAuthData: %v", err)
	}
	if ad.SignCount != 42 || !ad.UserPresent {
		t.Fatalf("bad parse: %+v", ad)
	}
	if string(ad.CredentialID) != string(credID) {
		t.Fatal("credential id mismatch")
	}
	if string(ad.CredentialPublicKey) != string(cose) {
		t.Fatal("cose key mismatch")
	}
	if err := ad.verifyRP("localhost"); err != nil {
		t.Fatalf("verifyRP: %v", err)
	}
	if err := ad.verifyRP("evil.example"); err == nil {
		t.Fatal("wrong rp accepted")
	}
}
