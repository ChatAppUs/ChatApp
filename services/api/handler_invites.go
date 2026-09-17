package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
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

func (a *App) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		MaxUses    int `json:"max_uses"`
		ExpiresInH int `json:"expires_in_hours"`
	}
	if !decodeJSON(w, r, &req) { return }
	if req.MaxUses <= 0 { req.MaxUses = 1 }
	if req.ExpiresInH <= 0 { req.ExpiresInH = 72 }
	code := make([]byte, 16)
	rand.Read(code)
	codeStr := hex.EncodeToString(code)
	var inv SingleUseInvite
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO single_use_invites (inviter_id, code, max_uses, expires_at)
		 VALUES ($1,$2,$3, now() + ($4 || ' hours')::interval)
		 RETURNING id, code, max_uses, use_count, expires_at, created_at, revoked`,
		uid, codeStr, req.MaxUses, req.ExpiresInH,
	).Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UseCount, &inv.ExpiresAt, &inv.CreatedAt, &inv.Revoked)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create invite")
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (a *App) handleListInvites(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, code, max_uses, use_count, expires_at, created_at, revoked
		 FROM single_use_invites WHERE inviter_id=$1 ORDER BY created_at DESC`, uid)
	if err != nil { writeErr(w, http.StatusInternalServerError, "query failed"); return }
	defer rows.Close()
	var invs []SingleUseInvite
	for rows.Next() {
		var inv SingleUseInvite
		rows.Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.UseCount, &inv.ExpiresAt, &inv.CreatedAt, &inv.Revoked)
		invs = append(invs, inv)
	}
	writeJSON(w, http.StatusOK, invs)
}

func (a *App) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	invID := r.PathValue("id")
	_, err := a.db.Exec(r.Context(),
		`UPDATE single_use_invites SET revoked=true WHERE id=$1 AND inviter_id=$2`, invID, uid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "invite not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}