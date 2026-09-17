package main

import "net/http"

func (a *App) handleListCensorshipDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT domain, is_frontable, added_at FROM censorship_domains ORDER BY added_at DESC LIMIT 50`)
	if err != nil { writeErr(w, http.StatusInternalServerError, "query failed"); return }
	defer rows.Close()
	type Domain struct {
		Domain    string `json:"domain"`
		Frontable bool   `json:"is_frontable"`
		AddedAt   string `json:"added_at"`
	}
	var domains []Domain
	for rows.Next() {
		var d Domain
		var addedAt interface{}
		rows.Scan(&d.Domain, &d.Frontable, &addedAt)
		if s, ok := addedAt.(string); ok { d.AddedAt = s }
		domains = append(domains, d)
	}
	writeJSON(w, http.StatusOK, domains)
}

func (a *App) handleGetCensorshipStatus(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var torEnabled bool
	err := a.db.QueryRow(r.Context(),
		`SELECT tor_enabled FROM transport_isolation_config WHERE user_id=$1`, uid).Scan(&torEnabled)
	if err != nil { torEnabled = true }
	var bridgeCount int
	a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM censorship_bridges WHERE active=true`).Scan(&bridgeCount)
	writeJSON(w, http.StatusOK, map[string]any{
		"tor_configured": a.tor != nil && a.tor.cfg.Enabled,
		"tor_enabled":    torEnabled,
		"bridges_count":  bridgeCount,
	})
}