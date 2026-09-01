package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Native staking plane: users delegate any listed asset into the platform
// treasury for a fixed term at an APY locked in at stake time. Rewards are
// simple (non-compounding) and settled from the treasury ledger in one
// double-entry transaction — no external provider is involved.
//
// The superadmin may deploy pooled principal externally (recorded in
// staking_treasury_moves, ledgered as staking_treasury_out) and redeposit
// ('in'). Queued mature unlocks settle automatically as soon as liquidity
// returns; users can never unlock before ends_at.

type stakingAsset struct {
	ID        string    `json:"id"`
	Asset     string    `json:"asset"`
	Chain     string    `json:"chain"`
	APY       string    `json:"apy"`
	MinAmount string    `json:"min_amount"`
	Durations []int32   `json:"durations_days"`
	Active    bool      `json:"active"`
	Name      string    `json:"name"`
	Logo      string    `json:"logo_url"`
	PriceUSD  *string   `json:"price_usd"`
	Created   time.Time `json:"created_at"`
}

type stakePosition struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id,omitempty"`
	Username  string     `json:"username,omitempty"`
	Asset     string     `json:"asset"`
	Chain     string     `json:"chain"`
	Amount    string     `json:"amount"`
	APY       string     `json:"apy"`
	Duration  int32      `json:"duration_days"`
	StartedAt time.Time  `json:"started_at"`
	EndsAt    time.Time  `json:"ends_at"`
	Status    string     `json:"status"`
	Reward    *string    `json:"reward,omitempty"`
	Accrued   *string    `json:"accrued_estimate,omitempty"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

const stakePosSelect = `
  SELECT p.id, p.user_id, p.asset, p.chain, p.amount::text, p.apy::text,
         p.duration_days, p.started_at, p.ends_at, p.status,
         (p.amount * (p.apy/100.0) * (p.duration_days::numeric/365.0))::text AS reward,
         CASE WHEN p.status='active'
              THEN (p.amount * (p.apy/100.0) *
                    (GREATEST(0, EXTRACT(EPOCH FROM (LEAST(now(), p.ends_at) - p.started_at)))/86400.0)
                    / 365.0)::text
         END AS accrued, p.closed_at
  FROM stake_positions p`

// rewardExpr is the canonical simple-interest reward for a position. The
// whole expression is parenthesized so `rewardExpr + "::text"` is one cast.
const rewardExpr = `(amount * (apy/100.0) * (duration_days::numeric/365.0))`

// settleStakes pays out a mature position from treasury liquidity. Returns
// false (without side effects) when liquidity is insufficient.
func (a *App) settleStakes(ctx context.Context, posID string) (bool, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var userID, asset, chain, amount, reward, status string
	err = tx.QueryRow(ctx,
		`SELECT user_id, asset, chain, amount::text, `+rewardExpr+`::text, status
                 FROM stake_positions WHERE id=$1 FOR UPDATE`, posID).
		Scan(&userID, &asset, &chain, &amount, &reward, &status)
	if err != nil {
		return false, err
	}
	if status == "closed" {
		return true, nil
	}
	treasury, err := a.ensureAccount(ctx, platformTreasuryID, asset, chain)
	if err != nil {
		return false, err
	}
	userAcct, err := a.ensureAccount(ctx, userID, asset, chain)
	if err != nil {
		return false, err
	}
	var liquid bool
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE((SELECT SUM(amount) FROM ledger_entries WHERE account_id=$1),0)
                        >= ($2::numeric + $3::numeric) AND $2::numeric > 0`,
		treasury, amount, reward).Scan(&liquid); err != nil || !liquid {
		return false, err
	}
	var txID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&txID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
                 VALUES ($1::uuid,$2,-$4::numeric,'stake_unlock',$5::uuid,'Stake principal return'),
                        ($1::uuid,$2,-$6::numeric,'stake_reward',$5::uuid,'Staking reward'),
                        ($1::uuid,$3, $4::numeric,'stake_unlock',$7::uuid,'Stake principal return'),
                        ($1::uuid,$3, $6::numeric,'stake_reward',$7::uuid,'Staking reward')`,
		txID, treasury, userAcct, amount, userID, reward, platformTreasuryID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE stake_positions SET status='closed', closed_at=now(),
                        reward_paid=$2::numeric, unlock_tx=$3::uuid WHERE id=$1::uuid`,
		posID, reward, txID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// ---- user plane ----

func (a *App) handleStakingAssets(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT sa.id, sa.asset, sa.chain, sa.apy::text, sa.min_amount::text,
                        sa.durations_days, sa.active, sa.created_at,
                        COALESCE(pt.name,''), COALESCE(pt.logo_url,''),
                        (SELECT cp.price_usd::text FROM crypto_prices cp
                          WHERE cp.asset=sa.asset AND cp.chain=sa.chain)
                 FROM staking_assets sa
                 LEFT JOIN platform_tokens pt ON pt.symbol=sa.asset AND pt.chain=sa.chain
                 WHERE sa.active ORDER BY sa.asset, sa.chain`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load staking assets")
		return
	}
	defer rows.Close()
	out := []stakingAsset{}
	for rows.Next() {
		var s stakingAsset
		if err := rows.Scan(&s.ID, &s.Asset, &s.Chain, &s.APY, &s.MinAmount,
			&s.Durations, &s.Active, &s.Created, &s.Name, &s.Logo, &s.PriceUSD); err == nil {
			out = append(out, s)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

func (a *App) handleStake(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID  string `json:"asset_id"`
		Amount   string `json:"amount"`
		Duration int32  `json:"duration_days"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if !a.requireKYC(r.Context(), uid) {
		writeErr(w, http.StatusForbidden, "kyc verification required to stake")
		return
	}
	var asset, chain string
	var durations []int32
	var minAmount string
	err := a.db.QueryRow(r.Context(),
		`SELECT asset, chain, durations_days, min_amount::text
                 FROM staking_assets WHERE id=$1 AND active`, req.AssetID).
		Scan(&asset, &chain, &durations, &minAmount)
	if err != nil {
		writeErr(w, http.StatusNotFound, "staking asset not found")
		return
	}
	allowed := false
	for _, d := range durations {
		if d == req.Duration {
			allowed = true
		}
	}
	if !allowed {
		writeErr(w, http.StatusBadRequest, "duration not allowed for this asset")
		return
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	userAcct, err := a.ensureAccount(r.Context(), uid, asset, chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "wallet account unavailable")
		return
	}
	treasury, err := a.ensureAccount(r.Context(), platformTreasuryID, asset, chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "treasury account unavailable")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`SELECT id FROM wallet_accounts WHERE id=$1 FOR UPDATE`, userAcct); err != nil {
		writeErr(w, http.StatusInternalServerError, "wallet lock failed")
		return
	}
	var ok bool
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric >= $3::numeric AND $1::numeric > 0
                 FROM ledger_entries WHERE account_id=$2`,
		req.Amount, userAcct, minAmount).Scan(&ok); err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "insufficient balance or below minimum stake")
		return
	}
	var txID, posID string
	if err := tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&txID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to allocate tx id")
		return
	}
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO stake_positions (user_id, asset, chain, amount, apy, duration_days, ends_at, lock_tx)
                 SELECT $1, sa.asset, sa.chain, $2::numeric, sa.apy, $3,
                        now() + ($3::int * interval '1 day'), $4
                 FROM staking_assets sa WHERE sa.id=$5
                 RETURNING id`,
		uid, req.Amount, req.Duration, txID, req.AssetID).Scan(&posID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to open position")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
                 VALUES ($1,$2,-$3::numeric,'stake_lock',$4,'Staking principal lock'),
                        ($1,$5, $3::numeric,'stake_lock',$4,'Staking principal lock')`,
		txID, userAcct, req.Amount, platformTreasuryID, treasury); err != nil {
		writeErr(w, http.StatusInternalServerError, "ledger write failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "commit failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": posID, "status": "active"})
}

func (a *App) handleStakingPositions(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), stakePosSelect+` WHERE p.user_id=$1::uuid ORDER BY p.created_at DESC`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load positions")
		return
	}
	defer rows.Close()
	out := []stakePosition{}
	for rows.Next() {
		var p stakePosition
		if err := rows.Scan(&p.ID, &p.UserID, &p.Asset, &p.Chain, &p.Amount, &p.APY,
			&p.Duration, &p.StartedAt, &p.EndsAt, &p.Status, &p.Reward, &p.Accrued, &p.ClosedAt); err == nil {
			p.UserID = ""
			out = append(out, p)
		} else {
			log.Printf("staking position scan: %v", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"positions": out})
}

func (a *App) handleStakingUnlock(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	id := r.PathValue("id")
	var endsAt time.Time
	var status string
	err := a.db.QueryRow(r.Context(),
		`SELECT ends_at, status FROM stake_positions WHERE id=$1::uuid AND user_id=$2::uuid`, id, uid).
		Scan(&endsAt, &status)
	if err != nil {
		writeErr(w, http.StatusNotFound, "position not found")
		return
	}
	if status != "closed" && time.Now().Before(endsAt) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprintf(w, `{"error":"locked until maturity","unlock_at":%q}`, endsAt.Format(time.RFC3339))
		return
	}
	if status != "closed" {
		if _, err := a.db.Exec(r.Context(),
			`UPDATE stake_positions SET status='unlock_requested',
                                unlock_requested_at=COALESCE(unlock_requested_at, now())
                         WHERE id=$1 AND status='active'`, id); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to queue unlock")
			return
		}
	}
	settled, err := a.settleStakes(r.Context(), id)
	if err != nil {
		log.Printf("staking settle %s: %v", id, err)
		writeErr(w, http.StatusInternalServerError, "settlement failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": map[bool]string{true: "closed", false: "unlock_requested"}[settled]})
}

// ---- admin plane ----

func (a *App) handleAdminStakingAssets(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT sa.id, sa.asset, sa.chain, sa.apy::text, sa.min_amount::text,
                        sa.durations_days, sa.active, sa.created_at,
                        COALESCE(pt.name,''), COALESCE(pt.logo_url,''),
                        (SELECT COUNT(*) FROM stake_positions sp
                          WHERE sp.asset=sa.asset AND sp.chain=sa.chain AND sp.status !='closed')
                 FROM staking_assets sa
                 LEFT JOIN platform_tokens pt ON pt.symbol=sa.asset AND pt.chain=sa.chain
                 ORDER BY sa.asset, sa.chain`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load assets")
		return
	}
	defer rows.Close()
	type row struct {
		stakingAsset
		OpenPositions int `json:"open_positions"`
	}
	out := []row{}
	for rows.Next() {
		var r2 row
		if err := rows.Scan(&r2.ID, &r2.Asset, &r2.Chain, &r2.APY, &r2.MinAmount,
			&r2.Durations, &r2.Active, &r2.Created, &r2.Name, &r2.Logo, &r2.OpenPositions); err == nil {
			r2.PriceUSD = nil
			out = append(out, r2)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

func (a *App) handleAdminStakingAssetCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Asset     string  `json:"asset"`
		Chain     string  `json:"chain"`
		APY       string  `json:"apy"`
		MinAmount string  `json:"min_amount"`
		Durations []int32 `json:"durations_days"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	req.Chain = strings.ToLower(strings.TrimSpace(req.Chain))
	if req.Asset == "" || req.Chain == "" || req.APY == "" {
		writeErr(w, http.StatusBadRequest, "asset, chain and apy required")
		return
	}
	if req.Durations != nil && len(req.Durations) == 0 {
		writeErr(w, http.StatusBadRequest, "at least one duration required")
		return
	}
	if req.Durations == nil {
		req.Durations = []int32{7, 30, 90, 180, 365}
	}
	for _, d := range req.Durations {
		if d <= 0 || d > 3650 {
			writeErr(w, http.StatusBadRequest, "duration must be 1..3650 days")
			return
		}
	}
	uid := userIDFrom(r)
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO staking_assets (asset, chain, apy, min_amount, durations_days, created_by)
                 SELECT $1, $2, $3::numeric, COALESCE($4::numeric,0), $5, $6
                 FROM platform_tokens WHERE symbol=$1 AND chain=$2 AND enabled
                 ON CONFLICT (asset, chain) DO UPDATE SET
                   apy=$3::numeric, min_amount=COALESCE($4::numeric, staking_assets.min_amount),
                   durations_days=$5, active=true, updated_at=now()
                 RETURNING id`,
		req.Asset, req.Chain, req.APY, nilIfEmpty(req.MinAmount), req.Durations, uid).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "token not listed")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO staking_rates (asset_id, apy, set_by) VALUES ($1,$2::numeric,$3)`,
		id, req.APY, uid); err == nil {
	}
	a.audit(r.Context(), uid, "staking.asset_add", req.Asset+"/"+req.Chain, map[string]any{"apy": req.APY})
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleAdminStakingAssetUpdate(w http.ResponseWriter, r *http.Request) {
	var req stakingAssetPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	a.updateStakingAsset(w, r, r.PathValue("id"), &req)
}

func (a *App) handleAdminStakingAssetDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var n int
	if err := a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM stake_positions sp JOIN staking_assets sa
                  ON sp.asset=sa.asset AND sp.chain=sa.chain WHERE sa.id=$1`,
		id).Scan(&n); err == nil && n > 0 {
		writeErr(w, http.StatusConflict, "position history exists; deactivate instead")
		return
	}
	tag, err := a.db.Exec(r.Context(), `DELETE FROM staking_assets WHERE id=$1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "asset not found")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "staking.asset_delete", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *App) handleAdminStakingPositions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	query := `SELECT p.id, u.username, p.asset, p.chain, p.amount::text, p.apy::text,
                         p.duration_days, p.started_at, p.ends_at, p.status,
                         (p.amount * (p.apy/100.0) * (p.duration_days::numeric/365.0))::text, p.closed_at
                  FROM stake_positions p JOIN users u ON u.id=p.user_id`
	args := []any{}
	if status != "" {
		query += ` WHERE p.status=$1`
		args = append(args, status)
	}
	query += ` ORDER BY p.created_at DESC LIMIT 200`
	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load positions")
		return
	}
	defer rows.Close()
	out := []stakePosition{}
	for rows.Next() {
		var p stakePosition
		if err := rows.Scan(&p.ID, &p.Username, &p.Asset, &p.Chain, &p.Amount, &p.APY,
			&p.Duration, &p.StartedAt, &p.EndsAt, &p.Status, &p.Reward, &p.ClosedAt); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"positions": out})
}

func (a *App) handleAdminStakingQueue(w http.ResponseWriter, r *http.Request) {
	a.handleAdminStakingPositionsFiltered(w, r, "unlock_requested")
}

func (a *App) handleAdminStakingPositionsFiltered(w http.ResponseWriter, r *http.Request, status string) {
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, u.username, p.asset, p.chain, p.amount::text, p.apy::text,
                        p.duration_days, p.started_at, p.ends_at, p.status,
                        (p.amount * (p.apy/100.0) * (p.duration_days::numeric/365.0))::text, p.closed_at
                 FROM stake_positions p JOIN users u ON u.id=p.user_id
                 WHERE p.status=$1 ORDER BY p.unlock_requested_at NULLS LAST, p.created_at LIMIT 200`, status)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load queue")
		return
	}
	defer rows.Close()
	out := []stakePosition{}
	for rows.Next() {
		var p stakePosition
		if err := rows.Scan(&p.ID, &p.Username, &p.Asset, &p.Chain, &p.Amount, &p.APY,
			&p.Duration, &p.StartedAt, &p.EndsAt, &p.Status, &p.Reward, &p.ClosedAt); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"positions": out})
}

type treasuryMove struct {
	ID        string    `json:"id"`
	Admin     string    `json:"admin"`
	Asset     string    `json:"asset"`
	Chain     string    `json:"chain"`
	Amount    string    `json:"amount"`
	Direction string    `json:"direction"`
	Purpose   string    `json:"purpose"`
	Created   time.Time `json:"created_at"`
}

func (a *App) handleAdminStakingMovesList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT m.id, COALESCE(u.username,$1), m.asset, m.chain, m.amount::text,
                        m.direction, m.purpose, m.created_at
                 FROM staking_treasury_moves m LEFT JOIN users u ON u.id=m.admin_id
                 ORDER BY m.created_at DESC LIMIT 200`, "admin")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load moves")
		return
	}
	defer rows.Close()
	out := []treasuryMove{}
	for rows.Next() {
		var m treasuryMove
		if err := rows.Scan(&m.ID, &m.Admin, &m.Asset, &m.Chain, &m.Amount,
			&m.Direction, &m.Purpose, &m.Created); err == nil {
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"moves": out})
}

func (a *App) handleAdminStakingMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Direction string `json:"direction"`
		Asset     string `json:"asset"`
		Chain     string `json:"chain"`
		Amount    string `json:"amount"`
		Purpose   string `json:"purpose"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Direction = strings.ToLower(strings.TrimSpace(req.Direction))
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	req.Chain = strings.ToLower(strings.TrimSpace(req.Chain))
	if (req.Direction != "out" && req.Direction != "in") || req.Asset == "" || req.Chain == "" || req.Amount == "" {
		writeErr(w, http.StatusBadRequest, "direction in|out, asset, chain and amount required")
		return
	}
	uid := userIDFrom(r)
	acct, err := a.ensureAccount(r.Context(), platformTreasuryID, req.Asset, req.Chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "treasury account unavailable")
		return
	}
	kind := "staking_treasury_out"
	sign := "-"
	if req.Direction == "in" {
		kind = "staking_treasury_in"
		sign = ""
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`SELECT id FROM wallet_accounts WHERE id=$1 FOR UPDATE`, acct); err != nil {
		writeErr(w, http.StatusInternalServerError, "treasury lock failed")
		return
	}
	if req.Direction == "out" {
		var ok bool
		if err := tx.QueryRow(r.Context(),
			`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
                         FROM ledger_entries WHERE account_id=$2`,
			req.Amount, acct).Scan(&ok); err != nil || !ok {
			writeErr(w, http.StatusBadRequest, "treasury liquidity insufficient")
			return
		}
	}
	var txID, moveID string
	if err := tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&txID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to allocate tx id")
		return
	}
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO staking_treasury_moves (admin_id, asset, chain, amount, direction, purpose)
                 VALUES ($1,$2,$3,$4::numeric,$5,$6) RETURNING id`,
		uid, req.Asset, req.Chain, req.Amount, req.Direction, req.Purpose).Scan(&moveID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record move")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
                 VALUES ($1,$2, (`+sign+`$3::numeric), $4, $5)`,
		txID, acct, req.Amount, kind, "treasury "+req.Direction+": "+req.Purpose); err != nil {
		writeErr(w, http.StatusInternalServerError, "ledger write failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "commit failed")
		return
	}
	settled := 0
	if req.Direction == "in" {
		rows, err := a.db.Query(r.Context(),
			`SELECT id FROM stake_positions WHERE asset=$1 AND chain=$2
                          AND status='unlock_requested' ORDER BY unlock_requested_at`, req.Asset, req.Chain)
		if err == nil {
			defer rows.Close()
			ids := []string{}
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			for _, id := range ids {
				if ok, _ := a.settleStakes(r.Context(), id); ok {
					settled++
				} else {
					break
				}
			}
		}
	}
	a.audit(r.Context(), uid, "staking.treasury_"+req.Direction, req.Asset+"/"+req.Chain,
		map[string]any{"amount": req.Amount, "purpose": req.Purpose, "settled": settled})
	writeJSON(w, http.StatusCreated, map[string]any{"id": moveID, "settled": settled})
}

func (a *App) handleAdminStakingSettle(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	settled, err := a.settleStakes(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "position not found")
		return
	}
	if !settled {
		writeErr(w, http.StatusConflict, "treasury liquidity insufficient")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func nilIfEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// Position-settle from the admin dashboard: by position id or by
// (asset, chain) batch filter over the unlock queue.
func (a *App) handleAdminStakingSettleBy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PositionID string `json:"position_id"`
		Asset      string `json:"asset"`
		Chain      string `json:"chain"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.PositionID != "" {
		settled, err := a.settleStakes(r.Context(), req.PositionID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "position not found")
			return
		}
		if !settled {
			writeErr(w, http.StatusConflict, "treasury liquidity insufficient")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "closed"})
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT id FROM stake_positions
		 WHERE status='unlock_requested'
		   AND ($1::text='' OR asset=$1) AND ($2::text='' OR chain=$2)`,
		req.Asset, req.Chain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	failed := 0
	for _, id := range ids {
		settled, err := a.settleStakes(r.Context(), id)
		if err != nil || !settled {
			failed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settled":       len(ids) - failed,
		"pending_total": len(ids),
		"status":        "closed",
	})
}

// Aggregate staking totals for the admin audit panel.
func (a *App) handleAdminStakingAudit(w http.ResponseWriter, r *http.Request) {
	var active string
	var totalLocked string
	_ = a.db.QueryRow(r.Context(),
		`SELECT COUNT(*)::text,
		        COALESCE(SUM(p.amount * COALESCE(cp.price_usd, 0))::text, '0')
		 FROM stake_positions p
		 LEFT JOIN crypto_prices cp ON cp.asset=p.asset AND cp.chain=p.chain
		 WHERE p.status='active'`).Scan(&active, &totalLocked)
	writeJSON(w, http.StatusOK, map[string]any{
		"positions_active": atoi(active),
		"total_locked_usd": totalLocked,
	})
}

// Resolves a staking asset id from (asset, chain) for the
// dashboard-friendly update URL /api/admin/staking/assets/{asset}/{chain}.
func (a *App) handleAdminStakingAssetUpdateBy(w http.ResponseWriter, r *http.Request) {
	id, err := a.stakingAssetID(r.Context(), r.PathValue("asset"), r.PathValue("chain"))
	if err != nil {
		writeErr(w, http.StatusNotFound, "asset not found")
		return
	}
	var req stakingAssetPatch
	if !decodeJSON(w, r, &req) {
		return
	}
	a.updateStakingAsset(w, r, id, &req)
}

func (a *App) stakingAssetID(ctx context.Context, asset, chain string) (string, error) {
	var id string
	err := a.db.QueryRow(ctx, `SELECT id FROM staking_assets WHERE asset=$1 AND chain=$2`,
		asset, chain).Scan(&id)
	return id, err
}

type stakingAssetPatch struct {
	APY       *string `json:"apy"`
	MinAmount *string `json:"min_amount"`
	Durations []int32 `json:"durations_days"`
	Active    *bool   `json:"active"`
}

func (a *App) updateStakingAsset(w http.ResponseWriter, r *http.Request, id string, req *stakingAssetPatch) {
	uid := userIDFrom(r)
	sets := []string{}
	args := []any{id}
	add := func(clause string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}
	if req.APY != nil {
		add("apy=$%d::numeric", *req.APY)
	}
	if req.MinAmount != nil {
		add("min_amount=$%d::numeric", *req.MinAmount)
	}
	if len(req.Durations) != 0 {
		add("durations_days=$%d", req.Durations)
	}
	if req.Active != nil {
		add("active=$%d", *req.Active)
	}
	if len(sets) == 0 {
		writeErr(w, http.StatusBadRequest, "nothing to update")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE staking_assets SET `+strings.Join(sets, ", ")+`, updated_at=now() WHERE id=$1`,
		args...)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "asset not found or invalid value")
		return
	}
	if req.APY != nil {
		_, _ = a.db.Exec(r.Context(),
			`INSERT INTO staking_rates (asset_id, apy, set_by) VALUES ($1,$2::numeric,$3)`,
			id, *req.APY, uid)
	}
	a.audit(r.Context(), uid, "staking.asset_update", id, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
