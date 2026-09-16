package main

// Unit tests for gap pack 11 (master-plan gaps) pure helpers. DB-backed
// behaviour is exercised by the repository's integration gates.

import (
	"strings"
	"testing"
)

func TestNormalizeScopes(t *testing.T) {
	scopes, bad := normalizeScopes([]string{"wallet:read", "profile", "profile"})
	if bad != "" {
		t.Fatalf("expected all scopes valid, got %q", bad)
	}
	if len(scopes) != 2 || scopes[0] != "profile" || scopes[1] != "wallet:read" {
		t.Fatalf("scope dedupe/order wrong: %v", scopes)
	}
	if _, bad = normalizeScopes([]string{"admin:*"}); bad == "" {
		t.Fatal("admin:* must be rejected")
	}
	def, bad := normalizeScopes(nil)
	if bad != "" || len(def) != 1 || def[0] != "identity" {
		t.Fatalf("empty scope list must default to identity, got %v %q", def, bad)
	}
}

func TestNormalizeRedirectURIs(t *testing.T) {
	uris, bad := normalizeRedirectURIs([]string{"https://app.example.com", "https://app.example.com"})
	if bad != "" {
		t.Fatalf("expected https uris accepted, got %q", bad)
	}
	if len(uris) != 1 {
		t.Fatalf("duplicates must collapse: %v", uris)
	}
	if _, bad = normalizeRedirectURIs([]string{"http://app.example.com"}); bad == "" {
		t.Fatal("plain http redirect URIs must be rejected")
	}
	if _, bad = normalizeRedirectURIs([]string{"https://app.example.com/callback"}); bad == "" {
		t.Fatal("redirect URIs with paths must be rejected (host-only policy)")
	}
	if _, bad = normalizeRedirectURIs([]string{"https://"}); bad == "" {
		t.Fatal("bare host must be rejected")
	}
}

func TestSanitizeChapterTitle(t *testing.T) {
	if got, ok := sanitizeChapterTitle("  Intro & welcome  "); !ok || got != "Intro & welcome" {
		t.Fatalf("title normalisation wrong: %q ok=%v", got, ok)
	}
	if _, ok := sanitizeChapterTitle(strings.Repeat("x", 141)); ok {
		t.Fatal("over-long titles must be rejected")
	}
	if _, ok := sanitizeChapterTitle("   "); ok {
		t.Fatal("blank titles must be rejected")
	}
}

func TestValidateChapterMS(t *testing.T) {
	if ms, ok := validateChapterMS("90000"); !ok || ms != 90000 {
		t.Fatalf("90s expected 90000ms, got %d ok=%v", ms, ok)
	}
	if _, ok := validateChapterMS("-5"); ok {
		t.Fatal("negative chapter times must be rejected")
	}
	if _, ok := validateChapterMS("90000000"); ok {
		t.Fatal("times beyond 24h must be rejected")
	}
}

func TestSummarizeDiscussion(t *testing.T) {
	bodies := []string{
		"The launch date moved to Friday because of the app store review.",
		"Can someone share the new build link?",
		"Launch moved to Friday, build link is in the pinned comment.",
		"Friday works for me.",
	}
	themes, sentences := summarizeDiscussion(bodies)
	joined := strings.Join(themes, " ")
	if !strings.Contains(joined, "friday") {
		t.Fatalf("summary should surface the dominant topic, got %q", joined)
	}
	found := false
	for _, q := range sentences {
		if strings.Contains(q, "build link") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sentences should include %q, got %v", "build link", sentences)
	}
	if s, q := summarizeDiscussion([]string{"hi"}); len(s) != 0 || len(q) != 0 {
		t.Fatal("tiny threads must produce no summary")
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	if hashToken("abc") != hashToken("abc") {
		t.Fatal("token hashing must be deterministic")
	}
	if hashToken("abc") == hashToken("abd") {
		t.Fatal("token hashing must differ per input")
	}
	if len(hashToken("abc")) != 64 {
		t.Fatalf("expected sha256 hex, got len %d", len(hashToken("abc")))
	}
}

func TestRandomHexUnique(t *testing.T) {
	a, b := randomHex(16), randomHex(16)
	if a == b {
		t.Fatal("randomHex must not repeat")
	}
	if len(a) != 32 {
		t.Fatalf("expected 32 hex chars for 16 bytes, got %d", len(a))
	}
}
