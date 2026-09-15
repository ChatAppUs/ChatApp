package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Live Shopping — master plan §20. A live room pins products, buyers check
// out for real: inventory is decremented under a row lock and funds move on
// the double-entry ledger (buyer -> seller, platform fee -> treasury) in the
// same transaction that records the order. Coupons apply a real discount and
// are consumed on use. Analytics are computed from the order rows.

const liveShopFeePct = "5.00"

type liveProductJSON struct {
	ID          string    `json:"id"`
	RoomID      string    `json:"room_id"`
	SellerID    string    `json:"seller_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	PriceUSD    string    `json:"price_usd"`
	Inventory   int       `json:"inventory"`
	DiscountPct string    `json:"discount_pct"`
	ImageURL    string    `json:"image_url"`
	Active      bool      `json:"active"`
	Pinned      bool      `json:"pinned"`
	CreatedAt   time.Time `json:"created_at"`
}

const liveProductSelect = `
 SELECT p.id, p.room_id, p.seller_id, p.title, p.description,
        p.price_usd::text, p.inventory, p.discount_pct::text, p.image_url,
        p.active, p.created_at,
        EXISTS (SELECT 1 FROM live_product_pins pin
                 WHERE pin.product_id = p.id AND pin.unpinned_at IS NULL)
   FROM live_products p `

func scanLiveProducts(rows interface {
	Next() bool
	Scan(...any) error
	Close()
}) []liveProductJSON {
	out := []liveProductJSON{}
	for rows.Next() {
		var p liveProductJSON
		if err := rows.Scan(&p.ID, &p.RoomID, &p.SellerID, &p.Title, &p.Description,
			&p.PriceUSD, &p.Inventory, &p.DiscountPct, &p.ImageURL, &p.Active,
			&p.CreatedAt, &p.Pinned); err == nil {
			out = append(out, p)
		}
	}
	rows.Close()
	return out
}

// POST /api/live-rooms/{roomId}/products — a seller lists a product.
func (a *App) handleLiveProductCreate(w http.ResponseWriter, r *http.Request) {
	roomID, ok := requireUUIDPath(w, r, "roomId")
	if !ok {
		return
	}
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		PriceUSD    string `json:"price_usd"`
		Inventory   int    `json:"inventory"`
		DiscountPct string `json:"discount_pct"`
		ImageURL    string `json:"image_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 {
		writeErr(w, http.StatusBadRequest, "title required (max 200 chars)")
		return
	}
	price := strings.TrimSpace(req.PriceUSD)
	if price == "" {
		writeErr(w, http.StatusBadRequest, "price_usd required")
		return
	}
	if req.Inventory < 0 || req.Inventory > 1_000_000 {
		writeErr(w, http.StatusBadRequest, "inventory must be 0..1000000")
		return
	}
	disc := strings.TrimSpace(req.DiscountPct)
	if disc == "" {
		disc = "0"
	}
	if len(req.Description) > 4000 || len(req.ImageURL) > 2048 {
		writeErr(w, http.StatusBadRequest, "field too long")
		return
	}
	uid := userIDFrom(r)
	// The price must be a positive numeric; validate against Postgres rather
	// than trusting a client-supplied float.
	var priceOK bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT $1::numeric > 0 AND $2::numeric >= 0 AND $2::numeric <= 100`,
		price, disc).Scan(&priceOK); err != nil || !priceOK {
		writeErr(w, http.StatusBadRequest, "price_usd must be > 0 and discount_pct 0..100")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO live_products (room_id, seller_id, title, description,
		        price_usd, inventory, discount_pct, image_url)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6,$7::numeric,$8) RETURNING id`,
		roomID, uid, title, strings.TrimSpace(req.Description), price,
		req.Inventory, disc, strings.TrimSpace(req.ImageURL)).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "product creation failed")
		return
	}
	a.audit(r.Context(), uid, "liveshop.product.create", id,
		map[string]any{"room_id": roomID, "price_usd": price})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// GET /api/live-rooms/{roomId}/products — product carousel for a room.
func (a *App) handleLiveProductList(w http.ResponseWriter, r *http.Request) {
	roomID, ok := requireUUIDPath(w, r, "roomId")
	if !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), liveProductSelect+
		` WHERE p.room_id = $1 AND p.active = TRUE
		  ORDER BY p.created_at DESC LIMIT 100`, roomID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load products")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"products": scanLiveProducts(rows)})
}

// POST /api/live-rooms/{roomId}/pin — pin a product (seller/room owner only).
func (a *App) handleLiveProductPin(w http.ResponseWriter, r *http.Request) {
	roomID, ok := requireUUIDPath(w, r, "roomId")
	if !ok {
		return
	}
	var req struct {
		ProductID string `json:"product_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	// Only the product's seller may pin it — server-side authorization.
	var seller string
	if err := a.db.QueryRow(r.Context(),
		`SELECT seller_id FROM live_products WHERE id=$1 AND room_id=$2`,
		req.ProductID, roomID).Scan(&seller); err != nil {
		writeErr(w, http.StatusNotFound, "product not found in this room")
		return
	}
	if seller != uid {
		writeErr(w, http.StatusForbidden, "only the seller can pin this product")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "pin failed")
		return
	}
	defer tx.Rollback(r.Context())
	// One active pin per room: unpin whatever was pinned.
	if _, err := tx.Exec(r.Context(),
		`UPDATE live_product_pins SET unpinned_at = now()
		  WHERE room_id = $1 AND unpinned_at IS NULL`, roomID); err != nil {
		writeErr(w, http.StatusInternalServerError, "pin failed")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO live_product_pins (room_id, product_id, pinned_by)
		 VALUES ($1,$2,$3)`, roomID, req.ProductID, uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "pin failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "pin failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "pinned", "room_id": roomID, "product_id": req.ProductID,
	})
}

// DELETE /api/live-rooms/{roomId}/pin — unpin the current product.
func (a *App) handleLiveProductUnpin(w http.ResponseWriter, r *http.Request) {
	roomID, ok := requireUUIDPath(w, r, "roomId")
	if !ok {
		return
	}
	uid := userIDFrom(r)
	res, err := a.db.Exec(r.Context(),
		`UPDATE live_product_pins pin SET unpinned_at = now()
		  WHERE pin.room_id = $1 AND pin.unpinned_at IS NULL
		    AND EXISTS (SELECT 1 FROM live_products lp
		                 WHERE lp.id = pin.product_id AND lp.seller_id = $2)`,
		roomID, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "unpin failed")
		return
	}
	if res.RowsAffected() == 0 {
		writeErr(w, http.StatusForbidden, "nothing pinned by you in this room")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unpinned"})
}

// POST /api/live-rooms/{roomId}/coupons — seller mints a discount coupon.
func (a *App) handleLiveCouponCreate(w http.ResponseWriter, r *http.Request) {
	roomID, ok := requireUUIDPath(w, r, "roomId")
	if !ok {
		return
	}
	var req struct {
		Code        string `json:"code"`
		DiscountPct string `json:"discount_pct"`
		MaxUses     int    `json:"max_uses"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	if code == "" || len(code) > 32 {
		writeErr(w, http.StatusBadRequest, "code required (max 32 chars)")
		return
	}
	if req.MaxUses < 0 {
		writeErr(w, http.StatusBadRequest, "max_uses must be >= 0")
		return
	}
	disc := strings.TrimSpace(req.DiscountPct)
	var discOK bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT $1::numeric > 0 AND $1::numeric <= 100`, disc).Scan(&discOK); err != nil || !discOK {
		writeErr(w, http.StatusBadRequest, "discount_pct must be 1..100")
		return
	}
	uid := userIDFrom(r)
	// Only a seller with a product in this room may mint coupons.
	var allowed bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM live_products WHERE room_id=$1 AND seller_id=$2)`,
		roomID, uid).Scan(&allowed)
	if !allowed {
		writeErr(w, http.StatusForbidden, "only a seller in this room can create coupons")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO live_coupons (room_id, code, discount_pct, max_uses, created_by)
		 VALUES ($1,$2,$3::numeric,$4,$5) RETURNING id`,
		roomID, code, disc, req.MaxUses, uid).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			writeErr(w, http.StatusConflict, "coupon code already exists in this room")
			return
		}
		writeErr(w, http.StatusBadRequest, "coupon creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "code": code})
}

// POST /api/live-rooms/{roomId}/checkout — buy a product.
func (a *App) handleLiveCheckout(w http.ResponseWriter, r *http.Request) {
	roomID, ok := requireUUIDPath(w, r, "roomId")
	if !ok {
		return
	}
	var req struct {
		ProductID string `json:"product_id"`
		Quantity  int    `json:"quantity"`
		Coupon    string `json:"coupon"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Quantity < 1 || req.Quantity > 1000 {
		writeErr(w, http.StatusBadRequest, "quantity must be 1..1000")
		return
	}
	uid := userIDFrom(r)

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	defer tx.Rollback(r.Context())

	// Lock the product row so concurrent checkouts cannot oversell inventory.
	var seller string
	var price, discountPct string
	var inventory int
	var active bool
	err = tx.QueryRow(r.Context(),
		`SELECT seller_id, price_usd::text, inventory, discount_pct::text, active
		   FROM live_products
		  WHERE id=$1 AND room_id=$2 FOR UPDATE`, req.ProductID, roomID).
		Scan(&seller, &price, &inventory, &discountPct, &active)
	if err != nil {
		writeErr(w, http.StatusNotFound, "product not found in this room")
		return
	}
	if !active {
		writeErr(w, http.StatusConflict, "product is no longer available")
		return
	}
	if seller == uid {
		writeErr(w, http.StatusBadRequest, "you cannot buy your own product")
		return
	}
	if inventory < req.Quantity {
		writeErr(w, http.StatusConflict,
			fmt.Sprintf("insufficient inventory: %d remaining", inventory))
		return
	}

	// Apply the product discount, then any coupon discount, entirely in SQL
	// numeric arithmetic (no float drift).
	var unit string
	if err := tx.QueryRow(r.Context(),
		`SELECT round($1::numeric * (1 - $2::numeric/100), 18)::text`, price, discountPct).
		Scan(&unit); err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	// Apply the coupon in the same locked transaction: claiming a use and
	// reading its percentage happen atomically, so a coupon cannot be
	// over-redeemed by concurrent checkouts.
	finalUnit := unit
	coupon := strings.ToUpper(strings.TrimSpace(req.Coupon))
	if coupon != "" {
		var coupPct string
		if err := tx.QueryRow(r.Context(),
			`UPDATE live_coupons
			    SET uses = uses + 1
			  WHERE room_id=$1 AND code=$2 AND active=TRUE
			    AND (max_uses = 0 OR uses < max_uses)
			  RETURNING discount_pct::text`, roomID, coupon).Scan(&coupPct); err != nil {
			writeErr(w, http.StatusBadRequest, "coupon invalid or exhausted")
			return
		}
		if err := tx.QueryRow(r.Context(),
			`SELECT round($1::numeric * (1 - $2::numeric/100), 18)::text`, unit, coupPct).
			Scan(&finalUnit); err != nil {
			writeErr(w, http.StatusInternalServerError, "checkout failed")
			return
		}
	}

	var subtotal, total, lineDiscount, fee string
	if err := tx.QueryRow(r.Context(),
		`SELECT round($1::numeric * $2, 18)::text,
		        round($1::numeric * $2, 18)::text,
		        round(($3::numeric - $1::numeric) * $2, 18)::text,
		        round(($1::numeric * $2) * $4::numeric / 100, 18)::text`,
		finalUnit, req.Quantity, price, liveShopFeePct).
		Scan(&subtotal, &total, &lineDiscount, &fee); err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}

	buyerAcct, err := a.ensureAccount(r.Context(), uid, "USD", "internal")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	// Lock the buyer's account and verify balance before moving funds.
	if _, err := tx.Exec(r.Context(),
		`SELECT id FROM wallet_accounts WHERE id=$1 FOR UPDATE`, buyerAcct); err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	var solvent bool
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(amount),0) >= $1::numeric AND $1::numeric > 0
		   FROM ledger_entries WHERE account_id=$2`, total, buyerAcct).Scan(&solvent); err != nil || !solvent {
		writeErr(w, http.StatusBadRequest, "insufficient USD balance; top up your wallet first")
		return
	}
	sellerAcct, err := a.ensureAccount(r.Context(), seller, "USD", "internal")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	treasuryAcct, err := a.ensureAccount(r.Context(), platformTreasuryID, "USD", "internal")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	var netToSeller, txID string
	if err := tx.QueryRow(r.Context(),
		`SELECT round($1::numeric - $2::numeric, 18)::text, gen_random_uuid()`,
		total, fee).Scan(&netToSeller, &txID); err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	// Buyer -total; seller +net; treasury +fee. The three entries sum to zero.
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO ledger_entries (tx_id, account_id, amount, kind, counterparty, memo)
		 VALUES ($1,$2, -$3::numeric, 'liveshop_purchase', $4, 'live shopping order'),
		        ($1,$5,  $6::numeric, 'liveshop_sale',     $7, 'live shopping order'),
		        ($1,$8,  $9::numeric, 'liveshop_fee',      $7, 'live shopping platform fee')`,
		txID, buyerAcct, total, req.ProductID, sellerAcct, netToSeller, uid,
		treasuryAcct, fee); err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	// Decrement inventory inside the same locked transaction.
	res, err := tx.Exec(r.Context(),
		`UPDATE live_products SET inventory = inventory - $2
		  WHERE id=$1 AND inventory >= $2`, req.ProductID, req.Quantity)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, "insufficient inventory")
		return
	}
	var orderID string
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO live_orders (product_id, room_id, buyer_id, seller_id,
		        quantity, unit_price_usd, discount_usd, coupon_code, total_usd,
		        platform_fee_usd, ledger_tx)
		 VALUES ($1,$2,$3,$4,$5,$6::numeric,$7::numeric,$8,$9::numeric,$10::numeric,$11)
		 RETURNING id`,
		req.ProductID, roomID, uid, seller, req.Quantity, price, lineDiscount,
		coupon, total, fee, txID).Scan(&orderID); err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "checkout failed")
		return
	}
	a.notifyKind(seller, "liveshop_order", map[string]any{"order_id": orderID, "product_id": req.ProductID,
		"quantity": req.Quantity, "net_usd": netToSeller})
	a.audit(r.Context(), uid, "liveshop.checkout", orderID,
		map[string]any{"product_id": req.ProductID, "total_usd": total, "fee_usd": fee})
	writeJSON(w, http.StatusCreated, map[string]any{
		"order_id": orderID, "total_usd": total, "platform_fee_usd": fee,
		"net_to_seller_usd": netToSeller, "ledger_tx": txID,
		"coupon": coupon, "quantity": req.Quantity,
	})
}

// GET /api/live-rooms/{roomId}/live-purchases — live purchase analytics.
func (a *App) handleLivePurchaseAnalytics(w http.ResponseWriter, r *http.Request) {
	roomID, ok := requireUUIDPath(w, r, "roomId")
	if !ok {
		return
	}
	var orders, units int
	var gross, fees string
	err := a.db.QueryRow(r.Context(),
		`SELECT COUNT(*), COALESCE(SUM(quantity),0),
		        COALESCE(SUM(total_usd),0)::text,
		        COALESCE(SUM(platform_fee_usd),0)::text
		   FROM live_orders WHERE room_id=$1 AND status='paid'`, roomID).
		Scan(&orders, &units, &gross, &fees)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT p.id, p.title, COALESCE(SUM(o.quantity),0) AS units,
		        COALESCE(SUM(o.total_usd),0)::text AS revenue
		   FROM live_products p
		   LEFT JOIN live_orders o ON o.product_id = p.id AND o.status='paid'
		  WHERE p.room_id = $1
		  GROUP BY p.id, p.title ORDER BY units DESC LIMIT 50`, roomID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load analytics")
		return
	}
	defer rows.Close()
	perProduct := []map[string]any{}
	for rows.Next() {
		var id, title, revenue string
		var unitsSold int
		if err := rows.Scan(&id, &title, &unitsSold, &revenue); err == nil {
			perProduct = append(perProduct, map[string]any{
				"product_id": id, "title": title, "units": unitsSold, "revenue_usd": revenue,
			})
		}
	}
	// Best-selling coupon codes in this room.
	couponRows, _ := a.db.Query(r.Context(),
		`SELECT COALESCE(NULLIF(coupon_code,''),'(none)') AS code, COUNT(*) AS uses,
		        COALESCE(SUM(discount_usd),0)::text
		   FROM live_orders WHERE room_id=$1 AND status='paid'
		  GROUP BY code ORDER BY uses DESC LIMIT 20`, roomID)
	coupons := []map[string]any{}
	if couponRows != nil {
		defer couponRows.Close()
		for couponRows.Next() {
			var code, saved string
			var uses int
			if err := couponRows.Scan(&code, &uses, &saved); err == nil {
				coupons = append(coupons, map[string]any{
					"code": code, "uses": uses, "discount_given_usd": saved,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id": roomID, "orders": orders, "units": units,
		"gross_usd": gross, "platform_fees_usd": fees,
		"per_product": perProduct, "coupons": coupons,
	})
}
