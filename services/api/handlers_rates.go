package main

import (
	"net/http"
)

// Price discovery from our own P2P order book: completed trades over the
// last 7 days give a volume-weighted implied rate per asset-vs-USD pair.
// Admins can preview the derived rates and apply them to convert_rates
// (rows flagged auto_from_p2p keep tracking the order book).

// GET /api/admin/convert/rates/derived — implied USD rates from completed
// P2P trades (VWAP over the last 7 days, assets with >= 3 trades only).
func (a *App) handleDerivedRates(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT t.asset,
                        COUNT(*) AS trades,
                        SUM(t.fiat_amount) / NULLIF(SUM(t.crypto_amount), 0) AS vwap
                 FROM p2p_trades t
                 WHERE t.status='completed' AND t.fiat_currency='USD'
                   AND t.updated_at > now() - interval '7 days'
                 GROUP BY t.asset
                 HAVING COUNT(*) >= 3
                 ORDER BY trades DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to derive rates")
		return
	}
	defer rows.Close()
	type derived struct {
		Asset  string  `json:"asset"`
		Trades int     `json:"trades"`
		VWAP   float64 `json:"usd_rate"`
	}
	out := []derived{}
	for rows.Next() {
		var d derived
		if err := rows.Scan(&d.Asset, &d.Trades, &d.VWAP); err == nil {
			out = append(out, d)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"derived": out, "window": "7d", "min_trades": 3})
}

// POST /api/admin/convert/rates/apply-derived — upsert convert_rates rows
// (asset -> USD) from the derived book prices and flag them auto_from_p2p.
func (a *App) handleApplyDerivedRates(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT t.asset, t.chain, SUM(t.fiat_amount) / NULLIF(SUM(t.crypto_amount), 0)
                 FROM p2p_trades t
                 WHERE t.status='completed' AND t.fiat_currency='USD'
                   AND t.updated_at > now() - interval '7 days'
                 GROUP BY t.asset, t.chain
                 HAVING COUNT(*) >= 3`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to derive rates")
		return
	}
	defer rows.Close()
	type pair struct {
		asset string
		chain string
		rate  float64
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		if rows.Scan(&p.asset, &p.chain, &p.rate) == nil && p.rate > 0 {
			pairs = append(pairs, p)
		}
	}
	applied := 0
	for _, p := range pairs {
		if _, err := a.db.Exec(r.Context(),
			`INSERT INTO convert_rates (asset, chain, usd_rate, auto_from_p2p, updated_at)
                         VALUES ($1,$2,$3,TRUE,now())
                         ON CONFLICT (asset, chain)
                         DO UPDATE SET usd_rate=$3, auto_from_p2p=TRUE, updated_at=now()`,
			p.asset, p.chain, p.rate); err == nil {
			applied++
		}
	}
	a.audit(r.Context(), userIDFrom(r), "apply_derived_rates", "", map[string]any{"applied": applied})
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied, "total_derived": len(pairs)})
}
