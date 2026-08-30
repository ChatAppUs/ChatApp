package main

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ---- Dynamic admin role definitions (superadmin only) ----

var roleNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,32}$`)

type roleDefJSON struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	BuiltIn     bool      `json:"built_in"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a *App) handleAdminListRoleDefs(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT name, description, permissions, built_in, created_at
		 FROM admin_role_defs ORDER BY built_in DESC, name`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load roles")
		return
	}
	defer rows.Close()
	out := []roleDefJSON{}
	for rows.Next() {
		var d roleDefJSON
		if err := rows.Scan(&d.Name, &d.Description, &d.Permissions, &d.BuiltIn, &d.CreatedAt); err == nil {
			out = append(out, d)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": out})
}

func (a *App) handleAdminCreateRoleDef(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	if !roleNameRe.MatchString(req.Name) {
		writeErr(w, http.StatusBadRequest, "role name must be 3-33 chars: a-z 0-9 _")
		return
	}
	// The '*' wildcard is reserved for the built-in superadmin role.
	for _, p := range req.Permissions {
		if p == "*" {
			writeErr(w, http.StatusBadRequest, "wildcard permission is reserved for superadmin")
			return
		}
	}
	if len(req.Permissions) == 0 || len(req.Permissions) > 50 || len(req.Description) > 300 {
		writeErr(w, http.StatusBadRequest, "1-50 permissions and a short description required")
		return
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO admin_role_defs (name, description, permissions, created_by)
		 VALUES ($1,$2,$3,$4)`,
		req.Name, strings.TrimSpace(req.Description), req.Permissions, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusConflict, "role already exists")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "role_def_create", req.Name, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": req.Name})
}

func (a *App) handleAdminDeleteRoleDef(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(),
		`DELETE FROM admin_role_defs WHERE name=$1 AND NOT built_in`, name)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, "role not found or built-in")
		return
	}
	// Deleting a role strips it from every admin who held it.
	_, _ = tx.Exec(r.Context(), `DELETE FROM admin_roles WHERE role=$1`, name)
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "role_def_delete", name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- Withdrawal review (signing authority) ----
//
// The review endpoint can only APPROVE (sign) or REJECT (refund) a pending
// request. It cannot create withdrawals, change amounts, or redirect the
// destination address — no admin path exists that moves user funds anywhere
// except the address the user themselves requested.

func (a *App) handleAdminListWithdrawals(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	query := `SELECT w.id, u.username, w.asset, w.chain, w.to_address, w.amount::text,
	                 w.fee::text, w.status, w.risk_score, w.risk_flags, w.auto_approved,
	                 COALESCE(w.approved_by,''), COALESCE(w.tx_hash,''), w.created_at
	          FROM withdrawal_requests w JOIN users u ON u.id = w.user_id`
	args := []any{}
	switch status {
	case "pending_review", "approved", "signed", "completed", "rejected", "failed":
		query += ` WHERE w.status=$1`
		args = append(args, status)
	}
	query += ` ORDER BY w.created_at DESC LIMIT 200`
	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load withdrawals")
		return
	}
	defer rows.Close()
	type item struct {
		ID           string    `json:"id"`
		Username     string    `json:"username"`
		Asset        string    `json:"asset"`
		Chain        string    `json:"chain"`
		ToAddress    string    `json:"to_address"`
		Amount       string    `json:"amount"`
		Fee          string    `json:"fee"`
		Status       string    `json:"status"`
		RiskScore    int       `json:"risk_score"`
		RiskFlags    []string  `json:"risk_flags"`
		AutoApproved bool      `json:"auto_approved"`
		ApprovedBy   string    `json:"approved_by"`
		TxHash       string    `json:"tx_hash"`
		CreatedAt    time.Time `json:"created_at"`
	}
	out := []item{}
	for rows.Next() {
		var x item
		if err := rows.Scan(&x.ID, &x.Username, &x.Asset, &x.Chain, &x.ToAddress,
			&x.Amount, &x.Fee, &x.Status, &x.RiskScore, &x.RiskFlags,
			&x.AutoApproved, &x.ApprovedBy, &x.TxHash, &x.CreatedAt); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"withdrawals": out})
}

func (a *App) handleAdminReviewWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"` // approve | reject
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Decision != "approve" && req.Decision != "reject" {
		writeErr(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	id := r.PathValue("id")
	adminID := userIDFrom(r)

	var userID, asset, chain, toAddr, amount, fee string
	err := a.db.QueryRow(r.Context(),
		`SELECT user_id, asset, chain, to_address, amount::text, fee::text
		 FROM withdrawal_requests WHERE id=$1 AND status='pending_review'`, id).
		Scan(&userID, &asset, &chain, &toAddr, &amount, &fee)
	if err != nil {
		writeErr(w, http.StatusNotFound, "withdrawal not found or already decided")
		return
	}

	if req.Decision == "reject" {
		if _, err := a.db.Exec(r.Context(),
			`UPDATE withdrawal_requests SET status='rejected', approved_by=$2, approved_at=now(), updated_at=now()
			 WHERE id=$1 AND status='pending_review'`, id, adminID); err != nil {
			writeErr(w, http.StatusInternalServerError, "review failed")
			return
		}
		a.refundWithdrawal(r.Context(), id)
		a.audit(r.Context(), adminID, "withdrawal_reject", id, nil)
		a.notifyUser(r.Context(), userID, "withdrawal_rejected", map[string]string{"id": id})
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
		return
	}

	// Approve: sign with the platform signing key (the superadmin authority)
	// and execute. The signature binds the user-supplied destination.
	sig := a.signWithdrawal(id, userID, asset, chain, toAddr, amount, fee)
	tag, err := a.db.Exec(r.Context(),
		`UPDATE withdrawal_requests
		 SET status='approved', approved_by=$2, approved_at=now(), signature=$3, signed_at=now(), updated_at=now()
		 WHERE id=$1 AND status='pending_review'`, id, adminID, sig)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, "withdrawal already decided")
		return
	}
	status := a.executeWithdrawal(r.Context(), id)
	a.audit(r.Context(), adminID, "withdrawal_approve", id, nil)
	a.notifyUser(r.Context(), userID, "withdrawal_"+status, map[string]string{"id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// ---- Convert rates management ----

func (a *App) handleAdminSetConvertRate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Asset   string `json:"asset"`
		Chain   string `json:"chain"`
		USDRate string `json:"usd_rate"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(req.Asset))
	chain := strings.ToLower(strings.TrimSpace(req.Chain))
	var known bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM platform_tokens WHERE symbol=$1 AND chain=$2)`,
		asset, chain).Scan(&known)
	if !known {
		writeErr(w, http.StatusBadRequest, "unknown token; add it to platform tokens first")
		return
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO convert_rates (asset, chain, usd_rate, updated_by, updated_at)
		 VALUES ($1,$2,$3::numeric,$4,now())
		 ON CONFLICT (asset, chain) DO UPDATE SET usd_rate=EXCLUDED.usd_rate,
		        updated_by=EXCLUDED.updated_by, updated_at=now()`,
		asset, chain, req.USDRate, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid rate")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "convert_rate_set", asset+"/"+chain, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// ---- P2P dispute resolution ----

func (a *App) handleAdminP2PDisputes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), tradeSelect+
		`WHERE t.status='disputed' ORDER BY t.updated_at ASC LIMIT 200`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load disputes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": a.scanTrades(rows)})
}

// Resolve a disputed trade: escrowed crypto goes to buyer or seller. This is
// the one admin path that moves escrowed funds, it requires the p2p.resolve
// permission, every resolution is audit-logged, and it can only pay one of
// the two trade parties — never an admin or an arbitrary address.
func (a *App) handleAdminP2PResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"` // buyer | seller
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.To != "buyer" && req.To != "seller" {
		writeErr(w, http.StatusBadRequest, "to must be buyer or seller")
		return
	}
	id := r.PathValue("id")
	adminID := userIDFrom(r)

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "resolution failed")
		return
	}
	defer tx.Rollback(r.Context())

	var buyerID, sellerID, asset, chain, amount string
	err = tx.QueryRow(r.Context(),
		`SELECT buyer_id, seller_id, asset, chain, crypto_amount::text
		 FROM p2p_trades WHERE id=$1 AND status='disputed' FOR UPDATE`, id).
		Scan(&buyerID, &sellerID, &asset, &chain, &amount)
	if err != nil {
		writeErr(w, http.StatusNotFound, "dispute not found")
		return
	}
	recipient := buyerID
	counterparty := sellerID
	newStatus := "resolved_buyer"
	if req.To == "seller" {
		recipient = sellerID
		counterparty = buyerID
		newStatus = "resolved_seller"
	}
	acctID, err := a.ensureAccount(r.Context(), recipient, asset, chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "resolution failed")
		return
	}
	var releaseTx string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&releaseTx)
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2,$3::numeric,'p2p_dispute_resolution',$4,'P2P dispute '||$5)`,
		releaseTx, acctID, amount, counterparty, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "resolution failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE p2p_trades SET status=$2, release_tx=$3, updated_at=now() WHERE id=$1`,
		id, newStatus, releaseTx); err != nil {
		writeErr(w, http.StatusInternalServerError, "resolution failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "resolution failed")
		return
	}
	a.audit(r.Context(), adminID, "p2p_dispute_"+newStatus, id, nil)
	a.notifyUser(r.Context(), buyerID, "p2p_trade_"+newStatus, map[string]string{"trade_id": id})
	a.notifyUser(r.Context(), sellerID, "p2p_trade_"+newStatus, map[string]string{"trade_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}
