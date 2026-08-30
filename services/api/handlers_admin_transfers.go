package main

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// Admin oversight of user-to-user transfers: every p2p_send/p2p_recv ledger
// pair is listed with both parties, and mistaken or fraudulent transfers can
// be reversed. Reversal is a compensating double-entry pair on the same tx
// lineage — it never deletes history, it requires the recipient to still hold
// the funds, and it is audit-logged.

type transferJSON struct {
	TxID      string    `json:"tx_id"`
	From      string    `json:"from_username"`
	To        string    `json:"to_username"`
	Asset     string    `json:"asset"`
	Chain     string    `json:"chain"`
	Amount    string    `json:"amount"`
	Memo      string    `json:"memo"`
	Reversed  bool      `json:"reversed"`
	CreatedAt time.Time `json:"created_at"`
}

// GET /api/admin/transfers?asset=&q= — recent transfers, newest first.
func (a *App) handleAdminListTransfers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT s.tx_id, uf.username, ut.username, wa.asset, wa.chain,
		        (-s.amount)::text, COALESCE(s.memo,''),
		        EXISTS(SELECT 1 FROM ledger_entries r
		               WHERE r.kind='transfer_reversal' AND r.memo LIKE '%'||s.tx_id::text||'%'),
		        s.created_at
		 FROM ledger_entries s
		 JOIN wallet_accounts wa ON wa.id = s.account_id
		 JOIN users uf ON uf.id = wa.user_id
		 JOIN ledger_entries c ON c.tx_id = s.tx_id AND c.kind = 'p2p_recv'
		 JOIN wallet_accounts wc ON wc.id = c.account_id
		 JOIN users ut ON ut.id = wc.user_id
		 WHERE s.kind = 'p2p_send'
		 ORDER BY s.created_at DESC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load transfers")
		return
	}
	defer rows.Close()
	out := []transferJSON{}
	for rows.Next() {
		var t transferJSON
		if err := rows.Scan(&t.TxID, &t.From, &t.To, &t.Asset, &t.Chain,
			&t.Amount, &t.Memo, &t.Reversed, &t.CreatedAt); err == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": out})
}

// POST /api/admin/transfers/{txId}/reverse — compensating entries move the
// funds back to the sender. Only possible while the recipient's balance still
// covers the amount; already-reversed transfers are rejected.
func (a *App) handleAdminReverseTransfer(w http.ResponseWriter, r *http.Request) {
	txID := r.PathValue("txId")
	adminID := userIDFrom(r)

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reversal failed")
		return
	}
	defer tx.Rollback(r.Context())

	var senderAcct, recipAcct, amount, asset, chain, recipID, senderID string
	err = tx.QueryRow(r.Context(),
		`SELECT s.account_id, c.account_id, (-s.amount)::text, wa.asset, wa.chain,
		        wc.user_id, wa.user_id
		 FROM ledger_entries s
		 JOIN wallet_accounts wa ON wa.id = s.account_id
		 JOIN ledger_entries c ON c.tx_id = s.tx_id AND c.kind = 'p2p_recv'
		 JOIN wallet_accounts wc ON wc.id = c.account_id
		 WHERE s.kind = 'p2p_send' AND s.tx_id = $1::uuid`, txID).
		Scan(&senderAcct, &recipAcct, &amount, &asset, &chain, &recipID, &senderID)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusNotFound, "transfer not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "reversal failed")
		return
	}
	var already bool
	_ = tx.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM ledger_entries
		 WHERE kind='transfer_reversal' AND memo LIKE '%'||$1::text||'%')`, txID).Scan(&already)
	if already {
		writeErr(w, http.StatusConflict, "transfer already reversed")
		return
	}
	// Lock the recipient account and check the funds are still there.
	var locked string
	if err := tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE id=$1::uuid FOR UPDATE`, recipAcct).Scan(&locked); err != nil {
		writeErr(w, http.StatusInternalServerError, "reversal failed")
		return
	}
	var enough bool
	_ = tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric FROM ledger_entries WHERE account_id=$2`,
		amount, recipAcct).Scan(&enough)
	if !enough {
		writeErr(w, http.StatusConflict, "recipient no longer holds the funds; reversal not possible")
		return
	}
	var revTx string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&revTx)
	memo := "reversal of " + txID
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2, -$3::numeric, 'transfer_reversal', $4, $6),
		        ($1,$5,  $3::numeric, 'transfer_reversal', $4, $6)`,
		revTx, recipAcct, amount, senderID, senderAcct, memo); err != nil {
		writeErr(w, http.StatusInternalServerError, "reversal failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "reversal failed")
		return
	}
	a.audit(r.Context(), adminID, "transfer_reverse", txID, map[string]any{"amount": amount, "asset": asset})
	a.notifyUser(r.Context(), senderID, "transfer_reversed", map[string]string{"tx_id": txID, "amount": amount, "asset": asset})
	a.notifyUser(r.Context(), recipID, "transfer_reversed", map[string]string{"tx_id": txID, "amount": amount, "asset": asset})
	writeJSON(w, http.StatusOK, map[string]string{"status": "reversed"})
}
