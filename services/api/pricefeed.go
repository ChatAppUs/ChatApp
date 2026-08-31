package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Live crypto prices. Three sources, in strict precedence order:
//   1) admin   - manual override (PUT /api/admin/prices/{asset}/{chain});
//                the worker never overwrites an admin override.
//   2) coingecko - polled ~every 75s for every token that has a coingecko_id.
//   3) orderbook - mean of our own active P2P offers quoted in USD; used
//                lazily on read when a token has no price at all.
// GET /api/prices is the single read surface for every client.

type priceRow struct {
	Asset     string     `json:"asset"`
	Chain     string     `json:"chain"`
	Name      string     `json:"name"`
	LogoURL   string     `json:"logo_url"`
	PriceUSD  *string    `json:"price_usd"`
	Source    *string    `json:"source"`
	UpdatedAt *time.Time `json:"updated_at"`
}

func (a *App) handlePrices(w http.ResponseWriter, r *http.Request) {
	a.fillOrderbookPrices(r.Context())
	rows, err := a.db.Query(r.Context(),
		`SELECT pt.symbol, pt.chain, pt.name, COALESCE(pt.logo_url,''),
                        cp.price_usd::text, cp.source, cp.updated_at
                 FROM platform_tokens pt
                 LEFT JOIN crypto_prices cp ON cp.asset=pt.symbol AND cp.chain=pt.chain
                 WHERE pt.enabled ORDER BY pt.symbol, pt.chain`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load prices")
		return
	}
	defer rows.Close()
	out := []priceRow{}
	for rows.Next() {
		var p priceRow
		if err := rows.Scan(&p.Asset, &p.Chain, &p.Name, &p.LogoURL,
			&p.PriceUSD, &p.Source, &p.UpdatedAt); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": out})
}

// fillOrderbookPrices derives prices for tokens without any quote from our
// own P2P order book (average of active offers priced in USD).
func (a *App) fillOrderbookPrices(ctx context.Context) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO crypto_prices (asset, chain, price_usd, source)
                 SELECT asset, chain, AVG(price), 'orderbook'
                 FROM p2p_offers
                 WHERE active AND fiat_currency='USD'
                   AND (asset, chain) NOT IN (SELECT asset, chain FROM crypto_prices)
                 GROUP BY asset, chain
                 HAVING COUNT(*) > 0
                 ON CONFLICT (asset, chain) DO NOTHING`)
}

func (a *App) startPriceWorker() {
	go func() {
		a.pollCoinGecko(context.Background())
		ticker := time.NewTicker(75 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.pollCoinGecko(context.Background())
		}
	}()
}

func (a *App) pollCoinGecko(ctx context.Context) {
	rows, err := a.db.Query(ctx,
		`SELECT symbol, chain, coingecko_id FROM platform_tokens
                 WHERE enabled AND coingecko_id != ''`)
	if err != nil {
		return
	}
	defer rows.Close()
	type tok struct{ sym, chain, cg string }
	tokens := []tok{}
	ids := []string{}
	for rows.Next() {
		var t tok
		if rows.Scan(&t.sym, &t.chain, &t.cg) == nil {
			tokens = append(tokens, t)
			ids = append(ids, t.cg)
		}
	}
	if len(ids) == 0 {
		return
	}
	url := "https://api.coingecko.com/api/v3/simple/price?vs_currencies=usd&ids=" +
		strings.Join(ids, ",")
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var payload map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return
	}
	for _, t := range tokens {
		price, ok := payload[t.cg]["usd"]
		if !ok || price <= 0 {
			continue
		}
		// Admin overrides (source='admin') take precedence; coingecko
		// only fills/refresh its own rows.
		_, _ = a.db.Exec(ctx,
			`INSERT INTO crypto_prices (asset, chain, price_usd, source)
                         VALUES ($1,$2,$3::numeric,'coingecko')
                         ON CONFLICT (asset, chain) DO UPDATE
                           SET price_usd=EXCLUDED.price_usd, updated_at=now()
                           WHERE crypto_prices.source != 'admin'`,
			t.sym, t.chain, fmt.Sprintf("%f", price))
	}
}

func (a *App) handleAdminPriceOverride(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PriceUSD string `json:"price_usd"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.PriceUSD) == "" {
		writeErr(w, http.StatusBadRequest, "price_usd required")
		return
	}
	asset := strings.ToUpper(r.PathValue("asset"))
	chain := strings.ToLower(r.PathValue("chain"))
	var n int
	if err := a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM platform_tokens WHERE symbol=$1 AND chain=$2`,
		asset, chain).Scan(&n); err != nil || n == 0 {
		writeErr(w, http.StatusNotFound, "token not listed")
		return
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO crypto_prices (asset, chain, price_usd, source)
                 VALUES ($1,$2,$3::numeric,'admin')
                 ON CONFLICT (asset, chain) DO UPDATE
                   SET price_usd=$3::numeric, source='admin', updated_at=now()`,
		asset, chain, req.PriceUSD)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid price")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "prices.override", asset+"/"+chain, map[string]any{"usd": req.PriceUSD})
	writeJSON(w, http.StatusOK, map[string]string{"status": "set"})
}
