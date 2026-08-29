package main

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ---- Platform token management (admin plane) ----
//
// Superadmin/finance manage which tokens the built-in multichain wallet
// offers. Users only ever see enabled rows from platform_tokens.

var symbolRe = regexp.MustCompile(`^[A-Z0-9]{2,12}$`)
var chainRe = regexp.MustCompile(`^[a-z0-9-]{2,24}$`)

type platformToken struct {
	ID       string    `json:"id"`
	Symbol   string    `json:"symbol"`
	Name     string    `json:"name"`
	Chain    string    `json:"chain"`
	Contract *string   `json:"contract_address"`
	Decimals int       `json:"decimals"`
	LogoURL  string    `json:"logo_url"`
	IsNative bool      `json:"is_native"`
	Enabled  bool      `json:"enabled"`
	AddedBy  *string   `json:"added_by"`
	Created  time.Time `json:"created_at"`
}

func (a *App) handleAdminListTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, symbol, name, chain, contract_address, decimals,
		        COALESCE(logo_url,''), is_native, enabled, added_by, created_at
		 FROM platform_tokens ORDER BY symbol, chain`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tokens")
		return
	}
	defer rows.Close()
	out := []platformToken{}
	for rows.Next() {
		var t platformToken
		if err := rows.Scan(&t.ID, &t.Symbol, &t.Name, &t.Chain, &t.Contract,
			&t.Decimals, &t.LogoURL, &t.IsNative, &t.Enabled, &t.AddedBy, &t.Created); err == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (a *App) handleAdminAddToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Symbol   string `json:"symbol"`
		Name     string `json:"name"`
		Chain    string `json:"chain"`
		Contract string `json:"contract_address"`
		Decimals int    `json:"decimals"`
		LogoURL  string `json:"logo_url"`
		IsNative bool   `json:"is_native"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	req.Chain = strings.ToLower(strings.TrimSpace(req.Chain))
	req.Name = strings.TrimSpace(req.Name)
	if !symbolRe.MatchString(req.Symbol) || !chainRe.MatchString(req.Chain) || req.Name == "" {
		writeErr(w, http.StatusBadRequest, "valid symbol, chain and name required")
		return
	}
	if req.Decimals < 0 || req.Decimals > 36 {
		writeErr(w, http.StatusBadRequest, "decimals must be 0..36")
		return
	}
	if !req.IsNative && strings.TrimSpace(req.Contract) == "" {
		writeErr(w, http.StatusBadRequest, "contract address required for non-native tokens")
		return
	}
	uid := userIDFrom(r)
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO platform_tokens (symbol, name, chain, contract_address, decimals, logo_url, is_native, added_by)
		 VALUES ($1,$2,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8)
		 ON CONFLICT (symbol, chain) DO UPDATE SET
		   name=EXCLUDED.name, contract_address=EXCLUDED.contract_address,
		   decimals=EXCLUDED.decimals, logo_url=EXCLUDED.logo_url,
		   is_native=EXCLUDED.is_native, enabled=true
		 RETURNING id`,
		req.Symbol, req.Name, req.Chain, strings.TrimSpace(req.Contract),
		req.Decimals, strings.TrimSpace(req.LogoURL), req.IsNative, uid).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add token")
		return
	}
	a.audit(r.Context(), uid, "wallet.token_add", req.Symbol+"/"+req.Chain, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "enabled"})
}

func (a *App) handleAdminSetTokenStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	tag, err := a.db.Exec(r.Context(),
		`UPDATE platform_tokens SET enabled=$2 WHERE id=$1`, id, req.Enabled)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	}
	action := "wallet.token_disable"
	if req.Enabled {
		action = "wallet.token_enable"
	}
	a.audit(r.Context(), userIDFrom(r), action, id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (a *App) handleAdminDeleteToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Safety: refuse to delete a token that still has wallet accounts; disable it instead.
	var inUse bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM wallet_accounts wa
		 JOIN platform_tokens pt ON pt.symbol = wa.asset AND pt.chain = wa.chain
		 WHERE pt.id = $1)`, id).Scan(&inUse)
	if inUse {
		writeErr(w, http.StatusConflict, "token in use by wallets; disable it instead")
		return
	}
	tag, err := a.db.Exec(r.Context(), `DELETE FROM platform_tokens WHERE id=$1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "wallet.token_delete", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
