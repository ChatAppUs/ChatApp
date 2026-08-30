package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// P2P merchant program: verified merchants get a badge on their offers and
// tier-scaled trading limits. Applications are reviewed by admins holding the
// merchants.review permission. Tier caps are enforced at trade-open time:
// per-trade value and rolling 24h completed sell volume, both measured in USD
// via convert_rates.

type merchantJSON struct {
	UserID       string     `json:"user_id"`
	Username     string     `json:"username"`
	BusinessName string     `json:"business_name"`
	Status       string     `json:"status"`
	Tier         int        `json:"tier"`
	TierName     string     `json:"tier_name"`
	Note         string     `json:"note"`
	AppliedAt    time.Time  `json:"applied_at"`
	DecidedAt    *time.Time `json:"decided_at"`
}

const merchantSelect = `SELECT m.user_id, u.username, m.business_name, m.status, m.tier,
       t.name, m.note, m.applied_at, m.decided_at
 FROM p2p_merchants m
 JOIN users u ON u.id = m.user_id
 JOIN p2p_merchant_tiers t ON t.level = m.tier `

func scanMerchants(rows pgx.Rows) []merchantJSON {
	defer rows.Close()
	out := []merchantJSON{}
	for rows.Next() {
		var m merchantJSON
		if err := rows.Scan(&m.UserID, &m.Username, &m.BusinessName, &m.Status,
			&m.Tier, &m.TierName, &m.Note, &m.AppliedAt, &m.DecidedAt); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// POST /api/p2p/merchant/apply — KYC-verified users apply for the merchant
// program. Re-application is allowed after a rejection or revocation.
func (a *App) handleMerchantApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BusinessName string `json:"business_name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.BusinessName = strings.TrimSpace(req.BusinessName)
	if len(req.BusinessName) < 2 || len(req.BusinessName) > 100 {
		writeErr(w, http.StatusBadRequest, "business name must be 2-100 characters")
		return
	}
	uid := userIDFrom(r)
	if !a.requireKYC(r.Context(), uid) {
		writeErr(w, http.StatusForbidden, "KYC verification required to become a merchant")
		return
	}
	var status string
	err := a.db.QueryRow(r.Context(),
		`SELECT status FROM p2p_merchants WHERE user_id=$1`, uid).Scan(&status)
	if err == nil && (status == "pending" || status == "verified") {
		writeErr(w, http.StatusConflict, "merchant application already "+status)
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO p2p_merchants (user_id, business_name) VALUES ($1,$2)
		 ON CONFLICT (user_id) DO UPDATE SET business_name=$2, status='pending',
		       tier=1, note='', applied_at=now(), decided_by=NULL, decided_at=NULL`,
		uid, req.BusinessName); err != nil {
		writeErr(w, http.StatusInternalServerError, "application failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "pending"})
}

// GET /api/p2p/merchant/status — own application/merchant state with the
// tier limits currently in force.
func (a *App) handleMerchantStatus(w http.ResponseWriter, r *http.Request) {
	var m merchantJSON
	var maxTrade, dailyVol string
	err := a.db.QueryRow(r.Context(), merchantSelect+`
		 WHERE m.user_id=$1`, userIDFrom(r)).
		Scan(&m.UserID, &m.Username, &m.BusinessName, &m.Status, &m.Tier,
			&m.TierName, &m.Note, &m.AppliedAt, &m.DecidedAt)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"merchant": nil})
		return
	}
	_ = a.db.QueryRow(r.Context(),
		`SELECT trim_scale(max_trade_usd)::text, trim_scale(daily_volume_usd)::text
		 FROM p2p_merchant_tiers WHERE level=$1`, m.Tier).Scan(&maxTrade, &dailyVol)
	writeJSON(w, http.StatusOK, map[string]any{
		"merchant":         m,
		"max_trade_usd":    maxTrade,
		"daily_volume_usd": dailyVol,
	})
}

// GET /api/p2p/merchant/tiers — public tier ladder.
func (a *App) handleMerchantTiers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT level, name, trim_scale(max_trade_usd)::text, trim_scale(daily_volume_usd)::text,
		        min_completed_trades, min_completion_rate::text
		 FROM p2p_merchant_tiers ORDER BY level`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tiers")
		return
	}
	defer rows.Close()
	type tier struct {
		Level       int    `json:"level"`
		Name        string `json:"name"`
		MaxTrade    string `json:"max_trade_usd"`
		DailyVolume string `json:"daily_volume_usd"`
		MinTrades   int    `json:"min_completed_trades"`
		MinRate     string `json:"min_completion_rate"`
	}
	out := []tier{}
	for rows.Next() {
		var t tier
		if err := rows.Scan(&t.Level, &t.Name, &t.MaxTrade, &t.DailyVolume,
			&t.MinTrades, &t.MinRate); err == nil {
			out = append(out, t)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tiers": out})
}

// enforceMerchantLimits validates a trade being opened against a seller who
// is a verified merchant. tradeUSD is the crypto value of the trade priced
// via convert_rates. Returns an error message when a cap is exceeded.
func (a *App) enforceMerchantLimits(ctx context.Context, tx pgx.Tx, sellerID string, tradeUSD float64) string {
	var tier int
	err := tx.QueryRow(ctx,
		`SELECT tier FROM p2p_merchants WHERE user_id=$1 AND status='verified'`,
		sellerID).Scan(&tier)
	if err != nil {
		return "" // not a merchant: platform-default limits only
	}
	var maxTrade, dailyVol float64
	if err := tx.QueryRow(ctx,
		`SELECT max_trade_usd::float8, daily_volume_usd::float8
		 FROM p2p_merchant_tiers WHERE level=$1`, tier).Scan(&maxTrade, &dailyVol); err != nil {
		return ""
	}
	if tradeUSD > maxTrade {
		return "trade exceeds the merchant tier per-trade limit"
	}
	var used float64
	_ = tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(t.crypto_amount * cr.usd_rate),0)::float8
		 FROM p2p_trades t
		 JOIN convert_rates cr ON cr.asset = t.asset AND cr.chain = t.chain
		 WHERE t.seller_id=$1 AND t.status IN ('completed','resolved_buyer')
		   AND t.updated_at > now() - interval '24 hours'`, sellerID).Scan(&used)
	if used+tradeUSD > dailyVol {
		return "trade exceeds the merchant tier daily volume limit"
	}
	return ""
}

// ---- Admin: merchant review + tier management ----

// GET /api/admin/p2p/merchants?status=pending|verified|rejected|revoked
func (a *App) handleAdminListMerchants(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	query := merchantSelect
	args := []any{}
	switch status {
	case "pending", "verified", "rejected", "revoked":
		query += ` WHERE m.status=$1`
		args = append(args, status)
	}
	query += ` ORDER BY m.applied_at ASC LIMIT 200`
	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load merchants")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"merchants": scanMerchants(rows)})
}

// POST /api/admin/p2p/merchants/{userId}/review — approve (with tier) or
// reject a pending application.
func (a *App) handleAdminReviewMerchant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"` // approve | reject
		Tier     int    `json:"tier"`
		Note     string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Decision != "approve" && req.Decision != "reject" {
		writeErr(w, http.StatusBadRequest, "decision must be approve or reject")
		return
	}
	if len(req.Note) > 500 {
		writeErr(w, http.StatusBadRequest, "note too long")
		return
	}
	adminID := userIDFrom(r)
	uid := r.PathValue("userId")
	if req.Decision == "reject" {
		tag, err := a.db.Exec(r.Context(),
			`UPDATE p2p_merchants SET status='rejected', note=$3, decided_by=$2, decided_at=now()
			 WHERE user_id=$1 AND status='pending'`, uid, adminID, strings.TrimSpace(req.Note))
		if err != nil || tag.RowsAffected() == 0 {
			writeErr(w, http.StatusNotFound, "no pending application for this user")
			return
		}
		a.audit(r.Context(), adminID, "merchant_reject", uid, nil)
		a.notifyUser(r.Context(), uid, "merchant_rejected", map[string]string{})
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
		return
	}
	tier := req.Tier
	if tier == 0 {
		tier = 1
	}
	var tierOK bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM p2p_merchant_tiers WHERE level=$1)`, tier).Scan(&tierOK)
	if !tierOK {
		writeErr(w, http.StatusBadRequest, "unknown tier")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE p2p_merchants SET status='verified', tier=$3, note=$4, decided_by=$2, decided_at=now()
		 WHERE user_id=$1 AND status='pending'`, uid, adminID, tier, strings.TrimSpace(req.Note))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "no pending application for this user")
		return
	}
	a.audit(r.Context(), adminID, "merchant_approve", uid, map[string]any{"tier": tier})
	a.notifyUser(r.Context(), uid, "merchant_verified", map[string]string{})
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified", "tier": strconv.Itoa(tier)})
}

// POST /api/admin/p2p/merchants/{userId}/revoke — strip merchant status.
func (a *App) handleAdminRevokeMerchant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	adminID := userIDFrom(r)
	uid := r.PathValue("userId")
	tag, err := a.db.Exec(r.Context(),
		`UPDATE p2p_merchants SET status='revoked', note=$3, decided_by=$2, decided_at=now()
		 WHERE user_id=$1 AND status='verified'`, uid, adminID, strings.TrimSpace(req.Note))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "merchant not found or not verified")
		return
	}
	a.audit(r.Context(), adminID, "merchant_revoke", uid, nil)
	a.notifyUser(r.Context(), uid, "merchant_revoked", map[string]string{})
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// POST /api/admin/p2p/merchants/{userId}/tier — promote/demote a verified
// merchant. Eligibility requirements (min trades / completion rate) are
// enforced for upward moves.
func (a *App) handleAdminSetMerchantTier(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tier int `json:"tier"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := r.PathValue("userId")
	var minTrades, minRate float64
	var current int
	err := a.db.QueryRow(r.Context(),
		`SELECT m.tier, t.min_completed_trades, t.min_completion_rate::float8
		 FROM p2p_merchants m JOIN p2p_merchant_tiers t ON t.level=$2
		 WHERE m.user_id=$1 AND m.status='verified'`, uid, req.Tier).
		Scan(&current, &minTrades, &minRate)
	if err != nil {
		writeErr(w, http.StatusNotFound, "verified merchant or target tier not found")
		return
	}
	if req.Tier > current {
		var completed, total float64
		_ = a.db.QueryRow(r.Context(),
			`SELECT
			   COUNT(*) FILTER (WHERE status IN ('completed','resolved_buyer'))::float8,
			   COUNT(*)::float8
			 FROM p2p_trades WHERE seller_id=$1`, uid).Scan(&completed, &total)
		rate := 1.0
		if total > 0 {
			rate = completed / total
		}
		if completed < minTrades || rate < minRate {
			writeErr(w, http.StatusBadRequest, "merchant does not meet tier eligibility requirements")
			return
		}
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE p2p_merchants SET tier=$2 WHERE user_id=$1 AND status='verified'`,
		uid, req.Tier); err != nil {
		writeErr(w, http.StatusInternalServerError, "tier update failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "merchant_tier_set", uid, map[string]any{"tier": req.Tier})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "tier": strconv.Itoa(req.Tier)})
}

// POST /api/admin/p2p/merchant-tiers — upsert a tier definition.
func (a *App) handleAdminUpsertMerchantTier(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level       int    `json:"level"`
		Name        string `json:"name"`
		MaxTrade    string `json:"max_trade_usd"`
		DailyVolume string `json:"daily_volume_usd"`
		MinTrades   int    `json:"min_completed_trades"`
		MinRate     string `json:"min_completion_rate"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Level < 1 || req.Level > 10 || req.Name == "" || len(req.Name) > 50 {
		writeErr(w, http.StatusBadRequest, "level 1-10 and a short name required")
		return
	}
	minRate := req.MinRate
	if minRate == "" {
		minRate = "0"
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO p2p_merchant_tiers (level, name, max_trade_usd, daily_volume_usd,
		                                 min_completed_trades, min_completion_rate)
		 VALUES ($1,$2,$3::numeric,$4::numeric,$5,$6::numeric)
		 ON CONFLICT (level) DO UPDATE SET name=$2, max_trade_usd=$3::numeric,
		       daily_volume_usd=$4::numeric, min_completed_trades=$5,
		       min_completion_rate=$6::numeric`,
		req.Level, req.Name, req.MaxTrade, req.DailyVolume, req.MinTrades, minRate)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid tier values")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "merchant_tier_upsert", strconv.Itoa(req.Level), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
