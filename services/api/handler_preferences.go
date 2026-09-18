package main

import (
	"encoding/json"
	"net/http"
)

type AnonymousPreferences struct {
	TorRequired        bool            `json:"tor_required"`
	IncognitoByDefault bool            `json:"incognito_by_default"`
	ScreenshotBlock    bool            `json:"screenshot_block"`
	MetadataEncryption bool            `json:"metadata_encryption"`
	ReadReceipts       bool            `json:"read_receipts"`
	TypingIndicators   bool            `json:"typing_indicators"`
	PerContact         map[string]any  `json:"per_contact"`
	TransportIsolation map[string]bool `json:"transport_isolation"`
}

func defaultAnonymousPreferences() *AnonymousPreferences {
	return &AnonymousPreferences{
		TorRequired: true, IncognitoByDefault: false, ScreenshotBlock: true,
		MetadataEncryption: true, ReadReceipts: false, TypingIndicators: false,
		PerContact:         make(map[string]any),
		TransportIsolation: map[string]bool{"tor": true, "bridge": false},
	}
}

func (a *App) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var raw []byte
	err := a.db.QueryRow(r.Context(), "SELECT anon_preferences FROM users WHERE id=$1", uid).Scan(&raw)
	if err != nil || len(raw) == 0 {
		writeJSON(w, http.StatusOK, defaultAnonymousPreferences())
		return
	}
	var p AnonymousPreferences
	if json.Unmarshal(raw, &p) != nil {
		writeJSON(w, http.StatusOK, defaultAnonymousPreferences())
		return
	}
	writeJSON(w, http.StatusOK, &p)
}

func (a *App) handleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var p AnonymousPreferences
	if !decodeJSON(w, r, &p) {
		return
	}
	data, _ := json.Marshal(p)
	_, err := a.db.Exec(r.Context(), "UPDATE users SET anon_preferences=$2 WHERE id=$1", uid, data)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update preferences")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
