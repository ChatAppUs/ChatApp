package main

import (
	"testing"
	"time"
)

func TestAccessClaimsCarrySessionBinding(t *testing.T) {
	token, err := signJWT([]byte("test-secret"), Claims{
		Sub: "user-1", Type: "access", JTI: "session-1",
		Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := parseJWT([]byte("test-secret"), token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.JTI != "session-1" {
		t.Fatalf("expected session binding, got %q", claims.JTI)
	}
}

func TestAccessClaimsWithoutSessionBindingAreNotUsable(t *testing.T) {
	token, err := signJWT([]byte("test-secret"), Claims{
		Sub: "user-1", Type: "access",
		Iat: time.Now().Unix(), Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := parseJWT([]byte("test-secret"), token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.JTI != "" {
		t.Fatalf("expected no session binding in legacy token")
	}
}
