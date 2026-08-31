package main

import (
	"net/http"
	"strings"
	"time"
)

func (a *App) handleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string   `json:"name"`
		Objective       string   `json:"objective"`
		DailyBudget     string   `json:"daily_budget"`
		TotalBudget     string   `json:"total_budget"`
		Currency        string   `json:"currency"`
		TargetCountries []string `json:"target_countries"`
		TargetLocales   []string `json:"target_locales"`
		AgeMin          int      `json:"age_min"`
		AgeMax          int      `json:"age_max"`
		StartsAt        string   `json:"starts_at"`
		EndsAt          string   `json:"ends_at"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "campaign name required")
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if req.AgeMin == 0 {
		req.AgeMin = 18
	}
	if req.AgeMax == 0 {
		req.AgeMax = 65
	}
	if req.AgeMin < 13 || req.AgeMax > 100 || req.AgeMin > req.AgeMax {
		writeErr(w, http.StatusBadRequest, "invalid age targeting range")
		return
	}
	var startsAt, endsAt *time.Time
	if req.StartsAt != "" {
		if t, err := time.Parse(time.RFC3339, req.StartsAt); err == nil {
			startsAt = &t
		}
	}
	if req.EndsAt != "" {
		if t, err := time.Parse(time.RFC3339, req.EndsAt); err == nil {
			endsAt = &t
		}
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO ad_campaigns (advertiser_id, name, objective, daily_budget, total_budget, currency,
		 target_countries, target_locales, target_age_min, target_age_max, starts_at, ends_at)
		 VALUES ($1,$2,COALESCE(NULLIF($3,''),'reach'),$4::numeric,$5::numeric,$6,$7,$8,$9,$10,$11,$12)
		 RETURNING id`,
		userIDFrom(r), req.Name, req.Objective, req.DailyBudget, req.TotalBudget, req.Currency,
		req.TargetCountries, req.TargetLocales, req.AgeMin, req.AgeMax, startsAt, endsAt).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create campaign")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "draft"})
}

func (a *App) handleListCampaigns(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, name, objective, status, daily_budget, total_budget, spent, currency,
		        target_countries, target_locales, created_at
		 FROM ad_campaigns WHERE advertiser_id = $1 ORDER BY created_at DESC`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load campaigns")
		return
	}
	defer rows.Close()
	type camp struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Objective string    `json:"objective"`
		Status    string    `json:"status"`
		Daily     string    `json:"daily_budget"`
		Total     string    `json:"total_budget"`
		Spent     string    `json:"spent"`
		Currency  string    `json:"currency"`
		Countries []string  `json:"target_countries"`
		Locales   []string  `json:"target_locales"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []camp{}
	for rows.Next() {
		var c camp
		if err := rows.Scan(&c.ID, &c.Name, &c.Objective, &c.Status, &c.Daily, &c.Total, &c.Spent,
			&c.Currency, &c.Countries, &c.Locales, &c.CreatedAt); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"campaigns": out})
}

func (a *App) handleAddCreative(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		MediaURL string `json:"media_url"`
		CTAURL   string `json:"cta_url"`
	}
	if !decodeJSON(w, r, &req) || strings.TrimSpace(req.Title) == "" {
		writeErr(w, http.StatusBadRequest, "creative title required")
		return
	}
	campaignID := r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT advertiser_id FROM ad_campaigns WHERE id=$1`, campaignID).Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, "campaign not found")
		return
	}
	if owner != userIDFrom(r) {
		writeErr(w, http.StatusForbidden, "not your campaign")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO ad_creatives (campaign_id, title, body, media_url, cta_url)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		campaignID, req.Title, req.Body, req.MediaURL, req.CTAURL).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add creative")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleSubmitCampaign(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE ad_campaigns SET status='pending_review'
		 WHERE id=$1 AND advertiser_id=$2 AND status IN ('draft','rejected')`,
		r.PathValue("id"), userIDFrom(r))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "campaign not found or not editable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "pending_review"})
}

// Serve an ad targeted to the viewer's country/locale. An impression is
// recorded and the advertiser's USD ledger is charged CPM-style per view.
// adCreatorShare is the fraction of each attributed impression's cost paid
// to the creator whose content the ad ran against (in-stream rev share).
const adCreatorShare = "0.55"

// platformTreasuryID is the platform treasury user (migration 019).
const platformTreasuryID = "00000000-0000-0000-0000-000000000000"

func (a *App) handleServeAd(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	country := strings.ToUpper(r.URL.Query().Get("country"))
	locale := r.URL.Query().Get("locale")
	placementPostID := r.URL.Query().Get("placement_post_id")
	var creativeID, campaignID, title, body, mediaURL, ctaURL string
	err := a.db.QueryRow(r.Context(),
		`SELECT cr.id, c.id, cr.title, cr.body, cr.media_url, cr.cta_url
		 FROM ad_creatives cr JOIN ad_campaigns c ON c.id = cr.campaign_id
		 WHERE c.status = 'active'
		   AND (c.starts_at IS NULL OR c.starts_at <= now())
		   AND (c.ends_at IS NULL OR c.ends_at >= now())
		   AND c.spent < c.total_budget
		   AND (COALESCE(cardinality(c.target_countries), 0) = 0 OR $1 = ANY(c.target_countries))
		   AND (COALESCE(cardinality(c.target_locales), 0) = 0 OR $2 = ANY(c.target_locales))
		 ORDER BY random() LIMIT 1`, country, locale).
		Scan(&creativeID, &campaignID, &title, &body, &mediaURL, &ctaURL)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ad": nil})
		return
	}
	const cpmCost = "0.005" // $5 CPM

	// Resolve the placement creator before opening the tx (account lookups
	// use the pool; doing them inside the tx risks self-deadlock).
	creatorID, creatorAcct, treasuryAcct := "", "", ""
	if placementPostID != "" {
		_ = a.db.QueryRow(r.Context(),
			`SELECT author_id::text FROM posts WHERE id=$1::uuid`, placementPostID).Scan(&creatorID)
	}
	if creatorID != "" {
		treasuryAcct, _ = a.ensureAccount(r.Context(), platformTreasuryID, "USD", "internal")
		creatorAcct, _ = a.ensureAccount(r.Context(), creatorID, "USD", "internal")
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ad serving failed")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ad_events (creative_id, user_id, kind, cost, placement_post_id)
		 VALUES ($1,$2,'impression',$3::numeric, NULLIF($4,'')::uuid)`,
		creativeID, uid, cpmCost, placementPostID); err != nil {
		writeErr(w, http.StatusInternalServerError, "ad serving failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE ad_campaigns SET spent = spent + $1::numeric WHERE id=$2`, cpmCost, campaignID); err != nil {
		writeErr(w, http.StatusInternalServerError, "ad serving failed")
		return
	}
	// True rev-share accounting: the creator's cut of this impression moves
	// from the platform treasury to the creator in the same transaction, so
	// an impression, the budget decrement and the payout never disagree.
	if creatorAcct != "" && treasuryAcct != "" {
		var funded bool
		if err := tx.QueryRow(r.Context(),
			`SELECT COALESCE(SUM(amount),0) >= ($1::numeric * $2::numeric)
			 FROM ledger_entries WHERE account_id=$3`,
			cpmCost, adCreatorShare, treasuryAcct).Scan(&funded); err == nil && funded {
			var shareTx string
			_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&shareTx)
			if _, err := tx.Exec(r.Context(),
				`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
				 VALUES ($1,$2, -($3::numeric * $4::numeric), 'ad_share_send', $5, $6),
				        ($1,$7,  ($3::numeric * $4::numeric), 'ad_share_recv', $8, $6)`,
				shareTx, treasuryAcct, cpmCost, adCreatorShare, creatorID,
				"placement "+placementPostID, creatorAcct, platformTreasuryID); err == nil {
				if _, err := tx.Exec(r.Context(),
					`INSERT INTO creator_earnings (creator_id, source, amount, currency, post_id)
					 VALUES ($1,'ad_share', ($2::numeric * $3::numeric), 'USD', $4::uuid)`,
					creatorID, cpmCost, adCreatorShare, placementPostID); err != nil {
					writeErr(w, http.StatusInternalServerError, "ad serving failed")
					return
				}
			}
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "ad serving failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ad": map[string]string{
		"creative_id": creativeID, "title": title, "body": body,
		"media_url": mediaURL, "cta_url": ctaURL,
	}})
}

func (a *App) handleAdClick(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO ad_events (creative_id, user_id, kind) VALUES ($1,$2,'click')`,
		r.PathValue("id"), userIDFrom(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

// Fund a campaign from the advertiser's internal USD wallet balance.
func (a *App) handleFundCampaign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount string `json:"amount"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid, campaignID := userIDFrom(r), r.PathValue("id")
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT advertiser_id FROM ad_campaigns WHERE id=$1`, campaignID).Scan(&owner); err != nil || owner != uid {
		writeErr(w, http.StatusNotFound, "campaign not found")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "funding failed")
		return
	}
	defer tx.Rollback(r.Context())
	var acctID string
	err = tx.QueryRow(r.Context(),
		`SELECT id FROM wallet_accounts WHERE user_id=$1 AND asset='USD' AND chain='internal' FOR UPDATE`, uid).Scan(&acctID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "no USD wallet; create one first")
		return
	}
	var ok bool
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
		 FROM ledger_entries WHERE account_id=$2`, req.Amount, acctID).Scan(&ok); err != nil || !ok {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance or invalid amount")
		return
	}
	var txID string
	_ = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()`).Scan(&txID)
	treasuryAcct, err := a.ensureAccount(r.Context(), platformTreasuryID, "USD", "internal")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "funding failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
		 VALUES ($1,$2, -$3::numeric, 'ad_spend', $4),
		        ($1,$5,  $3::numeric, 'ad_credit', $4)`,
		txID, acctID, req.Amount, "campaign "+campaignID, treasuryAcct); err != nil {
		writeErr(w, http.StatusInternalServerError, "funding failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE ad_campaigns SET total_budget = total_budget + $1::numeric WHERE id=$2`,
		req.Amount, campaignID); err != nil {
		writeErr(w, http.StatusInternalServerError, "funding failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "funding failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "funded"})
}
