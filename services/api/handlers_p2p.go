package main

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// P2P marketplace: buy/sell offers settled against local payment rails, with
// crypto locked in escrow on the double-entry ledger from trade open until
// release/cancel/dispute-resolution.

var fiatRe = regexp.MustCompile(`^[A-Z]{3}$`)
var isoRe = regexp.MustCompile(`^[A-Z]{2}$`)

func (a *App) requireKYC(ctx context.Context, userID string) bool {
	var kyc string
	if err := a.db.QueryRow(ctx, `SELECT kyc_status FROM users WHERE id=$1`, userID).Scan(&kyc); err != nil {
		return false
	}
	return kyc == "verified"
}

func (a *App) handleP2PPaymentMethods(w http.ResponseWriter, r *http.Request) {
	country := strings.ToUpper(r.URL.Query().Get("country"))
	query := `SELECT country_iso, name, kind FROM p2p_payment_methods`
	args := []any{}
	if isoRe.MatchString(country) {
		query += ` WHERE country_iso=$1`
		args = append(args, country)
	}
	query += ` ORDER BY country_iso, kind, name`
	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load payment methods")
		return
	}
	defer rows.Close()
	type pm struct {
		Country string `json:"country_iso"`
		Name    string `json:"name"`
		Kind    string `json:"kind"`
	}
	out := []pm{}
	for rows.Next() {
		var m pm
		if err := rows.Scan(&m.Country, &m.Name, &m.Kind); err == nil {
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"methods": out})
}

type p2pOfferJSON struct {
	ID             string    `json:"id"`
	Owner          string    `json:"owner_username"`
	Side           string    `json:"side"`
	Asset          string    `json:"asset"`
	Chain          string    `json:"chain"`
	Fiat           string    `json:"fiat_currency"`
	Country        string    `json:"country_iso"`
	Price          string    `json:"price"`
	MinAmount      string    `json:"min_amount"`
	MaxAmount      string    `json:"max_amount"`
	PaymentMethods []string  `json:"payment_methods"`
	Terms          string    `json:"terms"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
}

func (a *App) scanOffers(rows pgx.Rows) []p2pOfferJSON {
	defer rows.Close()
	out := []p2pOfferJSON{}
	for rows.Next() {
		var o p2pOfferJSON
		if err := rows.Scan(&o.ID, &o.Owner, &o.Side, &o.Asset, &o.Chain, &o.Fiat,
			&o.Country, &o.Price, &o.MinAmount, &o.MaxAmount, &o.PaymentMethods,
			&o.Terms, &o.Active, &o.CreatedAt); err == nil {
			out = append(out, o)
		}
	}
	return out
}

func (a *App) handleP2PListOffers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := []string{"o.active"}
	args := []any{}
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if v := q.Get("asset"); v != "" {
		add(`o.asset = upper($%d)`, v)
	}
	if v := q.Get("chain"); v != "" {
		add(`o.chain = lower($%d)`, v)
	}
	if v := q.Get("side"); v == "buy" || v == "sell" {
		add(`o.side = $%d`, v)
	}
	if v := q.Get("country"); isoRe.MatchString(strings.ToUpper(v)) {
		add(`o.country_iso = $%d`, strings.ToUpper(v))
	}
	if v := q.Get("fiat"); fiatRe.MatchString(strings.ToUpper(v)) {
		add(`o.fiat_currency = $%d`, strings.ToUpper(v))
	}
	limit, offset := pageParams(r)
	args = append(args, limit, offset)
	rows, err := a.db.Query(r.Context(),
		fmt.Sprintf(`SELECT o.id, u.username, o.side, o.asset, o.chain, o.fiat_currency, o.country_iso,
		        o.price::text, o.min_amount::text, o.max_amount::text, o.payment_methods,
		        o.terms, o.active, o.created_at
		 FROM p2p_offers o JOIN users u ON u.id = o.owner_id
		 WHERE %s
		 ORDER BY o.price ASC, o.created_at DESC LIMIT $%d OFFSET $%d`,
			strings.Join(where, " AND "), len(args)-1, len(args)), args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load offers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"offers": a.scanOffers(rows)})
}

func (a *App) handleP2PCreateOffer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Side           string   `json:"side"`
		Asset          string   `json:"asset"`
		Chain          string   `json:"chain"`
		Fiat           string   `json:"fiat_currency"`
		Country        string   `json:"country_iso"`
		Price          string   `json:"price"`
		MinAmount      string   `json:"min_amount"`
		MaxAmount      string   `json:"max_amount"`
		PaymentMethods []string `json:"payment_methods"`
		Terms          string   `json:"terms"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if !a.requireKYC(r.Context(), uid) {
		writeErr(w, http.StatusForbidden, "KYC verification required for P2P trading")
		return
	}
	_, _, p2pOn, _, _, _, ok := a.tokenFlags(r.Context(), req.Asset, req.Chain)
	if !ok || !p2pOn {
		writeErr(w, http.StatusBadRequest, "P2P trading not enabled for this asset/chain")
		return
	}
	if req.Side != "buy" && req.Side != "sell" {
		writeErr(w, http.StatusBadRequest, "side must be buy or sell")
		return
	}
	req.Fiat = strings.ToUpper(strings.TrimSpace(req.Fiat))
	req.Country = strings.ToUpper(strings.TrimSpace(req.Country))
	if !fiatRe.MatchString(req.Fiat) || !isoRe.MatchString(req.Country) {
		writeErr(w, http.StatusBadRequest, "valid fiat currency and country required")
		return
	}
	if len(req.PaymentMethods) == 0 || len(req.PaymentMethods) > 10 {
		writeErr(w, http.StatusBadRequest, "choose 1-10 payment methods")
		return
	}
	// Payment methods must be real rails of the chosen country.
	var validPM bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) = $2 FROM p2p_payment_methods
		 WHERE country_iso=$1 AND name = ANY($3)`,
		req.Country, len(req.PaymentMethods), req.PaymentMethods).Scan(&validPM)
	if !validPM {
		writeErr(w, http.StatusBadRequest, "unknown payment method for this country")
		return
	}
	if len(req.Terms) > 1000 {
		writeErr(w, http.StatusBadRequest, "terms too long")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO p2p_offers (owner_id, side, asset, chain, fiat_currency, country_iso,
		                         price, min_amount, max_amount, payment_methods, terms)
		 VALUES ($1,$2,upper($3),lower($4),$5,$6,$7::numeric,$8::numeric,$9::numeric,$10,$11)
		 RETURNING id`,
		uid, req.Side, req.Asset, req.Chain, req.Fiat, req.Country,
		req.Price, req.MinAmount, req.MaxAmount, req.PaymentMethods, strings.TrimSpace(req.Terms)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid amounts or price")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleP2PMyOffers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT o.id, u.username, o.side, o.asset, o.chain, o.fiat_currency, o.country_iso,
		        o.price::text, o.min_amount::text, o.max_amount::text, o.payment_methods,
		        o.terms, o.active, o.created_at
		 FROM p2p_offers o JOIN users u ON u.id = o.owner_id
		 WHERE o.owner_id=$1 ORDER BY o.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load offers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"offers": a.scanOffers(rows)})
}

func (a *App) handleP2PSetOfferActive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Active bool `json:"active"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE p2p_offers SET active=$2 WHERE id=$1 AND owner_id=$3`,
		r.PathValue("id"), req.Active, userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "offer not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// ---- trades (escrowed) ----

func (a *App) handleP2POpenTrade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OfferID       string `json:"offer_id"`
		CryptoAmount  string `json:"crypto_amount"`
		PaymentMethod string `json:"payment_method"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if !a.requireKYC(r.Context(), uid) {
		writeErr(w, http.StatusForbidden, "KYC verification required for P2P trading")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	defer tx.Rollback(r.Context())

	var ownerID, side, asset, chain, fiat string
	var price, minAmt, maxAmt string
	err = tx.QueryRow(r.Context(),
		`SELECT owner_id, side, asset, chain, fiat_currency, price::text,
		        min_amount::text, max_amount::text
		 FROM p2p_offers WHERE id=$1 AND active FOR UPDATE`, req.OfferID).
		Scan(&ownerID, &side, &asset, &chain, &fiat, &price, &minAmt, &maxAmt)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusNotFound, "offer not found or inactive")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	if ownerID == uid {
		writeErr(w, http.StatusBadRequest, "cannot trade against your own offer")
		return
	}
	var pmOK bool
	_ = tx.QueryRow(r.Context(),
		`SELECT $2 = ANY(payment_methods) FROM p2p_offers WHERE id=$1`,
		req.OfferID, req.PaymentMethod).Scan(&pmOK)
	if !pmOK {
		writeErr(w, http.StatusBadRequest, "payment method not accepted by this offer")
		return
	}
	var inRange bool
	_ = tx.QueryRow(r.Context(),
		`SELECT $1::numeric > 0 AND $1::numeric >= $2::numeric AND $1::numeric <= $3::numeric`,
		req.CryptoAmount, minAmt, maxAmt).Scan(&inRange)
	if !inRange {
		writeErr(w, http.StatusBadRequest, "amount outside offer limits")
		return
	}

	buyerID, sellerID := uid, ownerID
	if side == "buy" { // offer owner buys; the taker sells
		buyerID, sellerID = ownerID, uid
	}

	// Escrow: lock the seller's crypto now.
	var sellerAcct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset=$2 AND chain=$3 FOR UPDATE`,
		sellerID, asset, chain).Scan(&sellerAcct)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusBadRequest, "seller has no wallet account for this asset")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	var enough bool
	_ = tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric FROM ledger_entries WHERE account_id=$2`,
		req.CryptoAmount, sellerAcct).Scan(&enough)
	if !enough {
		writeErr(w, http.StatusBadRequest, "seller balance insufficient for escrow")
		return
	}
	var escrowTx string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&escrowTx)
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2, -$3::numeric, 'p2p_escrow_lock', $4, 'P2P escrow')`,
		escrowTx, sellerAcct, req.CryptoAmount, buyerID); err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}

	var id string
	err = tx.QueryRow(r.Context(),
		`INSERT INTO p2p_trades (offer_id, buyer_id, seller_id, asset, chain,
		                         crypto_amount, fiat_amount, fiat_currency, payment_method, escrow_tx)
		 VALUES ($1,$2,$3,$4,$5,$6::numeric, ($6::numeric * $7::numeric), $8, $9, $10)
		 RETURNING id`,
		req.OfferID, buyerID, sellerID, asset, chain, req.CryptoAmount, price, fiat,
		req.PaymentMethod, escrowTx).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	a.notifyUser(r.Context(), sellerID, "p2p_trade_open", map[string]string{"trade_id": id})
	a.notifyUser(r.Context(), buyerID, "p2p_trade_open", map[string]string{"trade_id": id})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "open"})
}

type p2pTradeJSON struct {
	ID            string     `json:"id"`
	OfferID       string     `json:"offer_id"`
	Buyer         string     `json:"buyer_username"`
	Seller        string     `json:"seller_username"`
	Asset         string     `json:"asset"`
	Chain         string     `json:"chain"`
	CryptoAmount  string     `json:"crypto_amount"`
	FiatAmount    string     `json:"fiat_amount"`
	Fiat          string     `json:"fiat_currency"`
	PaymentMethod string     `json:"payment_method"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	PaidAt        *time.Time `json:"paid_at"`
}

func (a *App) scanTrades(rows pgx.Rows) []p2pTradeJSON {
	defer rows.Close()
	out := []p2pTradeJSON{}
	for rows.Next() {
		var t p2pTradeJSON
		if err := rows.Scan(&t.ID, &t.OfferID, &t.Buyer, &t.Seller, &t.Asset, &t.Chain,
			&t.CryptoAmount, &t.FiatAmount, &t.Fiat, &t.PaymentMethod, &t.Status,
			&t.CreatedAt, &t.PaidAt); err == nil {
			out = append(out, t)
		}
	}
	return out
}

const tradeSelect = `SELECT t.id, t.offer_id, ub.username, us.username, t.asset, t.chain,
        t.crypto_amount::text, t.fiat_amount::text, t.fiat_currency, t.payment_method,
        t.status, t.created_at, t.paid_at
 FROM p2p_trades t
 JOIN users ub ON ub.id = t.buyer_id
 JOIN users us ON us.id = t.seller_id `

func (a *App) handleP2PMyTrades(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(), tradeSelect+
		`WHERE t.buyer_id=$1 OR t.seller_id=$1 ORDER BY t.created_at DESC LIMIT $2 OFFSET $3`,
		userIDFrom(r), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load trades")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": a.scanTrades(rows)})
}

// tradeActor returns the trade row when the caller is a party with the given
// status, else false.
func (a *App) tradeActor(ctx context.Context, tx pgx.Tx, id, uid, wantStatus string) (t struct {
	BuyerID, SellerID, Asset, Chain, Amount string
}, ok bool) {
	var status string
	err := tx.QueryRow(ctx,
		`SELECT buyer_id, seller_id, asset, chain, crypto_amount::text, status
		 FROM p2p_trades WHERE id=$1 FOR UPDATE`, id).
		Scan(&t.BuyerID, &t.SellerID, &t.Asset, &t.Chain, &t.Amount, &status)
	if err != nil {
		return t, false
	}
	if t.BuyerID != uid && t.SellerID != uid {
		return t, false
	}
	return t, status == wantStatus
}

func (a *App) handleP2PTradePaid(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	tag, err := a.db.Exec(r.Context(),
		`UPDATE p2p_trades SET status='paid', paid_at=now(), updated_at=now()
		 WHERE id=$1 AND buyer_id=$2 AND status='open'`, r.PathValue("id"), uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, "trade not found, not yours, or wrong state")
		return
	}
	a.notifyCounterparty(r.Context(), r.PathValue("id"), uid, "p2p_trade_paid")
	writeJSON(w, http.StatusOK, map[string]string{"status": "paid"})
}

// Release: seller confirms fiat receipt; escrowed crypto moves to the buyer.
func (a *App) handleP2PTradeRelease(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	a.settleTrade(w, r, uid, "release")
}

// Cancel: buyer aborts before payment; escrow returns to the seller.
func (a *App) handleP2PTradeCancel(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	a.settleTrade(w, r, uid, "cancel")
}

func (a *App) settleTrade(w http.ResponseWriter, r *http.Request, uid, action string) {
	id := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	defer tx.Rollback(r.Context())

	t, ok := a.tradeActor(r.Context(), tx, id, uid, map[string]string{
		"release": "paid", "cancel": "open",
	}[action])
	if !ok {
		writeErr(w, http.StatusConflict, "trade not found, not yours, or wrong state")
		return
	}
	if action == "release" && uid != t.SellerID {
		writeErr(w, http.StatusForbidden, "only the seller releases escrow")
		return
	}
	if action == "cancel" && uid != t.BuyerID {
		writeErr(w, http.StatusForbidden, "only the buyer cancels an unpaid trade")
		return
	}

	var releaseTx string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&releaseTx)
	var acctOwner string
	kind := "p2p_escrow_release"
	newStatus := "completed"
	if action == "release" {
		acctOwner = t.BuyerID
	} else {
		acctOwner = t.SellerID
		kind = "p2p_escrow_refund"
		newStatus = "cancelled"
	}
	acctID, err := a.ensureAccount(r.Context(), acctOwner, t.Asset, t.Chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2, $3::numeric, $4, $5, 'P2P trade '||$6)`,
		releaseTx, acctID, t.Amount, kind, map[string]string{
			"release": t.SellerID, "cancel": t.BuyerID,
		}[action], id); err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE p2p_trades SET status=$2, release_tx=$3, updated_at=now() WHERE id=$1`,
		id, newStatus, releaseTx); err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "trade failed")
		return
	}
	a.notifyCounterparty(r.Context(), id, uid, "p2p_trade_"+newStatus)
	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}

func (a *App) handleP2PTradeDispute(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	tag, err := a.db.Exec(r.Context(),
		`UPDATE p2p_trades SET status='disputed', updated_at=now()
		 WHERE id=$1 AND (buyer_id=$2 OR seller_id=$2) AND status='paid'`,
		r.PathValue("id"), uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, "trade not found, not yours, or not disputable")
		return
	}
	a.notifyCounterparty(r.Context(), r.PathValue("id"), uid, "p2p_trade_disputed")
	writeJSON(w, http.StatusOK, map[string]string{"status": "disputed"})
}

func (a *App) notifyCounterparty(ctx context.Context, tradeID, actorID, kind string) {
	var buyer, seller string
	if err := a.db.QueryRow(ctx, `SELECT buyer_id, seller_id FROM p2p_trades WHERE id=$1`, tradeID).
		Scan(&buyer, &seller); err != nil {
		return
	}
	target := buyer
	if actorID == buyer {
		target = seller
	}
	a.notifyUser(ctx, target, kind, map[string]string{"trade_id": tradeID})
}

func (a *App) notifyUser(ctx context.Context, userID, kind string, payload map[string]string) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,$2,$3)`, userID, kind, payload)
}
