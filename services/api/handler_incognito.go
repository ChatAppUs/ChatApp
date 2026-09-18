package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

type IncognitoSession struct {
	Token     string    `json:"session_token"`
	Pubkey    string    `json:"ephemeral_pubkey"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Active    bool      `json:"active"`
}

func (a *App) handleStartIncognitoSession(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	token := make([]byte, 32)
	rand.Read(token)
	pubkey := make([]byte, 32)
	rand.Read(pubkey)
	tokenStr := hex.EncodeToString(token)
	pubkeyStr := hex.EncodeToString(pubkey)

	var sid string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO incognito_sessions (user_id, session_token, ephemeral_pubkey, expires_at)
		 VALUES ($1, $2, $3, now() + interval '24 hours') RETURNING id`,
		uid, tokenStr, pubkeyStr).Scan(&sid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create incognito session")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"session_token": tokenStr, "public_key": pubkeyStr, "id": sid,
	})
}

func (a *App) handleListIncognitoSessions(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT session_token, ephemeral_pubkey, created_at, expires_at, active
		 FROM incognito_sessions WHERE user_id=$1 AND active=true AND expires_at > now()
		 ORDER BY created_at DESC`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	var sessions []IncognitoSession
	for rows.Next() {
		var s IncognitoSession
		if rows.Scan(&s.Token, &s.Pubkey, &s.CreatedAt, &s.ExpiresAt, &s.Active) == nil {
			sessions = append(sessions, s)
		}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (a *App) handleEndIncognitoSession(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	token := r.URL.Query().Get("token")
	if token == "" {
		writeErr(w, http.StatusBadRequest, "token required")
		return
	}
	_, err := a.db.Exec(r.Context(),
		`UPDATE incognito_sessions SET active=false WHERE user_id=$1 AND session_token=$2`,
		uid, token)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to end session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
}
