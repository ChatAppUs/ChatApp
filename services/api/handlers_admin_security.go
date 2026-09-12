package main

import (
	"net/http"
)

func (a *App) handleAdminSecurityAttestations(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
		SELECT s.id, s.user_id, u.username, s.purpose, s.score,
		       s.verified_at, s.expires_at
		FROM security_attestations s
		JOIN users u ON u.id=s.user_id
		ORDER BY s.created_at DESC LIMIT 200`)
	if err != nil { writeErr(w, http.StatusInternalServerError, "failed to load security attestations"); return }
	defer rows.Close()
	type item struct { ID string `json:"id"`; UserID string `json:"user_id"`; Username string `json:"username"`; Purpose string `json:"purpose"`; Score float64 `json:"score"`; VerifiedAt string `json:"verified_at"`; ExpiresAt string `json:"expires_at"` }
	items := make([]item, 0)
	for rows.Next() {
		var x item
		if err := rows.Scan(&x.ID, &x.UserID, &x.Username, &x.Purpose, &x.Score, &x.VerifiedAt, &x.ExpiresAt); err == nil { items = append(items, x) }
	}
	writeJSON(w, http.StatusOK, map[string]any{"attestations": items})
}
