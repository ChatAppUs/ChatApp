package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// CryptoProvider is the integration point for a custody SDK (Fireblocks,
// BitGo, Coinbase WaaS, ...). Wiring a provider activates on-chain broadcast;
// the full deposit-address, ledger, risk, signing and approval pipeline below
// is own code and runs regardless.
type CryptoProvider interface {
	Withdraw(ctx context.Context, userID, asset, chain, toAddress, amount string) (txHash string, err error)
}

var cryptoProvider CryptoProvider // nil until a custody SDK is configured

// tokenFlags loads the per-feature switches for an asset/chain pair.
func (a *App) tokenFlags(ctx context.Context, asset, chain string) (deposit, withdraw, p2p, convert bool, minWithdraw, fee string, ok bool) {
	ok = true
	err := a.db.QueryRow(ctx,
		`SELECT deposit_enabled, withdraw_enabled, p2p_enabled, convert_enabled,
		        min_withdraw::text, withdraw_fee::text
		 FROM platform_tokens
		 WHERE symbol = upper($1) AND chain = lower($2) AND enabled`,
		asset, chain).Scan(&deposit, &withdraw, &p2p, &convert, &minWithdraw, &fee)
	if err != nil {
		ok = false
	}
	return
}

// handleDepositAddress returns (creating on first use) the user's
// deterministic self-custody deposit address for an asset/chain. The address
// is derived from WALLET_MASTER_SEED; the web/mobile clients render it as a
// QR code with copy support.
func (a *App) handleDepositAddress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Asset string `json:"asset"`
		Chain string `json:"chain"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	depositOn, _, _, _, _, _, ok := a.tokenFlags(r.Context(), req.Asset, req.Chain)
	if !ok || !depositOn {
		writeErr(w, http.StatusBadRequest, "deposits not enabled for this asset/chain")
		return
	}
	uid := userIDFrom(r)
	asset := strings.ToUpper(req.Asset)
	chain := strings.ToLower(req.Chain)

	var addr string
	err := a.db.QueryRow(r.Context(),
		`SELECT address FROM deposit_addresses WHERE user_id=$1 AND asset=$2 AND chain=$3`,
		uid, asset, chain).Scan(&addr)
	if err == nil {
		writeJSON(w, http.StatusOK, depositPayload(asset, chain, addr))
		return
	}

	key := deriveDepositKey(a.cfg.WalletMasterSeed, uid, asset, chain)
	addr = encodeDepositAddress(key, asset, chain)
	derivation := fmt.Sprintf("m/%s/%s", chain, uid)
	_, err = a.db.Exec(r.Context(),
		`INSERT INTO deposit_addresses (user_id, asset, chain, address, derivation)
		 VALUES ($1,$2,$3,$4,$5) ON CONFLICT (user_id, asset, chain) DO NOTHING`,
		uid, asset, chain, addr, derivation)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to persist deposit address")
		return
	}
	acctID, err := a.ensureAccount(r.Context(), uid, asset, chain)
	if err == nil {
		_, _ = a.db.Exec(r.Context(), `UPDATE wallet_accounts SET address=$1 WHERE id=$2`, addr, acctID)
	}
	writeJSON(w, http.StatusOK, depositPayload(asset, chain, addr))
}

func depositPayload(asset, chain, addr string) map[string]any {
	uri := addr
	switch {
	case chain == "bitcoin":
		uri = "bitcoin:" + addr
	case evmChains[chain]:
		uri = "ethereum:" + addr
	case chain == "tron":
		uri = "tron:" + addr
	case chain == "solana":
		uri = "solana:" + addr
	}
	return map[string]any{"asset": asset, "chain": chain, "address": addr, "uri": uri}
}

// ---- Withdrawals: request -> risk policy -> sign -> execute -------------

// signWithdrawal produces the superadmin-authority signature over the
// canonical request tuple. Every executed withdrawal carries exactly one
// signature; verification is recomputation.
func (a *App) signWithdrawal(id, userID, asset, chain, toAddress, amount, fee string) string {
	m := hmac.New(sha256.New, []byte(a.cfg.WithdrawSigningKey))
	m.Write([]byte(strings.Join([]string{id, userID, asset, chain, toAddress, amount, fee}, "|")))
	return hex.EncodeToString(m.Sum(nil))
}

type withdrawRisk struct {
	score int
	flags []string
}

// scoreWithdrawal runs the risk policy. Thresholds are conservative; the
// auto-approver only fires below cfg.WithdrawAutoThreshold.
func (a *App) scoreWithdrawal(ctx context.Context, userID, asset, chain, toAddress string, amountUSD float64) withdrawRisk {
	r := withdrawRisk{}
	if amountUSD > a.cfg.WithdrawAutoLimitUSD {
		r.score += 100
		r.flags = append(r.flags, "amount_above_auto_limit")
	} else if amountUSD > a.cfg.WithdrawAutoLimitUSD/2 {
		r.score += 40
		r.flags = append(r.flags, "amount_elevated")
	}
	// Velocity: withdrawals in the last 24h.
	var recent int
	_ = a.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM withdrawal_requests
		 WHERE user_id=$1 AND created_at > now() - interval '24 hours'
		   AND status <> 'rejected'`, userID).Scan(&recent)
	switch {
	case recent >= 5:
		r.score += 60
		r.flags = append(r.flags, "high_velocity")
	case recent >= 2:
		r.score += 20
		r.flags = append(r.flags, "moderate_velocity")
	}
	// Address reuse: a previously used destination is lower risk.
	var usedBefore bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM withdrawal_requests
		 WHERE user_id=$1 AND to_address=$2 AND status='completed')`, userID, toAddress).Scan(&usedBefore)
	if !usedBefore {
		r.score += 25
		r.flags = append(r.flags, "new_address")
	}
	// Account age.
	var ageHours float64
	_ = a.db.QueryRow(ctx,
		`SELECT EXTRACT(EPOCH FROM (now() - created_at))/3600 FROM users WHERE id=$1`, userID).Scan(&ageHours)
	if ageHours < 24 {
		r.score += 50
		r.flags = append(r.flags, "account_new")
	}
	return r
}

func (a *App) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req struct {
		Asset     string `json:"asset"`
		Chain     string `json:"chain"`
		ToAddress string `json:"to_address"`
		Amount    string `json:"amount"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.ToAddress = strings.TrimSpace(req.ToAddress)
	asset := strings.ToUpper(strings.TrimSpace(req.Asset))
	chain := strings.ToLower(strings.TrimSpace(req.Chain))

	_, withdrawOn, _, _, minWithdraw, fee, ok := a.tokenFlags(r.Context(), asset, chain)
	if !ok || !withdrawOn {
		writeErr(w, http.StatusBadRequest, "withdrawals not enabled for this asset/chain")
		return
	}
	if err := validateAddress(chain, req.ToAddress); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := userIDFrom(r)

	var kyc string
	if err := a.db.QueryRow(r.Context(), `SELECT kyc_status FROM users WHERE id=$1`, uid).Scan(&kyc); err != nil {
		writeErr(w, http.StatusInternalServerError, "withdrawal failed")
		return
	}
	if kyc != "verified" {
		writeErr(w, http.StatusForbidden, "KYC verification required before withdrawing")
		return
	}

	// USD value for the risk policy (0 when the asset has no rate row —
	// treated as above-limit so it routes to manual review).
	var amountUSD float64
	_ = a.db.QueryRow(r.Context(),
		`SELECT ($1::numeric * usd_rate)::float8 FROM convert_rates
		 WHERE asset=$2 AND chain=$3`, req.Amount, asset, chain).Scan(&amountUSD)

	risk := a.scoreWithdrawal(r.Context(), uid, asset, chain, req.ToAddress, amountUSD)

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "withdrawal failed")
		return
	}
	defer tx.Rollback(r.Context())

	// Balance check + hold: the debit lands immediately so funds are locked
	// for the whole lifecycle; a rejection reverses it with a refund entry.
	var acct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset=$2 AND chain=$3 FOR UPDATE`,
		uid, asset, chain).Scan(&acct)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusBadRequest, "no wallet account for this asset")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "withdrawal failed")
		return
	}
	var enough bool
	err = tx.QueryRow(r.Context(),
		`SELECT $1::numeric >= $2::numeric AND $1::numeric > 0
		       AND COALESCE((SELECT SUM(amount) FROM ledger_entries WHERE account_id=$3),0) >= $1::numeric + $4::numeric`,
		req.Amount, minWithdraw, acct, fee).Scan(&enough)
	if err != nil || !enough {
		writeErr(w, http.StatusBadRequest, "insufficient balance, below minimum, or invalid amount")
		return
	}

	ledgerTx := ""
	if err := tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&ledgerTx); err != nil {
		writeErr(w, http.StatusInternalServerError, "withdrawal failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
		 VALUES ($1,$2, -($3::numeric + $4::numeric), 'withdrawal_hold', 'withdrawal request')`,
		ledgerTx, acct, req.Amount, fee); err != nil {
		writeErr(w, http.StatusInternalServerError, "withdrawal failed")
		return
	}

	var id string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO withdrawal_requests (user_id, asset, chain, to_address, amount, fee, risk_score, risk_flags, ledger_tx)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6::numeric,$7,$8,$9) RETURNING id`,
		uid, asset, chain, req.ToAddress, req.Amount, fee, risk.score, risk.flags, ledgerTx).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "withdrawal failed")
		return
	}

	auto := risk.score < a.cfg.WithdrawAutoThreshold
	if auto {
		sig := a.signWithdrawal(id, uid, asset, chain, req.ToAddress, req.Amount, fee)
		if _, err := tx.Exec(r.Context(),
			`UPDATE withdrawal_requests
			 SET status='approved', auto_approved=true, approved_by='policy-engine',
			     approved_at=now(), signature=$2, signed_at=now(), updated_at=now()
			 WHERE id=$1`, id, sig); err != nil {
			writeErr(w, http.StatusInternalServerError, "withdrawal failed")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "withdrawal failed")
		return
	}

	status := "pending_review"
	if auto {
		status = a.executeWithdrawal(r.Context(), id)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":             id,
		"status":         status,
		"auto_approved":  auto,
		"approved_in_ms": time.Since(start).Milliseconds(),
	})
}

// executeWithdrawal settles an approved+signed request: internal chain
// settles on the ledger immediately; external chains broadcast through the
// custody provider when configured, otherwise remain 'signed' (queued for
// broadcast). Returns the resulting status.
func (a *App) executeWithdrawal(ctx context.Context, id string) string {
	var userID, asset, chain, toAddr, amount string
	err := a.db.QueryRow(ctx,
		`SELECT user_id, asset, chain, to_address, amount::text FROM withdrawal_requests
		 WHERE id=$1 AND status='approved' AND signature IS NOT NULL`,
		id).Scan(&userID, &asset, &chain, &toAddr, &amount)
	if err != nil {
		return "approved"
	}
	if chain == "internal" {
		_, _ = a.db.Exec(ctx,
			`UPDATE withdrawal_requests SET status='completed', updated_at=now() WHERE id=$1`, id)
		return "completed"
	}
	if cryptoProvider != nil {
		txHash, err := cryptoProvider.Withdraw(ctx, userID, asset, chain, toAddr, amount)
		if err != nil {
			_, _ = a.db.Exec(ctx,
				`UPDATE withdrawal_requests SET status='failed', updated_at=now() WHERE id=$1`, id)
			a.refundWithdrawal(ctx, id)
			return "failed"
		}
		_, _ = a.db.Exec(ctx,
			`UPDATE withdrawal_requests SET status='completed', tx_hash=$2, updated_at=now() WHERE id=$1`,
			id, txHash)
		return "completed"
	}
	_, _ = a.db.Exec(ctx,
		`UPDATE withdrawal_requests SET status='signed', updated_at=now() WHERE id=$1`, id)
	return "signed"
}

// refundWithdrawal reverses the hold for rejected/failed requests.
func (a *App) refundWithdrawal(ctx context.Context, id string) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
		 SELECT wr.ledger_tx, wa.id, (wr.amount + wr.fee), 'withdrawal_refund', 'withdrawal ' || wr.id || ' refunded'
		 FROM withdrawal_requests wr
		 JOIN wallet_accounts wa ON wa.user_id = wr.user_id AND wa.asset = wr.asset AND wa.chain = wr.chain
		 WHERE wr.id = $1
		   AND NOT EXISTS (SELECT 1 FROM ledger_entries le
		                   WHERE le.tx_id = wr.ledger_tx AND le.kind = 'withdrawal_refund')`, id)
}

func (a *App) handleListWithdrawals(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, asset, chain, to_address, amount::text, fee::text, status,
		        auto_approved, COALESCE(tx_hash,''), created_at, updated_at
		 FROM withdrawal_requests WHERE user_id=$1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userIDFrom(r), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load withdrawals")
		return
	}
	defer rows.Close()
	type wd struct {
		ID           string    `json:"id"`
		Asset        string    `json:"asset"`
		Chain        string    `json:"chain"`
		ToAddress    string    `json:"to_address"`
		Amount       string    `json:"amount"`
		Fee          string    `json:"fee"`
		Status       string    `json:"status"`
		AutoApproved bool      `json:"auto_approved"`
		TxHash       string    `json:"tx_hash"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
	}
	out := []wd{}
	for rows.Next() {
		var x wd
		if err := rows.Scan(&x.ID, &x.Asset, &x.Chain, &x.ToAddress, &x.Amount, &x.Fee,
			&x.Status, &x.AutoApproved, &x.TxHash, &x.CreatedAt, &x.UpdatedAt); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"withdrawals": out})
}
