package main

import (
	"time"
)

type SingleUseInvite struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	MaxUses   int       `json:"max_uses"`
	UseCount  int       `json:"use_count"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	Revoked   bool      `json:"revoked"`
}
