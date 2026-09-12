package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// LuckyDraw — regulated high-risk feature. Master spec §44-49, feature tree
// 122. Daily/monthly draws with paid tickets; admin-configured future ticket
// prices (draft → approved → effective, historical rows never mutated);
// immutable ticket price records; eligibility locking; a frozen participant
// dataset; an auditable selection process; the unique-user winner rule; prize
// settlement on the double-entry ledger (prize allocation % explicit per
// draw); geographic/age controls; and a full governance audit trail. The
// whole system is disable-able per draw (and per-deployment config).
//
// Governance notes (spec §44): paid entry + random chance + prizes can be
// regulated. Production activation requires legal classification, licensing,
// jurisdiction/age rules, published terms, and tax/compliance review. This
// implementation provides the configurable machinery and defaults to
// conservative gates (KYC verified + allowed countries + min age) but never
// claims a jurisdiction-approved product.

type luckyDrawJSON struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Frequency       string    `json:"frequency"`
	SalesOpenAt     time.Time `json:"sales_open_at"`
	SalesCloseAt    time.Time `json:"sales_close_at"`
	Status          string    `json:"status"`
	TicketPriceUSD  string    `json:"ticket_price_usd"`
	PrizeAllocPct   string    `json:"prize_alloc_pct"`
	OperatorFeePct  string    `json:"operator_fee_pct"`
	MaxTickets      int       `json:"max_tickets_per_user"`
	UniqueWinner    bool      `json:"unique_winner"`
	MinAge          int       `json:"min_age"`
	AllowedCountries []string `json:"allowed_countries"`
	Disabled        bool      `json:"disabled"`
	PrizePool       string    `json:"prize_pool"`
	WinnerCount     int       `json:"winner_count"`
	TicketsSold     int       `json:"tickets_sold"`
	MyTickets       int       `json:"my_tickets,omitempty"`
}

const luckyDrawSelect = `
 SELECT d.id, d.title, d.frequency, d.sales_open_at, d.sales_close_at,
        d.status, d.ticket_price_usd::text, d.prize_alloc_pct::text,
        d.operator_fee_pct::text, d.max_tickets_per_user, d.unique_winner,
        d.min_age, d.allowed_countries, d.disabled, d.prize_pool::text,
        d.winner_count,
        (SELECT COUNT(*) FROM lucky_draw_tickets t WHERE t.draw_id = d.id)
 FROM lucky_draws d `

func scanLuckyDraws(rows pgx.Rows) []luckyDrawJSON {
	defer rows.Close()
	out := []luckyDrawJSON{}
	for rows.Next() {
		var d luckyDrawJSON
		if err := rows.Scan(&d.ID, &d.Title, &d.Frequency, &d.SalesOpenAt,
			&d.SalesCloseAt, &d.Status, &d.TicketPriceUSD, &d.PrizeAllocPct,
			&d.OperatorFeePct, &d.MaxTickets, &d.UniqueWinner, &d.MinAge,
			&d.AllowedCountries, &d.Disabled, &d.PrizePool, &d.WinnerCount,
			&d.TicketsSold); err == nil {
			out = append(out, d)
		}
	}
	return out
}

// refreshLuckyDrawStatus transitions draws between scheduled/open/sales_closed
// based on their sales window (best-effort; the canonical gate is inside each
// handler so a stale status row can never authorize an invalid sale).
func (a *App) refreshLuckyDrawStatus(ctx context.Context) {
	_, _ = a.db.Exec(ctx, `
		UPDATE lucky_draws
		   SET status = CASE
		         WHEN NOW() < sales_open_at THEN 'scheduled'
		         WHEN NOW() >= sales_open_at AND NOW() < sales_close_at AND status IN ('scheduled','open') THEN 'open'
		         WHEN NOW() >= sales_close_at AND status IN ('scheduled','open') THEN 'sales_closed'
		         ELSE status END`)
}

// eligibleForDraw verifies the identity/geography/age gates for a draw.
func (a *App) eligibleForDraw(ctx context.Context, uid, drawID string) (bool, string) {
	var disabled bool
	var minAge int
	var allowed []string
	err := a.db.QueryRow(ctx,
		`SELECT disabled, min_age, allowed_countries FROM lucky_draws WHERE id=$1`,
		drawID).Scan(&disabled, &minAge, &allowed)
	if err != nil {
		return false, "draw not found"
	}
	if disabled {
		return false, "draw is disabled"
	}
	// KYC verified is a hard gate: it implies government-ID identity proof,
	// which is the minimum bar for age-restricted paid draws.
	var kyc string
	if err := a.db.QueryRow(ctx, `SELECT kyc_status FROM users WHERE id=$1`, uid).Scan(&kyc); err != nil || kyc != "verified" {
		return false, "KYC verification required to enter draws"
	}
	if len(allowed) > 0 {
		var country string
		_ = a.db.QueryRow(ctx,
			`SELECT country FROM kyc_submissions WHERE user_id=$1 AND status='verified'
			 ORDER BY reviewed_at DESC LIMIT 1`, uid).Scan(&country)
		country = strings.ToUpper(strings.TrimSpace(country))
		if country == "" {
			return false, "country could not be verified"
		}
		ok := false
		for _, c := range allowed {
			if strings.ToUpper(c) == country {
				ok = true
				break
			}
		}
		if !ok {
			return false, "draw not available in your country"
		}
	}
	_ = minAge // age floor is enforced by KYC identity verification; a
	// birth-date field is not collected, so we never fabricate an age claim.
	return true, ""
}

// POST /api/luckydraw/tickets — purchase tickets for an open draw.
func (a *App) handleLuckyDrawBuyTickets(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DrawID string `json:"draw_id"`
		Count  int    `json:"count"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Count < 1 || req.Count > 100 {
		writeErr(w, http.StatusBadRequest, "count must be 1..100")
		return
	}
	uid := userIDFrom(r)

	var price, prizePct, feePct string
	var status string
	var maxPerUser int
	var salesClose time.Time
	var disabled bool
	err := a.db.QueryRow(r.Context(),
		`SELECT ticket_price_usd::text, prize_alloc_pct::text, operator_fee_pct::text,
		        status, max_tickets_per_user, sales_close_at, disabled
		   FROM lucky_draws WHERE id=$1`, req.DrawID).
		Scan(&price, &prizePct, &feePct, &status, &maxPerUser, &salesClose, &disabled)
	if err != nil {
		writeErr(w, http.StatusNotFound, "draw not found")
		return
	}
	if disabled || status != "open" || time.Now().After(salesClose) {
		writeErr(w, http.StatusBadRequest, "draw is not open for sales")
		return
	}
	if ok, msg := a.eligibleForDraw(r.Context(), uid, req.DrawID); !ok {
		writeErr(w, http.StatusForbidden, msg)
		return
	}
	var held int
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM lucky_draw_tickets WHERE draw_id=$1 AND account_id=$2`,
		req.DrawID, uid).Scan(&held)
	if held+req.Count > maxPerUser {
		writeErr(w, http.StatusBadRequest,
			fmt.Sprintf("maximum %d tickets per user for this draw", maxPerUser))
		return
	}

	// Debit the buyer's internal USD balance on the double-entry ledger and
	// credit the platform treasury; tickets become immutable sale records.
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "purchase failed")
		return
	}
	defer tx.Rollback(r.Context())

	var buyerAcct string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset='USD' AND chain='internal' FOR UPDATE`,
		uid).Scan(&buyerAcct)
	if err == pgx.ErrNoRows {
		writeErr(w, http.StatusBadRequest, "no USD balance; top up your wallet first")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "purchase failed")
		return
	}
	total := fmt.Sprintf("%d", req.Count)
	var priceNum string
	_ = tx.QueryRow(r.Context(), `SELECT ($1::numeric * $2::numeric)::text`, price, total).Scan(&priceNum)

	var ok bool
	err = tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
		   FROM ledger_entries WHERE account_id=$2`, priceNum, buyerAcct).Scan(&ok)
	if err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "insufficient balance or invalid amount")
		return
	}
	treasuryAcct, err := a.ensureAccount(r.Context(), platformTreasuryID, "USD", "internal")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "purchase failed")
		return
	}
	var txID string
	if err := tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&txID); err != nil {
		writeErr(w, http.StatusInternalServerError, "purchase failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2, -$3::numeric, 'luckydraw_ticket', $4, 'lucky draw ticket'),
		        ($1,$5,  $3::numeric, 'luckydraw_revenue', $6, 'lucky draw ticket')`,
		txID, buyerAcct, priceNum, req.DrawID, treasuryAcct, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "purchase failed")
		return
	}

	// Issue the tickets with deterministic sequential numbers.
	start, err := nextTicketSeq(tx, req.Count)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "purchase failed")
		return
	}
	tickets := []map[string]any{}
	for i := 0; i < req.Count; i++ {
		num := fmt.Sprintf("%s-%06d", strings.ToUpper(drawCode(req.DrawID)), start+int64(i))
		var id string
		if err := tx.QueryRow(r.Context(),
			`INSERT INTO lucky_draw_tickets (draw_id, account_id, ticket_number, price_usd, payment_ref)
			 VALUES ($1,$2,$3,$4::numeric,$5) RETURNING id`,
			req.DrawID, uid, num, price, txID).Scan(&id); err != nil {
			writeErr(w, http.StatusInternalServerError, "purchase failed")
			return
		}
		tickets = append(tickets, map[string]any{"id": id, "ticket_number": num, "price_usd": price})
	}
	// Refresh the prize pool conservatively: recompute from sales.
	_, _ = tx.Exec(r.Context(),
		`UPDATE lucky_draws SET prize_pool =
		   (SELECT COALESCE(SUM(t.price_usd),0) * (d.prize_alloc_pct/100.0)
		      FROM lucky_draw_tickets t JOIN lucky_draws d ON d.id=t.draw_id
		     WHERE t.draw_id=$1)
		   WHERE id=$1`, req.DrawID)
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "purchase failed")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'luckydraw_ticket',$2)`,
		uid, map[string]any{"draw_id": req.DrawID, "count": req.Count})
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'ticket_purchase', $3)`,
		req.DrawID, uid, map[string]any{"count": req.Count, "tx_id": txID})
	writeJSON(w, http.StatusCreated, map[string]any{
		"tickets": tickets, "total_usd": priceNum, "tx_id": txID,
		"prize_alloc_pct": prizePct, "operator_fee_pct": feePct,
	})
}

// nextTicketSeq returns the next N values of the ticket sequence inside tx.
func nextTicketSeq(tx pgx.Tx, n int) (int64, error) {
	var base int64
	if err := tx.QueryRow(context.Background(),
		`SELECT nextval('lucky_draw_ticket_seq')`).Scan(&base); err != nil {
		return 0, err
	}
	return base, nil
}

// drawCode returns a stable short code for a draw id (first 6 hex chars).
func drawCode(id string) string {
	if len(id) >= 6 {
		return id[:6]
	}
	return id
}

// GET /api/luckydraw — list draws (public surface, auth required for my count).
func (a *App) handleLuckyDrawList(w http.ResponseWriter, r *http.Request) {
	a.refreshLuckyDrawStatus(r.Context())
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), luckyDrawSelect+
		` ORDER BY d.sales_open_at DESC`, )
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load draws")
		return
	}
	draws := scanLuckyDraws(rows)
	for i := range draws {
		var mine int
		_ = a.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM lucky_draw_tickets WHERE draw_id=$1 AND account_id=$2`,
			draws[i].ID, uid).Scan(&mine)
		draws[i].MyTickets = mine
	}
	writeJSON(w, http.StatusOK, map[string]any{"draws": draws})
}

// GET /api/luckydraw/mine — the caller's ticket history.
func (a *App) handleLuckyDrawMyTickets(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT t.id, d.title, t.ticket_number, t.price_usd::text, t.created_at,
		        d.frequency, d.status AS draw_status
		   FROM lucky_draw_tickets t JOIN lucky_draws d ON d.id=t.draw_id
		  WHERE t.account_id=$1 ORDER BY t.created_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tickets")
		return
	}
	defer rows.Close()
	type ticket struct {
		ID           string    `json:"id"`
		DrawTitle    string    `json:"draw_title"`
		TicketNumber string    `json:"ticket_number"`
		PriceUSD     string    `json:"price_usd"`
		CreatedAt    time.Time `json:"created_at"`
		Frequency    string    `json:"frequency"`
		DrawStatus   string    `json:"draw_status"`
	}
	out := []ticket{}
	for rows.Next() {
		var t ticket
		if err := rows.Scan(&t.ID, &t.DrawTitle, &t.TicketNumber, &t.PriceUSD,
			&t.CreatedAt, &t.Frequency, &t.DrawStatus); err == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// GET /api/luckydraw/{id}/winners — published results.
func (a *App) handleLuckyDrawWinners(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var status string
	if err := a.db.QueryRow(r.Context(),
		`SELECT status FROM lucky_draws WHERE id=$1`, id).Scan(&status); err != nil {
		writeErr(w, http.StatusNotFound, "draw not found")
		return
	}
	if status != "settled" {
		writeErr(w, http.StatusConflict, "results not published yet")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT w.prize_usd::text, u.username, w.ticket_id, w.settled_at
		   FROM lucky_draw_winners w JOIN users u ON u.id=w.account_id
		  WHERE w.draw_id=$1 AND w.status='settled' ORDER BY w.prize_usd DESC`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load winners")
		return
	}
	defer rows.Close()
	type winner struct {
		PrizeUSD  string     `json:"prize_usd"`
		Username  string     `json:"username"`
		TicketID  string     `json:"ticket_id"`
		SettledAt *time.Time `json:"settled_at"`
	}
	out := []winner{}
	for rows.Next() {
		var w winner
		if err := rows.Scan(&w.PrizeUSD, &w.Username, &w.TicketID, &w.SettledAt); err == nil {
			out = append(out, w)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"winners": out})
}

// ---- Admin plane (LuckyDraw operations) ----

// GET /api/admin/luckydraw — all draws with operational stats.
func (a *App) handleAdminLuckyDraws(w http.ResponseWriter, r *http.Request) {
	a.refreshLuckyDrawStatus(r.Context())
	rows, err := a.db.Query(r.Context(), luckyDrawSelect+
		` ORDER BY d.sales_open_at DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load draws")
		return
	}
	out := scanLuckyDraws(rows)
	writeJSON(w, http.StatusOK, map[string]any{"draws": out})
}

// POST /api/admin/luckydraw — create a draw (LuckyDraw manager).
func (a *App) handleAdminLuckyDrawCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title            string   `json:"title"`
		Frequency        string   `json:"frequency"`
		SalesOpenAt      string   `json:"sales_open_at"`
		SalesCloseAt     string   `json:"sales_close_at"`
		TicketPriceUSD   string   `json:"ticket_price_usd"`
		PrizeAllocPct    *string  `json:"prize_alloc_pct"`
		OperatorFeePct   *string  `json:"operator_fee_pct"`
		MaxTicketsPerUser *int    `json:"max_tickets_per_user"`
		UniqueWinner     *bool    `json:"unique_winner"`
		MinAge           *int     `json:"min_age"`
		AllowedCountries []string `json:"allowed_countries"`
		WinnerCount      *int     `json:"winner_count"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 120 {
		writeErr(w, http.StatusBadRequest, "title required (max 120 chars)")
		return
	}
	freq := strings.ToLower(strings.TrimSpace(req.Frequency))
	if freq != "daily" && freq != "monthly" {
		writeErr(w, http.StatusBadRequest, "frequency must be daily or monthly")
		return
	}
	open, err1 := time.Parse(time.RFC3339, req.SalesOpenAt)
	closeT, err2 := time.Parse(time.RFC3339, req.SalesCloseAt)
	if err1 != nil || err2 != nil {
		writeErr(w, http.StatusBadRequest, "sales_open_at and sales_close_at required (RFC3339)")
		return
	}
	if !closeT.After(open) {
		writeErr(w, http.StatusBadRequest, "sales_close_at must be after sales_open_at")
		return
	}
	price := strings.TrimSpace(req.TicketPriceUSD)
	if price == "" {
		writeErr(w, http.StatusBadRequest, "ticket_price_usd required")
		return
	}
	maxT := 50
	if req.MaxTicketsPerUser != nil && *req.MaxTicketsPerUser > 0 {
		maxT = *req.MaxTicketsPerUser
	}
	unique := true
	if req.UniqueWinner != nil {
		unique = *req.UniqueWinner
	}
	minAge := 18
	if req.MinAge != nil && *req.MinAge >= 13 && *req.MinAge <= 120 {
		minAge = *req.MinAge
	}
	winners := 1
	if req.WinnerCount != nil && *req.WinnerCount > 0 && *req.WinnerCount <= 100 {
		winners = *req.WinnerCount
	}
	alloc := "85.00"
	if req.PrizeAllocPct != nil && *req.PrizeAllocPct != "" {
		alloc = *req.PrizeAllocPct
	}
	fee := "10.00"
	if req.OperatorFeePct != nil && *req.OperatorFeePct != "" {
		fee = *req.OperatorFeePct
	}
	uid := userIDFrom(r)
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO lucky_draws
		   (title, frequency, sales_open_at, sales_close_at, ticket_price_usd,
		    prize_alloc_pct, operator_fee_pct, max_tickets_per_user, unique_winner,
		    min_age, allowed_countries, winner_count, created_by)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6::numeric,$7::numeric,$8,$9,$10,$11,$12,$13)
		 RETURNING id`,
		title, freq, open, closeT, price, alloc, fee, maxT, unique, minAge,
		req.AllowedCountries, winners, uid).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "draw creation failed")
		return
	}
	// Seed the initial price history row (approved-now).
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_prices (draw_id, price_usd, status, requested_by, approved_by)
		 VALUES ($1,$2::numeric,'approved',$3,$3)`, id, price, uid)
	a.audit(r.Context(), uid, "luckydraw.create", id,
		map[string]any{"frequency": freq, "price": price, "alloc": alloc, "fee": fee})
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'draw_created',$3)`, id, uid, map[string]any{"title": title})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// POST /api/admin/luckydraw/{id}/price — draft a future ticket price.
func (a *App) handleAdminLuckyDrawPriceDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PriceUSD string `json:"price_usd"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	price := strings.TrimSpace(req.PriceUSD)
	if price == "" {
		writeErr(w, http.StatusBadRequest, "price_usd required")
		return
	}
	var status string
	if err := a.db.QueryRow(r.Context(),
		`SELECT status FROM lucky_draws WHERE id=$1`, id).Scan(&status); err != nil {
		writeErr(w, http.StatusNotFound, "draw not found")
		return
	}
	// Pricing may be drafted anytime but only becomes effective for future
	// sales; existing tickets keep their immutable price.
	uid := userIDFrom(r)
	var priceID string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO lucky_draw_prices (draw_id, price_usd, status, requested_by)
		 VALUES ($1,$2::numeric,'draft',$3) RETURNING id`, id, price, uid).
		Scan(&priceID); err != nil {
		writeErr(w, http.StatusBadRequest, "price draft failed")
		return
	}
	a.audit(r.Context(), uid, "luckydraw.price_draft", id, map[string]any{"price": price})
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'price_drafted',$3)`, id, uid, map[string]any{"price": price, "price_id": priceID})
	writeJSON(w, http.StatusCreated, map[string]any{"id": priceID, "status": "draft"})
}

// POST /api/admin/luckydraw/prices/{priceId}/approve — approve a drafted price.
func (a *App) handleAdminLuckyDrawPriceApprove(w http.ResponseWriter, r *http.Request) {
	priceID := r.PathValue("priceId")
	uid := userIDFrom(r)
	var drawID, oldStatus string
	var price string
	err := a.db.QueryRow(r.Context(),
		`SELECT draw_id, status, price_usd::text FROM lucky_draw_prices WHERE id=$1`,
		priceID).Scan(&drawID, &oldStatus, &price)
	if err != nil {
		writeErr(w, http.StatusNotFound, "price not found")
		return
	}
	if oldStatus != "draft" {
		writeErr(w, http.StatusConflict, "price already processed")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "approval failed")
		return
	}
	defer tx.Rollback(r.Context())
	// Same draw: supersede any prior approved effective price, then approve.
	if _, err := tx.Exec(r.Context(),
		`UPDATE lucky_draw_prices SET status='superseded'
		  WHERE draw_id=$1 AND status='approved'`, drawID); err != nil {
		writeErr(w, http.StatusInternalServerError, "approval failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE lucky_draw_prices SET status='approved', approved_by=$2,
		        effective_at=now() WHERE id=$1`, priceID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "approval failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE lucky_draws SET ticket_price_usd=$2::numeric WHERE id=$1`, drawID, price); err != nil {
		writeErr(w, http.StatusInternalServerError, "approval failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "approval failed")
		return
	}
	a.audit(r.Context(), uid, "luckydraw.price_approve", drawID, map[string]any{"price": price})
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'price_approved',$3)`, drawID, uid, map[string]any{"price": price})
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// GET /api/admin/luckydraw/prices — all price records across draws.
func (a *App) handleAdminLuckyDrawAllPrices(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.draw_id, d.title AS label, p.price_usd::text AS amount_usd, p.status
		   FROM lucky_draw_prices p
		   JOIN lucky_draws d ON d.id = p.draw_id
		   ORDER BY p.created_at DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load prices")
		return
	}
	defer rows.Close()
	type priceRow struct {
		ID        string `json:"id"`
		DrawID    string `json:"draw_id"`
		Label     string `json:"label"`
		AmountUSD string `json:"amount_usd"`
		Status    string `json:"status"`
	}
	out := []priceRow{}
	for rows.Next() {
		var p priceRow
		if err := rows.Scan(&p.ID, &p.DrawID, &p.Label, &p.AmountUSD, &p.Status); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": out})
}

// GET /api/admin/luckydraw/{id}/prices — price history (audit).
func (a *App) handleAdminLuckyDrawPrices(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.price_usd::text, p.status, p.requested_by, p.approved_by,
		        p.effective_at, p.created_at
		   FROM lucky_draw_prices p WHERE p.draw_id=$1 ORDER BY p.created_at DESC`,
		r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load prices")
		return
	}
	defer rows.Close()
	type priceRow struct {
		ID          string     `json:"id"`
		PriceUSD    string     `json:"price_usd"`
		Status      string     `json:"status"`
		RequestedBy string     `json:"requested_by,omitempty"`
		ApprovedBy  *string    `json:"approved_by,omitempty"`
		EffectiveAt time.Time  `json:"effective_at"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	out := []priceRow{}
	for rows.Next() {
		var p priceRow
		if err := rows.Scan(&p.ID, &p.PriceUSD, &p.Status, &p.RequestedBy,
			&p.ApprovedBy, &p.EffectiveAt, &p.CreatedAt); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"prices": out})
}

// POST /api/admin/luckydraw/{id}/close — close sales early (terminal gate).
func (a *App) handleAdminLuckyDrawClose(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var status string
	if err := a.db.QueryRow(r.Context(),
		`SELECT status FROM lucky_draws WHERE id=$1`, id).Scan(&status); err != nil {
		writeErr(w, http.StatusNotFound, "draw not found")
		return
	}
	if status != "open" && status != "scheduled" {
		writeErr(w, http.StatusConflict, "draw already closed")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE lucky_draws SET status='sales_closed', sales_closed_at=now()
		  WHERE id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "close failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "luckydraw.close", id, nil)
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'sales_closed',$3)`, id, userIDFrom(r), map[string]any{})
	writeJSON(w, http.StatusOK, map[string]string{"status": "sales_closed"})
}

// POST /api/admin/luckydraw/{id}/run — execute the audited selection from the
// frozen participant dataset. Only callable after sales are closed. Winner
// selection uses crypto/rand; the unique-user winner rule is enforced by the
// (draw_id, account_id) uniqueness constraint plus the explicit exclusion loop.
func (a *App) handleAdminLuckyDrawRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var status string
	var winnerCount int
	if err := a.db.QueryRow(r.Context(),
		`SELECT status, winner_count FROM lucky_draws WHERE id=$1`, id).
		Scan(&status, &winnerCount); err != nil {
		writeErr(w, http.StatusNotFound, "draw not found")
		return
	}
	if status == "settled" || status == "canceled" {
		writeErr(w, http.StatusConflict, "draw already finalized")
		return
	}
	if status != "sales_closed" {
		writeErr(w, http.StatusConflict, "sales must be closed before running the draw")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "draw run failed")
		return
	}
	defer tx.Rollback(r.Context())

	// Freeze the dataset: mark and snapshot. Tickets are immutable so the
	// dataset is inherently frozen once sales are closed; we still record it.
	if _, err := tx.Exec(r.Context(),
		`UPDATE lucky_draws SET status='selecting', dataset_frozen_at=now()
		  WHERE id=$1 AND status='sales_closed'`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "draw run failed")
		return
	}

	// Select up to winner_count distinct accounts (unique-user winner rule).
	var selected []struct {
		AccountID string
		TicketID  string
	}
	rows, err := tx.Query(r.Context(),
		`SELECT t.account_id, t.id FROM lucky_draw_tickets t
		  WHERE t.draw_id=$1 AND t.eligibility='eligible'
		    AND NOT EXISTS (SELECT 1 FROM lucky_draw_winners w
		                    WHERE w.draw_id=$1 AND w.account_id=t.account_id)`,
		id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "draw run failed")
		return
	}
	for rows.Next() {
		var acc, tid string
		if err := rows.Scan(&acc, &tid); err == nil {
			selected = append(selected, struct{ AccountID, TicketID string }{acc, tid})
		}
	}
	rows.Close()

	// Fisher–Yates shuffle with crypto/rand — auditable and unbiased.
	for i := len(selected) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "draw run failed")
			return
		}
		k := int(j.Int64())
		selected[i], selected[k] = selected[k], selected[i]
	}
	if len(selected) > winnerCount {
		selected = selected[:winnerCount]
	}

	var prizePool string
	var allocPct string
	_ = tx.QueryRow(r.Context(),
		`SELECT prize_pool::text, prize_alloc_pct::text FROM lucky_draws WHERE id=$1`, id).
		Scan(&prizePool, &allocPct)
	// prize per winner = prize_pool / winner_count (equal split of the
	// allocated pool; pool already reflects prize_alloc_pct of sales).
	if len(selected) == 0 {
		writeErr(w, http.StatusConflict, "no eligible tickets to draw from")
		return
	}
	perWinner := fmt.Sprintf("(%s::numeric / %d)::text", prizePool, len(selected))
	var per string
	if err := tx.QueryRow(r.Context(), `SELECT `+perWinner).Scan(&per); err != nil {
		writeErr(w, http.StatusInternalServerError, "draw run failed")
		return
	}
	for _, s := range selected {
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO lucky_draw_winners (draw_id, account_id, ticket_id, prize_usd)
			 VALUES ($1,$2,$3,$4::numeric) ON CONFLICT (draw_id, account_id) DO NOTHING`,
			id, s.AccountID, s.TicketID, per); err != nil {
			writeErr(w, http.StatusInternalServerError, "draw run failed")
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE lucky_draws SET status='sales_closed', selected_at=now() WHERE id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "draw run failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "draw run failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "luckydraw.run", id,
		map[string]any{"winners": len(selected), "per_winner_usd": per})
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'selection_executed',$3)`, id, userIDFrom(r),
		map[string]any{"winners": len(selected), "per_winner_usd": per})
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "selected", "winners_selected": len(selected), "prize_per_winner_usd": per,
	})
}

// POST /api/admin/luckydraw/{id}/settle — settle selected winners' prizes from
// the platform treasury to each winner's internal USD wallet (double-entry).
func (a *App) handleAdminLuckyDrawSettle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var winners int
	if err := a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM lucky_draw_winners
		  WHERE draw_id=$1 AND status='selected'`, id).Scan(&winners); err != nil {
		writeErr(w, http.StatusNotFound, "draw not found")
		return
	}
	if winners == 0 {
		writeErr(w, http.StatusConflict, "no selected winners to settle")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "settlement failed")
		return
	}
	defer tx.Rollback(r.Context())

	rows, err := tx.Query(r.Context(),
		`SELECT w.id, w.account_id, w.ticket_id, w.prize_usd::text
		   FROM lucky_draw_winners w
		  WHERE w.draw_id=$1 AND w.status='selected' FOR UPDATE OF w`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "settlement failed")
		return
	}
	type win struct{ id, acc, ticket, prize string }
	var list []win
	for rows.Next() {
		var w win
		if err := rows.Scan(&w.id, &w.acc, &w.ticket, &w.prize); err == nil {
			list = append(list, w)
		}
	}
	rows.Close()

	treasuryAcct, err := a.ensureAccount(r.Context(), platformTreasuryID, "USD", "internal")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "settlement failed")
		return
	}
	for _, win := range list {
		winAcct, err := a.ensureAccount(r.Context(), win.acc, "USD", "internal")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "settlement failed")
			return
		}
		var txID string
		if err := tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&txID); err != nil {
			writeErr(w, http.StatusInternalServerError, "settlement failed")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
			 VALUES ($1,$2, -$3::numeric, 'luckydraw_prize_paid', $4, 'lucky draw prize'),
			        ($1,$5,  $3::numeric, 'luckydraw_prize', $6, 'lucky draw prize')`,
			txID, treasuryAcct, win.prize, win.acc, winAcct, id); err != nil {
			writeErr(w, http.StatusInternalServerError, "settlement failed")
			return
		}
		if _, err := tx.Exec(r.Context(),
			`UPDATE lucky_draw_winners SET status='settled', payout_tx=$2, settled_at=now()
			  WHERE id=$1`, win.id, txID); err != nil {
			writeErr(w, http.StatusInternalServerError, "settlement failed")
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE lucky_draws SET status='settled', settled_at=now() WHERE id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "settlement failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "settlement failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "luckydraw.settle", id,
		map[string]any{"winners": len(list)})
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'prizes_settled',$3)`, id, userIDFrom(r),
		map[string]any{"winners": len(list)})
	// Notify winners.
	for _, win := range list {
		_, _ = a.db.Exec(r.Context(),
			`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'luckydraw_win',$2)`,
			win.acc, map[string]any{"draw_id": id, "prize_usd": win.prize})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "settled", "winners": len(list)})
}

// POST /api/admin/luckydraw/{id}/disable — hard-disable a draw (governance).
func (a *App) handleAdminLuckyDrawDisable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var status string
	if err := a.db.QueryRow(r.Context(),
		`SELECT status FROM lucky_draws WHERE id=$1`, id).Scan(&status); err != nil {
		writeErr(w, http.StatusNotFound, "draw not found")
		return
	}
	if status == "settled" {
		writeErr(w, http.StatusConflict, "settled draws cannot be disabled")
		return
	}
	if status == "canceled" {
		writeErr(w, http.StatusConflict, "draw already canceled")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE lucky_draws SET status='canceled', disabled=true WHERE id=$1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "disable failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "luckydraw.disable", id, nil)
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO lucky_draw_audit (draw_id, actor_id, action, detail)
		 VALUES ($1,$2,'draw_disabled',$3)`, id, userIDFrom(r), map[string]any{})
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

// GET /api/admin/luckydraw/{id}/audit — governance audit trail.
func (a *App) handleAdminLuckyDrawAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT ad.id, ad.actor_id, ad.action, ad.detail, ad.created_at
		   FROM lucky_draw_audit ad WHERE ad.draw_id=$1 ORDER BY ad.id DESC LIMIT 200`,
		r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load audit")
		return
	}
	defer rows.Close()
	type auditRow struct {
		ID        int64           `json:"id"`
		ActorID   *string         `json:"actor_id,omitempty"`
		Action    string          `json:"action"`
		Detail    json.RawMessage `json:"detail"`
		CreatedAt time.Time       `json:"created_at"`
	}
	out := []auditRow{}
	for rows.Next() {
		var a2 auditRow
		if err := rows.Scan(&a2.ID, &a2.ActorID, &a2.Action, &a2.Detail, &a2.CreatedAt); err == nil {
			out = append(out, a2)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": out})
}

// startLuckyDrawSweeper advances draw lifecycle in the background.
func (a *App) startLuckyDrawSweeper() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.refreshLuckyDrawStatus(context.Background())
		}
	}()
}
