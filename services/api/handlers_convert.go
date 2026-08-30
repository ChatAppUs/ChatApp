package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Crypto convert: USD-quoted rates (admin-maintained) drive atomic swaps in
// the double-entry ledger. Quote and execution both compute amounts in SQL
// numeric arithmetic — no float drift.

func (a *App) handleConvertRates(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT cr.asset, cr.chain, cr.usd_rate::text, cr.updated_at
		 FROM convert_rates cr
		 JOIN platform_tokens pt ON pt.symbol = cr.asset AND pt.chain = cr.chain
		 WHERE pt.enabled AND pt.convert_enabled
		 ORDER BY cr.asset, cr.chain`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load rates")
		return
	}
	defer rows.Close()
	type rate struct {
		Asset     string    `json:"asset"`
		Chain     string    `json:"chain"`
		USDRate   string    `json:"usd_rate"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	out := []rate{}
	for rows.Next() {
		var x rate
		if err := rows.Scan(&x.Asset, &x.Chain, &x.USDRate, &x.UpdatedAt); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rates": out})
}

// convertQuote computes the output amount for a pair/amount.
// Returns (toAmount, effectiveRate, ok).
func (a *App) convertQuote(r *http.Request, fromAsset, fromChain, toAsset, toChain, amount string) (string, string, bool) {
	var toAmount, rate string
	err := a.db.QueryRow(r.Context(),
		`WITH f AS (SELECT usd_rate FROM convert_rates WHERE asset=$1 AND chain=$2),
		      t AS (SELECT usd_rate FROM convert_rates WHERE asset=$3 AND chain=$4)
		 SELECT ($5::numeric * f.usd_rate / t.usd_rate)::text,
		        (f.usd_rate / t.usd_rate)::text
		 FROM f, t
		 WHERE $5::numeric > 0`,
		strings.ToUpper(fromAsset), strings.ToLower(fromChain),
		strings.ToUpper(toAsset), strings.ToLower(toChain), amount).
		Scan(&toAmount, &rate)
	if err != nil {
		return "", "", false
	}
	return toAmount, rate, true
}

func (a *App) handleConvertQuote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	to, rate, ok := a.convertQuote(r, q.Get("from_asset"), q.Get("from_chain"),
		q.Get("to_asset"), q.Get("to_chain"), q.Get("amount"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "no rate for this pair or invalid amount")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"to_amount": to, "rate": rate})
}

func (a *App) handleConvert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromAsset string `json:"from_asset"`
		FromChain string `json:"from_chain"`
		ToAsset   string `json:"to_asset"`
		ToChain   string `json:"to_chain"`
		Amount    string `json:"amount"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.EqualFold(req.FromAsset, req.ToAsset) && strings.EqualFold(req.FromChain, req.ToChain) {
		writeErr(w, http.StatusBadRequest, "source and destination must differ")
		return
	}
	_, _, _, fromConvert, _, _, okFrom := a.tokenFlags(r.Context(), req.FromAsset, req.FromChain)
	_, _, _, toConvert, _, _, okTo := a.tokenFlags(r.Context(), req.ToAsset, req.ToChain)
	if !okFrom || !okTo || !fromConvert || !toConvert {
		writeErr(w, http.StatusBadRequest, "convert not enabled for this pair")
		return
	}
	toAmount, rate, ok := a.convertQuote(r, req.FromAsset, req.FromChain, req.ToAsset, req.ToChain, req.Amount)
	if !ok {
		writeErr(w, http.StatusBadRequest, "no rate for this pair or invalid amount")
		return
	}
	uid := userIDFrom(r)

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "conversion failed")
		return
	}
	defer tx.Rollback(r.Context())

	fromAsset := strings.ToUpper(req.FromAsset)
	fromChain := strings.ToLower(req.FromChain)
	toAsset := strings.ToUpper(req.ToAsset)
	toChain := strings.ToLower(req.ToChain)

	var fromAcct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset=$2 AND chain=$3 FOR UPDATE`,
		uid, fromAsset, fromChain).Scan(&fromAcct)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusBadRequest, "no wallet account for the source asset")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "conversion failed")
		return
	}
	var enough bool
	_ = tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
		 FROM ledger_entries WHERE account_id=$2`, req.Amount, fromAcct).Scan(&enough)
	if !enough {
		writeErr(w, http.StatusBadRequest, "insufficient balance or invalid amount")
		return
	}
	toAcct, err := a.ensureAccount(r.Context(), uid, toAsset, toChain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "conversion failed")
		return
	}
	var ledgerTx string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&ledgerTx)
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
		 VALUES ($1,$2, -$3::numeric, 'convert_out', $4||' -> '||$5),
		        ($1,$6,  $7::numeric, 'convert_in',  $4||' -> '||$5)`,
		ledgerTx, fromAcct, req.Amount, fromAsset, toAsset, toAcct, toAmount); err != nil {
		writeErr(w, http.StatusInternalServerError, "conversion failed")
		return
	}
	var id string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO conversions (user_id, from_asset, from_chain, to_asset, to_chain,
		                          from_amount, to_amount, rate, ledger_tx)
		 VALUES ($1,$2,$3,$4,$5,$6::numeric,$7::numeric,$8::numeric,$9) RETURNING id`,
		uid, fromAsset, fromChain, toAsset, toChain, req.Amount, toAmount, rate, ledgerTx).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "conversion failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "conversion failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "to_amount": toAmount, "rate": rate,
	})
}

func (a *App) handleConvertHistory(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, from_asset, from_chain, to_asset, to_chain,
		        from_amount::text, to_amount::text, rate::text, created_at
		 FROM conversions WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userIDFrom(r), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load conversions")
		return
	}
	defer rows.Close()
	type conv struct {
		ID         string    `json:"id"`
		FromAsset  string    `json:"from_asset"`
		FromChain  string    `json:"from_chain"`
		ToAsset    string    `json:"to_asset"`
		ToChain    string    `json:"to_chain"`
		FromAmount string    `json:"from_amount"`
		ToAmount   string    `json:"to_amount"`
		Rate       string    `json:"rate"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := []conv{}
	for rows.Next() {
		var c conv
		if err := rows.Scan(&c.ID, &c.FromAsset, &c.FromChain, &c.ToAsset, &c.ToChain,
			&c.FromAmount, &c.ToAmount, &c.Rate, &c.CreatedAt); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversions": out})
}
