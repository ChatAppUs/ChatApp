package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

type DeviceSession struct {
	ID           string    `json:"id"`
	DeviceID     string    `json:"device_id"`
	DeviceName   string    `json:"device_name"`
	Platform     string    `json:"platform"`
	PublicKey    string    `json:"public_key"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
}

func (a *App) handleListDevices(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, device_id, device_name, platform, public_key, created_at, last_active_at
		 FROM multi_device_sessions WHERE user_id=$1 AND revoked=false ORDER BY last_active_at DESC`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	var devices []DeviceSession
	for rows.Next() {
		var d DeviceSession
		rows.Scan(&d.ID, &d.DeviceID, &d.DeviceName, &d.Platform, &d.PublicKey, &d.CreatedAt, &d.LastActiveAt)
		devices = append(devices, d)
	}
	writeJSON(w, http.StatusOK, devices)
}

func (a *App) handleLinkDevice(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		DeviceID   string `json:"device_id"`
		DeviceName string `json:"device_name"`
		Platform   string `json:"platform"`
		PublicKey  string `json:"public_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	token := make([]byte, 32)
	rand.Read(token)
	var ds DeviceSession
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO multi_device_sessions (user_id, device_id, device_name, platform, public_key, session_token)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING id, device_id, device_name, platform, public_key, created_at, last_active_at`,
		uid, req.DeviceID, req.DeviceName, req.Platform, req.PublicKey, hex.EncodeToString(token),
	).Scan(&ds.ID, &ds.DeviceID, &ds.DeviceName, &ds.Platform, &ds.PublicKey, &ds.CreatedAt, &ds.LastActiveAt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to link device")
		return
	}
	writeJSON(w, http.StatusCreated, ds)
}

func (a *App) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	deviceID := r.PathValue("id")
	_, err := a.db.Exec(r.Context(),
		`UPDATE multi_device_sessions SET revoked=true WHERE user_id=$1 AND device_id=$2`, uid, deviceID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "device not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
