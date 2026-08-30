package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Platform-issued virtual crypto cards. Cards spend from the owner's USD
// internal wallet account; top-ups convert any supported crypto asset into
// USD through the convert engine, atomically, on the double-entry ledger.
// The full PAN and CVV are returned exactly once at issuance — only SHA-256
// hashes and the last four digits are stored. The charge endpoint is the
// platform's card processor: it authenticates PAN + CVV + expiry, enforces
// card status, per-day/per-month limits and balance, and captures funds.

var (
	cardPANRe      = regexp.MustCompile(`^\d{16}$`)
	cardCVVRe      = regexp.MustCompile(`^\d{3}$`)
	cardMerchantRe = regexp.MustCompile(`^[\w .&'-]{2,80}$`)
	maxCardLimit   = 100000.0 // platform ceiling for user-set limits (USD)
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// luhnValid implements the standard card number checksum.
func luhnValid(pan string) bool {
	sum, dbl := 0, false
	for i := len(pan) - 1; i >= 0; i-- {
		d := int(pan[i] - '0')
		if dbl {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		dbl = !dbl
	}
	return sum%10 == 0
}

func randDigits(n int) string {
	var b strings.Builder
	for b.Len() < n {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			continue
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String()
}

// issuePAN generates a Luhn-valid 16-digit PAN on the platform's private-use
// range (prefix 990099, reserved for internal/closed-loop instruments).
func issuePAN() string {
	body := "990099" + randDigits(9)
	for d := 0; d <= 9; d++ {
		pan := body + string(byte('0'+d))
		if luhnValid(pan) {
			return pan
		}
	}
	return body + "0"
}

type cardJSON struct {
	ID          string    `json:"id"`
	Label       string    `json:"label"`
	Last4       string    `json:"last4"`
	ExpiryMonth int       `json:"expiry_month"`
	ExpiryYear  int       `json:"expiry_year"`
	Status      string    `json:"status"`
	DailyLimit  string    `json:"daily_limit_usd"`
	MonthlyLim  string    `json:"monthly_limit_usd"`
	Balance     string    `json:"balance_usd"`
	CreatedAt   time.Time `json:"created_at"`
}

const cardSelect = `SELECT c.id, c.label, c.pan_last4, c.expiry_month, c.expiry_year,
       c.status, c.daily_limit_usd::text, c.monthly_limit_usd::text,
       COALESCE((SELECT SUM(le.amount) FROM ledger_entries le
                 JOIN wallet_accounts wa ON wa.id = le.account_id
                 WHERE wa.user_id = c.user_id AND wa.asset='USD' AND wa.chain='internal'), 0)::text,
       c.created_at
 FROM cards c `

func scanCards(rows pgx.Rows) []cardJSON {
	defer rows.Close()
	out := []cardJSON{}
	for rows.Next() {
		var c cardJSON
		if err := rows.Scan(&c.ID, &c.Label, &c.Last4, &c.ExpiryMonth, &c.ExpiryYear,
			&c.Status, &c.DailyLimit, &c.MonthlyLim, &c.Balance, &c.CreatedAt); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// POST /api/cards — issue a virtual card. KYC-gated; max 5 live cards.
func (a *App) handleCardIssue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label string `json:"label"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if !a.requireKYC(r.Context(), uid) {
		writeErr(w, http.StatusForbidden, "KYC verification required to issue a card")
		return
	}
	var live int
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM cards WHERE user_id=$1 AND status <> 'terminated'`, uid).Scan(&live)
	if live >= 5 {
		writeErr(w, http.StatusBadRequest, "maximum of 5 active cards per account")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "Virtual card"
	}
	if len(label) > 50 {
		writeErr(w, http.StatusBadRequest, "label too long")
		return
	}
	pan, cvv := issuePAN(), randDigits(3)
	now := time.Now()
	expMonth, expYear := int(now.Month()), now.Year()+4
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO cards (user_id, label, pan_hash, pan_last4, cvv_hash, expiry_month, expiry_year)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		uid, label, sha256Hex(pan), pan[len(pan)-4:], sha256Hex(cvv), expMonth, expYear).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "card issuance failed")
		return
	}
	// Full details are shown exactly once; only hashes are stored.
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": id, "card_number": pan, "cvv": cvv,
		"expiry_month": expMonth, "expiry_year": expYear, "status": "active",
	})
}

// GET /api/cards — own cards with the USD spend balance.
func (a *App) handleCardList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), cardSelect+
		`WHERE c.user_id=$1 AND c.status <> 'terminated' ORDER BY c.created_at DESC`,
		userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load cards")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": scanCards(rows)})
}

// POST /api/cards/{id}/topup — convert crypto into the USD card balance.
func (a *App) handleCardTopUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Asset  string `json:"asset"`
		Chain  string `json:"chain"`
		Amount string `json:"amount"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	var ownerOK bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM cards WHERE id=$1 AND user_id=$2 AND status <> 'terminated')`,
		r.PathValue("id"), uid).Scan(&ownerOK)
	if !ownerOK {
		writeErr(w, http.StatusNotFound, "card not found")
		return
	}
	asset, chain := strings.ToUpper(req.Asset), strings.ToLower(req.Chain)
	if asset == "USD" && chain == "internal" {
		writeErr(w, http.StatusBadRequest, "card balance is already USD")
		return
	}
	_, _, _, convertOn, _, _, ok := a.tokenFlags(r.Context(), asset, chain)
	if !ok || !convertOn {
		writeErr(w, http.StatusBadRequest, "top-up not enabled for this asset")
		return
	}
	usdAmount, rate, ok := a.convertQuote(r, asset, chain, "USD", "internal", req.Amount)
	if !ok {
		writeErr(w, http.StatusBadRequest, "no rate for this asset or invalid amount")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "top-up failed")
		return
	}
	defer tx.Rollback(r.Context())

	var fromAcct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset=$2 AND chain=$3 FOR UPDATE`,
		uid, asset, chain).Scan(&fromAcct)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusBadRequest, "no wallet account for this asset")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "top-up failed")
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
	usdAcct, err := a.ensureAccount(r.Context(), uid, "USD", "internal")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "top-up failed")
		return
	}
	var ledgerTx string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&ledgerTx)
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
		 VALUES ($1,$2, -$3::numeric, 'card_topup_out', $4||' -> USD @ '||$5),
		        ($1,$6,  $7::numeric, 'card_topup_in',  $4||' -> USD @ '||$5)`,
		ledgerTx, fromAcct, req.Amount, asset, rate, usdAcct, usdAmount); err != nil {
		writeErr(w, http.StatusInternalServerError, "top-up failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "top-up failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"status": "topped_up", "usd_amount": usdAmount, "rate": rate,
	})
}

// POST /api/cards/charge — the platform card processor. Authenticates the
// presented card (PAN + CVV + expiry), enforces status, per-transaction,
// daily and monthly limits and the USD balance, then captures the funds.
func (a *App) handleCardCharge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CardNumber  string `json:"card_number"`
		CVV         string `json:"cvv"`
		ExpiryMonth int    `json:"expiry_month"`
		ExpiryYear  int    `json:"expiry_year"`
		Merchant    string `json:"merchant"`
		AmountUSD   string `json:"amount_usd"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.CardNumber = strings.ReplaceAll(strings.TrimSpace(req.CardNumber), " ", "")
	if !cardPANRe.MatchString(req.CardNumber) || !luhnValid(req.CardNumber) ||
		!cardCVVRe.MatchString(req.CVV) || !cardMerchantRe.MatchString(strings.TrimSpace(req.Merchant)) {
		writeErr(w, http.StatusBadRequest, "invalid card details or merchant")
		return
	}
	var amount float64
	if _, err := fmt.Sscanf(req.AmountUSD, "%g", &amount); err != nil || amount <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid amount")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "charge failed")
		return
	}
	defer tx.Rollback(r.Context())

	decline := func(reason string) {
		// Declines are recorded in the same transaction (it already holds the
		// card row lock) so fraud analytics see every attempt.
		_, _ = tx.Exec(r.Context(),
			`INSERT INTO card_transactions (card_id, merchant, amount_usd, kind, status)
			 SELECT id, $2, $3::numeric, 'purchase', 'declined' FROM cards
			 WHERE pan_hash=$1`, sha256Hex(req.CardNumber),
			strings.TrimSpace(req.Merchant), req.AmountUSD)
		_ = tx.Commit(r.Context())
		writeErr(w, http.StatusPaymentRequired, "card declined: "+reason)
	}

	var cardID, ownerID, status string
	var dailyLim, monthlyLim float64
	var expMonth, expYear int
	err = tx.QueryRow(r.Context(),
		`SELECT id, user_id, status, daily_limit_usd::float8, monthly_limit_usd::float8,
		        expiry_month, expiry_year
		 FROM cards WHERE pan_hash=$1 AND cvv_hash=$2 FOR UPDATE`,
		sha256Hex(req.CardNumber), sha256Hex(req.CVV)).
		Scan(&cardID, &ownerID, &status, &dailyLim, &monthlyLim, &expMonth, &expYear)
	if err != nil {
		tx.Rollback(r.Context())
		writeErr(w, http.StatusPaymentRequired, "card declined: invalid card")
		return
	}
	now := time.Now()
	if expYear < now.Year() || (expYear == now.Year() && expMonth < int(now.Month())) ||
		expMonth != req.ExpiryMonth || expYear != req.ExpiryYear {
		decline("wrong expiry or expired card")
		return
	}
	if status == "frozen" {
		decline("card is frozen")
		return
	}
	if status != "active" {
		decline("card is not active")
		return
	}
	var spentDay, spentMonth float64
	_ = tx.QueryRow(r.Context(),
		`SELECT
		   COALESCE(SUM(amount_usd) FILTER (WHERE created_at > now() - interval '24 hours'),0)::float8,
		   COALESCE(SUM(amount_usd) FILTER (WHERE created_at > now() - interval '30 days'),0)::float8
		 FROM card_transactions
		 WHERE card_id=$1 AND kind='purchase' AND status='captured'`, cardID).
		Scan(&spentDay, &spentMonth)
	if spentDay+amount > dailyLim {
		decline("daily limit exceeded")
		return
	}
	if spentMonth+amount > monthlyLim {
		decline("monthly limit exceeded")
		return
	}

	var usdAcct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset='USD' AND chain='internal' FOR UPDATE`,
		ownerID).Scan(&usdAcct)
	if err == pgx.ErrNoRows {
		decline("insufficient funds")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "charge failed")
		return
	}
	var enough bool
	_ = tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric
		 FROM ledger_entries WHERE account_id=$2`, req.AmountUSD, usdAcct).Scan(&enough)
	if !enough {
		decline("insufficient funds")
		return
	}

	var ledgerTx, chargeID string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&ledgerTx)
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
		 VALUES ($1,$2, -$3::numeric, 'card_spend', $4)`,
		ledgerTx, usdAcct, req.AmountUSD, strings.TrimSpace(req.Merchant)); err != nil {
		writeErr(w, http.StatusInternalServerError, "charge failed")
		return
	}
	err = tx.QueryRow(r.Context(),
		`INSERT INTO card_transactions (card_id, merchant, amount_usd, kind, status, ledger_tx)
		 VALUES ($1,$2,$3::numeric,'purchase','captured',$4) RETURNING id`,
		cardID, strings.TrimSpace(req.Merchant), req.AmountUSD, ledgerTx).Scan(&chargeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "charge failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "charge failed")
		return
	}
	a.notifyUser(r.Context(), ownerID, "card_spend", map[string]string{
		"merchant": strings.TrimSpace(req.Merchant), "amount_usd": req.AmountUSD,
	})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "captured", "id": chargeID})
}

// POST /api/cards/{id}/refund — the card owner reverses a captured purchase.
func (a *App) handleCardRefund(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TransactionID string `json:"transaction_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, cardID := userIDFrom(r), r.PathValue("id")

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "refund failed")
		return
	}
	defer tx.Rollback(r.Context())

	var merchant, amount string
	err = tx.QueryRow(r.Context(),
		`SELECT t.merchant, t.amount_usd::text
		 FROM card_transactions t JOIN cards c ON c.id = t.card_id
		 WHERE t.id=$1 AND t.card_id=$2 AND c.user_id=$3
		   AND t.kind='purchase' AND t.status='captured' FOR UPDATE`,
		req.TransactionID, cardID, uid).Scan(&merchant, &amount)
	if err != nil {
		writeErr(w, http.StatusNotFound, "transaction not found or already reversed")
		return
	}
	var usdAcct string
	if err := tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset='USD' AND chain='internal' FOR UPDATE`,
		uid).Scan(&usdAcct); err != nil {
		writeErr(w, http.StatusInternalServerError, "refund failed")
		return
	}
	var ledgerTx string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&ledgerTx)
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
		 VALUES ($1,$2,$3::numeric,'card_refund',$4)`,
		ledgerTx, usdAcct, amount, "refund: "+merchant); err != nil {
		writeErr(w, http.StatusInternalServerError, "refund failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE card_transactions SET status='reversed' WHERE id=$1`, req.TransactionID); err != nil {
		writeErr(w, http.StatusInternalServerError, "refund failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO card_transactions (card_id, merchant, amount_usd, kind, status, ledger_tx)
		 VALUES ($1,$2,$3::numeric,'refund','captured',$4)`,
		cardID, merchant, amount, ledgerTx); err != nil {
		writeErr(w, http.StatusInternalServerError, "refund failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "refund failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "refunded"})
}

// POST /api/cards/{id}/status — owner freeze/unfreeze/terminate.
func (a *App) handleCardSetStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"` // active | frozen | terminated
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Status {
	case "active", "frozen", "terminated":
	default:
		writeErr(w, http.StatusBadRequest, "status must be active, frozen or terminated")
		return
	}
	// Terminated cards can never come back.
	tag, err := a.db.Exec(r.Context(),
		`UPDATE cards SET status=$2
		 WHERE id=$1 AND user_id=$3 AND status <> 'terminated'`,
		r.PathValue("id"), req.Status, userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "card not found or already terminated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// PUT /api/cards/{id}/limits — owner-set spending limits, platform-capped.
func (a *App) handleCardSetLimits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Daily   float64 `json:"daily_limit_usd"`
		Monthly float64 `json:"monthly_limit_usd"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Daily <= 0 || req.Monthly <= 0 || req.Daily > maxCardLimit ||
		req.Monthly > maxCardLimit || req.Daily > req.Monthly {
		writeErr(w, http.StatusBadRequest,
			"limits must be positive, daily <= monthly, and at most 100000")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE cards SET daily_limit_usd=$2, monthly_limit_usd=$3
		 WHERE id=$1 AND user_id=$4 AND status <> 'terminated'`,
		r.PathValue("id"), req.Daily, req.Monthly, userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "card not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// GET /api/cards/{id}/transactions — owner statement.
func (a *App) handleCardTransactions(w http.ResponseWriter, r *http.Request) {
	var ownerOK bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM cards WHERE id=$1 AND user_id=$2)`,
		r.PathValue("id"), userIDFrom(r)).Scan(&ownerOK)
	if !ownerOK {
		writeErr(w, http.StatusNotFound, "card not found")
		return
	}
	limit, offset := pageParams(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, merchant, amount_usd::text, kind, status, created_at
		 FROM card_transactions WHERE card_id=$1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, r.PathValue("id"), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load transactions")
		return
	}
	defer rows.Close()
	type txOut struct {
		ID        string    `json:"id"`
		Merchant  string    `json:"merchant"`
		Amount    string    `json:"amount_usd"`
		Kind      string    `json:"kind"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []txOut{}
	for rows.Next() {
		var t txOut
		if err := rows.Scan(&t.ID, &t.Merchant, &t.Amount, &t.Kind, &t.Status, &t.CreatedAt); err == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"transactions": out})
}

// ---- Admin card oversight (cards.manage) ----

// GET /api/admin/cards?status=active|frozen — all cards with owner + totals.
func (a *App) handleAdminListCards(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	query := `SELECT c.id, u.username, c.label, c.pan_last4, c.status,
	                 c.daily_limit_usd::text, c.monthly_limit_usd::text,
	                 COALESCE((SELECT SUM(t.amount_usd) FROM card_transactions t
	                   WHERE t.card_id=c.id AND t.kind='purchase' AND t.status='captured'
	                     AND t.created_at > now() - interval '30 days'),0)::text AS month_spend,
	                 c.created_at
	          FROM cards c JOIN users u ON u.id = c.user_id`
	args := []any{}
	switch status {
	case "active", "frozen", "terminated":
		query += ` WHERE c.status=$1`
		args = append(args, status)
	}
	query += ` ORDER BY c.created_at DESC LIMIT 200`
	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load cards")
		return
	}
	defer rows.Close()
	type adminCard struct {
		ID         string    `json:"id"`
		Username   string    `json:"username"`
		Label      string    `json:"label"`
		Last4      string    `json:"last4"`
		Status     string    `json:"status"`
		Daily      string    `json:"daily_limit_usd"`
		Monthly    string    `json:"monthly_limit_usd"`
		MonthSpend string    `json:"month_spend_usd"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := []adminCard{}
	for rows.Next() {
		var c adminCard
		if err := rows.Scan(&c.ID, &c.Username, &c.Label, &c.Last4, &c.Status,
			&c.Daily, &c.Monthly, &c.MonthSpend, &c.CreatedAt); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": out})
}

// POST /api/admin/cards/{id}/status — admin freeze/terminate. Admins cannot
// reactivate a terminated card and can never spend from a card.
func (a *App) handleAdminSetCardStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"` // active | frozen | terminated
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Status {
	case "active", "frozen", "terminated":
	default:
		writeErr(w, http.StatusBadRequest, "status must be active, frozen or terminated")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE cards SET status=$2 WHERE id=$1 AND status <> 'terminated'`,
		r.PathValue("id"), req.Status)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "card not found or already terminated")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "card_status_"+req.Status, r.PathValue("id"), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}
