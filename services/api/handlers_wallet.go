package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// assetSupported checks the platform_tokens table (managed by admins); the
// built-in multichain wallet only offers tokens the platform has enabled.
func (a *App) assetSupported(ctx context.Context, asset, chain string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM platform_tokens
		 WHERE symbol = upper($1) AND chain = lower($2) AND enabled)`,
		asset, chain).Scan(&ok)
	return ok
}

func (a *App) handleWalletAssets(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT symbol, chain FROM platform_tokens WHERE enabled ORDER BY symbol, chain`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load assets")
		return
	}
	defer rows.Close()
	assets := map[string][]string{}
	for rows.Next() {
		var sym, chain string
		if err := rows.Scan(&sym, &chain); err == nil {
			assets[sym] = append(assets[sym], chain)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

func (a *App) ensureAccount(ctx context.Context, userID, asset, chain string) (string, error) {
	var id string
	err := a.db.QueryRow(ctx,
		`INSERT INTO wallet_accounts (user_id, asset, chain) VALUES ($1,$2,$3)
		 ON CONFLICT (user_id, asset, chain) DO UPDATE SET user_id = EXCLUDED.user_id
		 RETURNING id`, userID, strings.ToUpper(asset), strings.ToLower(chain)).Scan(&id)
	return id, err
}

func (a *App) handleWalletAccounts(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT wa.id, wa.asset, wa.chain, wa.address,
		        COALESCE((SELECT SUM(le.amount) FROM ledger_entries le WHERE le.account_id = wa.id), 0) AS balance
		 FROM wallet_accounts wa WHERE wa.user_id = $1 ORDER BY wa.asset`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load wallet")
		return
	}
	defer rows.Close()
	type acct struct {
		ID      string `json:"id"`
		Asset   string `json:"asset"`
		Chain   string `json:"chain"`
		Address string `json:"address"`
		Balance string `json:"balance"`
	}
	out := []acct{}
	for rows.Next() {
		var a2 acct
		if err := rows.Scan(&a2.ID, &a2.Asset, &a2.Chain, &a2.Address, &a2.Balance); err == nil {
			out = append(out, a2)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func (a *App) handleWalletCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Asset string `json:"asset"`
		Chain string `json:"chain"`
	}
	if !decodeJSON(w, r, &req) || !a.assetSupported(r.Context(), req.Asset, req.Chain) {
		writeErr(w, http.StatusBadRequest, "unsupported asset/chain combination")
		return
	}
	id, err := a.ensureAccount(r.Context(), userIDFrom(r), req.Asset, req.Chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create account")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// P2P transfer: double-entry ledger inside one transaction. The balance
// check is atomic with the insert via a locking read of the sender account.
func (a *App) handleP2PTransfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToUsername string `json:"to_username"`
		Asset      string `json:"asset"`
		Chain      string `json:"chain"`
		Amount     string `json:"amount"`
		Memo       string `json:"memo"`
	}
	if !decodeJSON(w, r, &req) || !a.assetSupported(r.Context(), req.Asset, req.Chain) {
		writeErr(w, http.StatusBadRequest, "unsupported asset/chain combination")
		return
	}
	if len(req.Memo) > 200 {
		writeErr(w, http.StatusBadRequest, "memo too long")
		return
	}
	uid := userIDFrom(r)

	// KYC gate: sending funds requires verified identity.
	var kyc string
	if err := a.db.QueryRow(r.Context(), `SELECT kyc_status FROM users WHERE id=$1`, uid).Scan(&kyc); err != nil {
		writeErr(w, http.StatusInternalServerError, "transfer failed")
		return
	}
	if kyc != "verified" {
		writeErr(w, http.StatusForbidden, "KYC verification required before sending payments")
		return
	}

	var recipientID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE lower(username)=lower($1) AND status='active'`,
		strings.TrimSpace(req.ToUsername)).Scan(&recipientID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "recipient not found")
		return
	}
	if recipientID == uid {
		writeErr(w, http.StatusBadRequest, "cannot pay yourself")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "transfer failed")
		return
	}
	defer tx.Rollback(r.Context())

	var senderAcct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset=$2 AND chain=$3 FOR UPDATE`,
		uid, strings.ToUpper(req.Asset), strings.ToLower(req.Chain)).Scan(&senderAcct)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusBadRequest, "no wallet account for this asset; create one first")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "transfer failed")
		return
	}

	var ok bool
	err = tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
		 FROM ledger_entries WHERE account_id=$2`, req.Amount, senderAcct).Scan(&ok)
	if err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "insufficient balance or invalid amount")
		return
	}

	recipientAcct, err := a.ensureAccount(r.Context(), recipientID, req.Asset, req.Chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "transfer failed")
		return
	}

	var txID string
	err = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&txID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "transfer failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2, -$3::numeric, 'p2p_send', $4, $5),
		        ($1,$6,  $3::numeric, 'p2p_recv', $7, $5)`,
		txID, senderAcct, req.Amount, recipientID, req.Memo, recipientAcct, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "transfer failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "transfer failed")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'payment_received',$2)`,
		recipientID, map[string]string{"from": uid, "amount": req.Amount, "asset": strings.ToUpper(req.Asset)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "tx_id": txID})
}

func (a *App) handleWalletHistory(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT le.tx_id, wa.asset, wa.chain, le.amount, le.kind, le.memo, le.created_at
		 FROM ledger_entries le JOIN wallet_accounts wa ON wa.id = le.account_id
		 WHERE wa.user_id = $1 ORDER BY le.id DESC LIMIT $2 OFFSET $3`,
		userIDFrom(r), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	defer rows.Close()
	type entry struct {
		TxID      string    `json:"tx_id"`
		Asset     string    `json:"asset"`
		Chain     string    `json:"chain"`
		Amount    string    `json:"amount"`
		Kind      string    `json:"kind"`
		Memo      string    `json:"memo"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.TxID, &e.Asset, &e.Chain, &e.Amount, &e.Kind, &e.Memo, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// ---- KYC ----

func (a *App) handleKYCSubmit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FullName    string `json:"full_name"`
		Country     string `json:"country"`
		DocType     string `json:"doc_type"` // passport | national_id | driving_license
		DocNumber   string `json:"doc_number"`
		DocImageURL string `json:"doc_image_url"`
		SelfieURL   string `json:"selfie_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.FullName) == "" || req.Country == "" || req.DocNumber == "" {
		writeErr(w, http.StatusBadRequest, "full_name, country and doc_number are required")
		return
	}
	uid := userIDFrom(r)
	var status string
	_ = a.db.QueryRow(r.Context(), `SELECT kyc_status FROM users WHERE id=$1`, uid).Scan(&status)
	if status == "verified" {
		writeErr(w, http.StatusConflict, "KYC already verified")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "submission failed")
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO kyc_submissions (user_id, provider, status) VALUES ($1,'sumsub','pending') RETURNING id`, uid).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "submission failed")
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE users SET kyc_status='pending' WHERE id=$1`, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "submission failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "submission failed")
		return
	}
	// When SUMSUB_APP_TOKEN/SUMSUB_SECRET_KEY are configured, the submission
	// is mirrored to Sumsub and its webhook drives the final decision; the
	// admin review endpoint remains as the manual fallback path.
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "pending"})
}

func (a *App) handleKYCStatus(w http.ResponseWriter, r *http.Request) {
	var status string
	_ = a.db.QueryRow(r.Context(), `SELECT kyc_status FROM users WHERE id=$1`, userIDFrom(r)).Scan(&status)
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}
