package main

import (
	"encoding/json"
	"net/http"
	"runtime"
)

func (a *App) handleTransportConfig(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var row struct {
		TorEnabled  bool
		BridgeType  *string
		BridgeCfg   json.RawMessage
		CustomRelay *string
	}
	err := a.db.QueryRow(r.Context(),
		`SELECT tor_enabled, bridge_type, bridge_config, custom_relay
		 FROM transport_isolation_config WHERE user_id=$1`, uid).
		Scan(&row.TorEnabled, &row.BridgeType, &row.BridgeCfg, &row.CustomRelay)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]bool{"tor_enabled": true})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (a *App) handleUpdateTransportConfig(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		TorEnabled  bool            `json:"tor_enabled"`
		BridgeType  string          `json:"bridge_type"`
		BridgeCfg   json.RawMessage `json:"bridge_config"`
		CustomRelay string          `json:"custom_relay"`
	}
	if !decodeJSON(w, r, &req) { return }
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO transport_isolation_config (user_id, tor_enabled, bridge_type, bridge_config, custom_relay)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (user_id) DO UPDATE SET tor_enabled=$2, bridge_type=$3, bridge_config=$4, custom_relay=$5, updated_at=now()`,
		uid, req.TorEnabled, req.BridgeType, req.BridgeCfg, req.CustomRelay)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (a *App) handleListBridges(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT bridge_type, address, fingerprint, capacity, region, active
		 FROM censorship_bridges WHERE active=true ORDER BY capacity DESC LIMIT 20`)
	if err != nil { writeErr(w, http.StatusInternalServerError, "query failed"); return }
	defer rows.Close()
	type Bridge struct {
		Type string `json:"type"`
		Addr string `json:"address"`
		FP   string `json:"fingerprint"`
		Cap  int    `json:"capacity"`
		Reg  string `json:"region"`
		Act  bool   `json:"active"`
	}
	var bridges []Bridge
	for rows.Next() {
		var b Bridge
		rows.Scan(&b.Type, &b.Addr, &b.FP, &b.Cap, &b.Reg, &b.Act)
		bridges = append(bridges, b)
	}
	writeJSON(w, http.StatusOK, bridges)
}

func init() { _ = runtime.GOARCH }