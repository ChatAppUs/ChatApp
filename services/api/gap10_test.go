package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestNormalisePhone(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"+1 (415) 555-2671", "+14155552671", true},
		{"+44 20 7946 0958", "+442079460958", true},
		{" +81-3-1234-5678 ", "+81312345678", true},
		{"", "", false},
		{"not a phone", "", false},
		{"abc123", "", false},
		// No country code: rejected, never guessed (client supplies E.164).
		{"415.555.2671", "", false},
		{"1-415-555-2671", "", false},
	}
	for _, c := range cases {
		got, ok := normalisePhone(c.in)
		if ok != c.wantOK {
			t.Errorf("normalisePhone(%q) ok=%v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && got != c.want {
			t.Errorf("normalisePhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPepperPhoneHash(t *testing.T) {
	h1 := pepperPhoneHash("pepper", "+14155552671")
	h2 := pepperPhoneHash("pepper", "+14155552671")
	h3 := pepperPhoneHash("other-pepper", "+14155552671")

	if h1 != h2 {
		t.Fatal("same inputs must produce identical digests")
	}
	if h1 == h3 {
		t.Fatal("different peppers must produce different digests")
	}

	mac := hmac.New(sha256.New, []byte("pepper"))
	mac.Write([]byte("+14155552671"))
	if h1 != hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("digest must be HMAC-SHA256(pepper, e164) hex")
	}

	// 64 hex chars = 32 bytes.
	if len(h1) != 64 {
		t.Fatalf("digest length = %d, want 64", len(h1))
	}
}

func TestAgeClass(t *testing.T) {
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	adult := time.Date(2000, 1, 15, 0, 0, 0, 0, time.UTC)
	cls, years, ok := ageClass(adult, now)
	if !ok || cls != "adult" {
		t.Fatalf("adult: cls=%q ok=%v", cls, ok)
	}
	if years != 26 {
		t.Fatalf("adult age = %d, want 26", years)
	}

	// 13th birthday today: exactly 13 -> minor (under 18), age computed right.
	thirteen := now.AddDate(-13, 0, 0)
	cls, years, _ = ageClass(thirteen, now)
	if cls != "minor" || years != 13 {
		t.Fatalf("exactly 13: cls=%q years=%d, want minor/13", cls, years)
	}

	// Day before the 13th birthday: still a minor, age 12.
	almost := now.AddDate(-13, 0, 1)
	cls, years, _ = ageClass(almost, now)
	if cls != "minor" || years != 12 {
		t.Fatalf("day before 13th birthday: cls=%q years=%d, want minor/12", cls, years)
	}

	// Future date of birth is invalid.
	future := now.AddDate(1, 0, 0)
	if _, _, ok := ageClass(future, now); ok {
		t.Fatal("future dob must be rejected")
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike("50%_off"); got != `50\%\_off` {
		t.Fatalf("escapeLike = %q", got)
	}
	if got := escapeLike("plain"); got != "plain" {
		t.Fatalf("escapeLike plain = %q", got)
	}
}
