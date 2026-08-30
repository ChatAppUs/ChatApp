package main

// Monetization depth: creator subscription tiers, recurring subscriptions,
// one-off tips, and a gift catalog. All value moves through the existing
// double-entry ledger in the internal USD asset (kind = subscription_payment,
// tip_send/tip_recv, gift_send/gift_recv) and is mirrored into
// creator_earnings so payouts work unchanged.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// moveUSD transfers amount USD between two users' internal USD wallet
// accounts atomically: balance check + debit + credit + earnings record in
// one transaction. sendKind/recvKind are the ledger entry kinds.
func (a *App) moveUSD(ctx context.Context, fromUser, toUser, amount, sendKind, recvKind, memo string) (string, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	fromAcct, err := a.ensureAccount(ctx, fromUser, "USD", "internal")
	if err != nil {
		return "", err
	}
	toAcct, err := a.ensureAccount(ctx, toUser, "USD", "internal")
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM wallet_accounts WHERE id=$1 FOR UPDATE`, fromAcct); err != nil {
		return "", err
	}
	var ok bool
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
		 FROM ledger_entries WHERE account_id=$2`, amount, fromAcct).Scan(&ok); err != nil || !ok {
		return "", pgx.ErrNoRows // insufficient balance
	}
	var txID string
	if err := tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&txID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2, -$3::numeric, $4, $5, $6),
		        ($1,$7,  $3::numeric, $8, $9, $6)`,
		txID, fromAcct, amount, sendKind, toUser, memo, toAcct, recvKind, fromUser); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return txID, nil
}

// creditCreatorEarnings records an income event for the creator dashboard.
func (a *App) creditCreatorEarnings(ctx context.Context, creatorID, source, amount, postID string) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO creator_earnings (creator_id, source, amount, currency, post_id)
		 VALUES ($1,$2,$3::numeric,'USD',NULLIF($4,'')::uuid)`,
		creatorID, source, amount, postID)
}

// ---- subscription tiers ----

func (a *App) handleCreateTier(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string  `json:"name"`
		Perks    string  `json:"perks"`
		PriceUSD float64 `json:"price_usd"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) < 2 || len(req.Name) > 60 {
		writeErr(w, http.StatusBadRequest, "tier name must be 2-60 characters")
		return
	}
	if req.PriceUSD <= 0 || req.PriceUSD > 10000 {
		writeErr(w, http.StatusBadRequest, "price must be between 0 and 10000 USD")
		return
	}
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO subscription_tiers (creator_id, name, perks, price_usd)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		userIDFrom(r), req.Name, req.Perks, req.PriceUSD).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "tier creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) listTiers(w http.ResponseWriter, r *http.Request, creatorID string) {
	rows, err := a.db.Query(r.Context(),
		`SELECT t.id, t.name, t.perks, t.price_usd, t.created_at,
		        (SELECT COUNT(*) FROM subscriptions s WHERE s.tier_id=t.id AND s.status='active')
		 FROM subscription_tiers t WHERE t.creator_id=$1 AND t.active ORDER BY t.price_usd`, creatorID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tiers")
		return
	}
	defer rows.Close()
	tiers := []map[string]any{}
	for rows.Next() {
		var id, name, perks string
		var price float64
		var createdAt time.Time
		var subs int64
		if err := rows.Scan(&id, &name, &perks, &price, &createdAt, &subs); err != nil {
			continue
		}
		tiers = append(tiers, map[string]any{
			"id": id, "name": name, "perks": perks, "price_usd": price,
			"created_at": createdAt, "subscriber_count": subs,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tiers": tiers})
}

func (a *App) handleListMyTiers(w http.ResponseWriter, r *http.Request) {
	a.listTiers(w, r, userIDFrom(r))
}

func (a *App) handleListCreatorTiers(w http.ResponseWriter, r *http.Request) {
	a.listTiers(w, r, r.PathValue("id"))
}

// handleDeleteTier soft-deactivates a tier; active subscriptions run out.
func (a *App) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`UPDATE subscription_tiers SET active=FALSE WHERE id=$1 AND creator_id=$2`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "tier not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

// ---- subscriptions ----

func (a *App) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	tierID := r.PathValue("id")
	uid := userIDFrom(r)
	var creatorID string
	var price float64
	var active bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT creator_id, price_usd, active FROM subscription_tiers WHERE id=$1`,
		tierID).Scan(&creatorID, &price, &active); err != nil || !active {
		writeErr(w, http.StatusNotFound, "tier not found")
		return
	}
	if creatorID == uid {
		writeErr(w, http.StatusBadRequest, "cannot subscribe to your own tier")
		return
	}
	var existing string
	var status string
	if err := a.db.QueryRow(r.Context(),
		`SELECT id, status FROM subscriptions WHERE tier_id=$1 AND subscriber_id=$2`,
		tierID, uid).Scan(&existing, &status); err == nil && status == "active" {
		writeErr(w, http.StatusConflict, "already subscribed")
		return
	}
	amount := strings.TrimSpace(formatMoney(price))
	txID, err := a.moveUSD(r.Context(), uid, creatorID, amount, "subscription_payment", "subscription_income", "tier "+tierID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	now := time.Now()
	if existing != "" {
		_, err = a.db.Exec(r.Context(),
			`UPDATE subscriptions SET status='active', current_period_start=$3,
			 current_period_end=$4, cancelled_at=NULL WHERE id=$1 AND subscriber_id=$2`,
			existing, uid, now, now.AddDate(0, 1, 0))
	} else {
		_, err = a.db.Exec(r.Context(),
			`INSERT INTO subscriptions (tier_id, subscriber_id, creator_id, current_period_start, current_period_end)
			 VALUES ($1,$2,$3,$4,$5)`, tierID, uid, creatorID, now, now.AddDate(0, 1, 0))
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "subscription failed")
		return
	}
	a.creditCreatorEarnings(r.Context(), creatorID, "subscription", amount, "")
	a.notify(r.Context(), creatorID, "new_subscriber", "New subscriber",
		"Someone subscribed to your tier", map[string]any{"tier_id": tierID, "tx_id": txID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "active", "tx_id": txID})
}

func (a *App) handleCancelSubscription(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`UPDATE subscriptions SET status='cancelled', cancelled_at=now()
		 WHERE id=$1 AND subscriber_id=$2 AND status='active'`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "active subscription not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (a *App) handleMySubscriptions(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT s.id, t.name, t.price_usd, u.username, u.display_name, s.status,
		        s.current_period_end, s.created_at
		 FROM subscriptions s
		 JOIN subscription_tiers t ON t.id=s.tier_id
		 JOIN users u ON u.id=s.creator_id
		 WHERE s.subscriber_id=$1 ORDER BY s.created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load subscriptions")
		return
	}
	defer rows.Close()
	subs := []map[string]any{}
	for rows.Next() {
		var id, name, uname, dname, status string
		var price float64
		var periodEnd, createdAt time.Time
		if err := rows.Scan(&id, &name, &price, &uname, &dname, &status, &periodEnd, &createdAt); err != nil {
			continue
		}
		subs = append(subs, map[string]any{
			"id": id, "tier_name": name, "price_usd": price, "creator_username": uname,
			"creator_display_name": dname, "status": status,
			"current_period_end": periodEnd, "created_at": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
}

func (a *App) handleCreatorSubscribers(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT s.id, u.username, u.display_name, u.avatar_url, t.name, t.price_usd,
		        s.status, s.current_period_end
		 FROM subscriptions s
		 JOIN users u ON u.id=s.subscriber_id
		 JOIN subscription_tiers t ON t.id=s.tier_id
		 WHERE s.creator_id=$1 ORDER BY s.created_at DESC LIMIT 500`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load subscribers")
		return
	}
	defer rows.Close()
	subs := []map[string]any{}
	for rows.Next() {
		var id, uname, dname, avatar, tierName, status string
		var price float64
		var periodEnd time.Time
		if err := rows.Scan(&id, &uname, &dname, &avatar, &tierName, &price, &status, &periodEnd); err != nil {
			continue
		}
		subs = append(subs, map[string]any{
			"id": id, "username": uname, "display_name": dname, "avatar_url": avatar,
			"tier_name": tierName, "price_usd": price, "status": status, "current_period_end": periodEnd,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribers": subs})
}

// ---- tips ----

func (a *App) handleSendTip(w http.ResponseWriter, r *http.Request) {
	toUser := r.PathValue("id")
	uid := userIDFrom(r)
	if toUser == uid {
		writeErr(w, http.StatusBadRequest, "cannot tip yourself")
		return
	}
	var req struct {
		AmountUSD float64 `json:"amount_usd"`
		PostID    string  `json:"post_id"`
		Message   string  `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AmountUSD <= 0 || req.AmountUSD > 10000 {
		writeErr(w, http.StatusBadRequest, "tip must be between 0 and 10000 USD")
		return
	}
	if len(req.Message) > 280 {
		writeErr(w, http.StatusBadRequest, "message too long")
		return
	}
	var targetActive bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT status='active' FROM users WHERE id=$1`, toUser).Scan(&targetActive); err != nil || !targetActive {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	amount := formatMoney(req.AmountUSD)
	txID, err := a.moveUSD(r.Context(), uid, toUser, amount, "tip_send", "tip_recv", req.Message)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO tips (from_user, to_user, amount_usd, post_id, message)
		 VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5)`, uid, toUser, req.AmountUSD, req.PostID, req.Message); err != nil {
		writeErr(w, http.StatusInternalServerError, "tip record failed")
		return
	}
	a.creditCreatorEarnings(r.Context(), toUser, "tips", amount, req.PostID)
	a.notify(r.Context(), toUser, "tip_received", "You received a tip",
		"Tip of $"+amount, map[string]any{"from": uid, "amount_usd": req.AmountUSD, "tx_id": txID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "sent", "tx_id": txID})
}

// ---- gifts ----

func (a *App) handleGiftCatalog(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, name, emoji, price_usd FROM gift_catalog WHERE active ORDER BY price_usd`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load gifts")
		return
	}
	defer rows.Close()
	gifts := []map[string]any{}
	for rows.Next() {
		var id, name, emoji string
		var price float64
		if err := rows.Scan(&id, &name, &emoji, &price); err != nil {
			continue
		}
		gifts = append(gifts, map[string]any{"id": id, "name": name, "emoji": emoji, "price_usd": price})
	}
	writeJSON(w, http.StatusOK, map[string]any{"gifts": gifts})
}

func (a *App) handleSendGift(w http.ResponseWriter, r *http.Request) {
	toUser := r.PathValue("id")
	uid := userIDFrom(r)
	if toUser == uid {
		writeErr(w, http.StatusBadRequest, "cannot gift yourself")
		return
	}
	var req struct {
		GiftID string `json:"gift_id"`
		PostID string `json:"post_id"`
	}
	if !decodeJSON(w, r, &req) || req.GiftID == "" {
		writeErr(w, http.StatusBadRequest, "gift_id required")
		return
	}
	var name, emoji string
	var price float64
	if err := a.db.QueryRow(r.Context(),
		`SELECT name, emoji, price_usd FROM gift_catalog WHERE id=$1 AND active`,
		req.GiftID).Scan(&name, &emoji, &price); err != nil {
		writeErr(w, http.StatusNotFound, "gift not found")
		return
	}
	amount := formatMoney(price)
	txID, err := a.moveUSD(r.Context(), uid, toUser, amount, "gift_send", "gift_recv", name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO gift_sends (gift_id, from_user, to_user, post_id, amount_usd)
		 VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5)`, req.GiftID, uid, toUser, req.PostID, price); err != nil {
		writeErr(w, http.StatusInternalServerError, "gift record failed")
		return
	}
	a.creditCreatorEarnings(r.Context(), toUser, "gifts", amount, req.PostID)
	a.notify(r.Context(), toUser, "gift_received", "You received a gift",
		emoji+" "+name, map[string]any{"from": uid, "gift": name, "emoji": emoji, "tx_id": txID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "sent", "tx_id": txID})
}

// formatMoney renders a float as a fixed 2-decimal string for NUMERIC params.
func formatMoney(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

// ---- subscription renewal worker ----

func (a *App) startSubscriptionWorker() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			a.renewDueSubscriptions()
		}
	}()
}

// renewDueSubscriptions charges every active subscription whose period ended.
// Rows are claimed FOR UPDATE SKIP LOCKED so multiple API nodes can run the
// worker concurrently; insufficient balance expires the subscription.
func (a *App) renewDueSubscriptions() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx,
		`SELECT s.id, s.subscriber_id, s.creator_id, t.price_usd
		 FROM subscriptions s JOIN subscription_tiers t ON t.id=s.tier_id
		 WHERE s.status='active' AND s.current_period_end <= now() AND t.active
		 ORDER BY s.current_period_end LIMIT 100 FOR UPDATE OF s SKIP LOCKED`)
	if err != nil {
		return
	}
	type due struct {
		id, subscriberID, creatorID string
		price                       float64
	}
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.subscriberID, &d.creatorID, &d.price); err == nil {
			dues = append(dues, d)
		}
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil || len(dues) == 0 {
		return
	}
	for _, d := range dues {
		amount := formatMoney(d.price)
		txID, err := a.moveUSD(ctx, d.subscriberID, d.creatorID, amount,
			"subscription_payment", "subscription_income", "renewal "+d.id)
		if err != nil {
			_, _ = a.db.Exec(ctx,
				`UPDATE subscriptions SET status='expired' WHERE id=$1 AND status='active'`, d.id)
			a.notify(ctx, d.subscriberID, "subscription_expired", "Subscription expired",
				"Insufficient balance to renew", map[string]any{"subscription_id": d.id})
			continue
		}
		_, _ = a.db.Exec(ctx,
			`UPDATE subscriptions SET current_period_start=now(),
			 current_period_end=now()+interval '1 month' WHERE id=$1`, d.id)
		a.creditCreatorEarnings(ctx, d.creatorID, "subscription", amount, "")
		a.notify(ctx, d.creatorID, "subscription_renewed", "Subscription renewed",
			"A subscriber renewed", map[string]any{"subscription_id": d.id, "tx_id": txID})
	}
}
