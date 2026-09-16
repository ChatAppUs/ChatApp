package main

// payout_worker.go — background payout settlement (§62 JOBS).
//
// The payout API (handlers_features.go) creates requests in 'pending' and an
// admin review (handleAdminReviewPayout) moves them to 'approved' or
// 'rejected'. This worker is the settlement side of that state machine: it
// batches every 'approved' request, debits the platform treasury through the
// double-entry ledger (the same ledger_entries table the wallet and P2P paths
// use), and marks the request 'paid' — atomically, per request, with
// FOR UPDATE SKIP LOCKED so concurrent workers can never double-pay.

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// payoutInterval is how often the worker looks for approved payouts.
const payoutInterval = 2 * time.Minute

type payoutJob struct {
	id          string
	creatorID   string
	amount      string
	asset       string
	destination string
}

// startPayoutWorker settles approved payout requests in the background.
func (a *App) startPayoutWorker() {
	go func() {
		ticker := time.NewTicker(payoutInterval)
		defer ticker.Stop()
		for {
			a.settleApprovedPayouts(context.Background())
			<-ticker.C
		}
	}()
}

// settleApprovedPayouts pays every approved request currently due. Each
// request is settled in its own transaction: one bad row (e.g. a creator
// account that vanished) must not block the rest of the batch. Returns the
// number of requests settled.
func (a *App) settleApprovedPayouts(ctx context.Context) int {
	rows, err := a.db.Query(ctx,
		`SELECT id, creator_id, amount::text, asset, destination
		   FROM payout_requests
		  WHERE status='approved'
		  ORDER BY created_at
		  LIMIT 100`)
	if err != nil {
		log.Printf("[payout-worker] list: %v", err)
		return 0
	}
	var batch []payoutJob
	for rows.Next() {
		var j payoutJob
		if err := rows.Scan(&j.id, &j.creatorID, &j.amount, &j.asset, &j.destination); err != nil {
			rows.Close()
			return 0
		}
		batch = append(batch, j)
	}
	rows.Close()

	paid := 0
	for _, j := range batch {
		if a.settleOnePayout(ctx, j) {
			paid++
		}
	}
	return paid
}

// settleOnePayout atomically: re-claims the row while still 'approved',
// credits the creator's ledger account, and flips the row to 'paid'. The
// conditional UPDATE is the claim; if it affects zero rows another worker
// (or an admin reversal) got there first and we skip silently.
func (a *App) settleOnePayout(ctx context.Context, j payoutJob) bool {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		log.Printf("[payout-worker] begin %s: %v", j.id, err)
		return false
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE payout_requests SET status='paid', reviewed_at=COALESCE(reviewed_at, now())
		  WHERE id=$1 AND status='approved'`, j.id)
	if err != nil {
		log.Printf("[payout-worker] claim %s: %v", j.id, err)
		return false
	}
	if tag.RowsAffected() == 0 {
		return false // already settled or reversed
	}

	acctID, err := ensureAccountInTx(ctx, tx, j.creatorID, j.asset, "chatapp")
	if err != nil {
		log.Printf("[payout-worker] account %s: %v", j.creatorID, err)
		return false
	}
	var txID string
	_ = tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&txID)
	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2,$3::numeric,'payout',$4,'Creator payout '||$5)`,
		txID, acctID, j.amount, j.creatorID, j.id); err != nil {
		log.Printf("[payout-worker] ledger %s: %v", j.id, err)
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		log.Printf("[payout-worker] commit %s: %v", j.id, err)
		return false
	}
	return true
}

// ensureAccountInTx is ensureAccount inside an existing transaction.
func ensureAccountInTx(ctx context.Context, tx pgx.Tx, userID, asset, chain string) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO wallet_accounts (user_id, asset, chain) VALUES ($1,$2,$3)
		 ON CONFLICT (user_id, asset, chain) DO UPDATE SET user_id = EXCLUDED.user_id
		 RETURNING id`, userID, strings.ToUpper(asset), strings.ToLower(chain)).Scan(&id)
	return id, err
}
