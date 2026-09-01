#!/usr/bin/env python3
"""End-to-end checks for the staking plane (migration 022):
admin-managed asset APY + durations, principal lock into treasury, maturity
gating (no unlock before ends_at), simple-interest reward settlement,
superadmin treasury in/out moves, queued unlock settlement, live prices
(admin override + order-book fallback + coingecko_id plumbing).

Runs against a live API on :8080 with all migrations applied. No mocks.
Uses a fresh token symbol per run so re-runs are fully idempotent.
"""
import os
import subprocess
import sys
import time
from decimal import Decimal

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req, grant_superadmin

Q = Decimal("0.000000000000000001")  # NUMERIC(38,18) scale


def db(sql):
    dburl = os.environ.get(
        "DATABASE_URL", "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
    subprocess.run(["psql", dburl, "-c", sql], check=True, capture_output=True)


def query(sql):
    dburl = os.environ.get(
        "DATABASE_URL", "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
    out = subprocess.run(["psql", dburl, "-t", "-A", "-c", sql],
                         check=True, capture_output=True, text=True)
    return out.stdout.strip()


def fund(username, asset, chain, amount):
    db(f"""
INSERT INTO wallet_accounts (user_id, asset, chain, address)
SELECT id, '{asset}', '{chain}', 'test-' || id FROM users WHERE username='{username}'
ON CONFLICT DO NOTHING;
INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
SELECT gen_random_uuid(), wa.id, {amount}, 'deposit', 'staking test funding'
FROM wallet_accounts wa JOIN users u ON u.id=wa.user_id
WHERE u.username='{username}' AND wa.asset='{asset}' AND wa.chain='{chain}';
""")


def balance(username, asset, chain):
    row = query(f"""
SELECT COALESCE(SUM(le.amount),0) FROM ledger_entries le
JOIN wallet_accounts wa ON wa.id=le.account_id
JOIN users u ON u.id=wa.user_id
WHERE u.username='{username}' AND wa.asset='{asset}' AND wa.chain='{chain}'
""")
    return Decimal(row or "0")


TREASURY = "00000000-0000-0000-0000-000000000000"


def treasury_balance(asset, chain):
    row = query(f"""
SELECT COALESCE(SUM(le.amount),0) FROM ledger_entries le
JOIN wallet_accounts wa ON wa.id=le.account_id
WHERE wa.user_id='{TREASURY}' AND wa.asset='{asset}' AND wa.chain='{chain}'
""")
    return Decimal(row or "0")


def main():
    ts = int(time.time())
    alice = f"stkA{ts}"
    bob = f"stkB{ts}"
    ASYM = "STK" + str(ts % 100000)
    CHAIN = "test"

    s, r = req("POST", "/api/auth/register", {
        "username": alice, "email": f"{alice}@test.dev", "password": "Passw0rd!123",
        "country_code": "US"})
    check("stk register alice (admin)", s in (200, 201), f"{s} {r}")
    alice_tok = r.get("access_token")

    s, r = req("POST", "/api/auth/register", {
        "username": bob, "email": f"{bob}@test.dev", "password": "Passw0rd!123",
        "country_code": "NG"})
    check("stk register bob", s in (200, 201), f"{s} {r}")
    bob_tok = r.get("access_token")

    grant_superadmin(alice)
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"{alice}@test.dev", "password": "Passw0rd!123"})
    check("stk admin login", s == 200 and r.get("access_token"), f"{s} {r}")
    admin_tok = r.get("access_token")

    db(f"UPDATE users SET kyc_status='verified' WHERE username='{alice}'")

    # ---- token coingecko_id plumbing + prices ----
    s, r = req("GET", "/api/admin/wallet/tokens", token=admin_tok)
    tokens = {t["symbol"] + "/" + t["chain"]: t for t in r.get("tokens", [])}
    btc = tokens.get("BTC/bitcoin", {})
    check("admin tokens lists BTC", bool(btc.get("id")), f"{s}")
    s, r = req("POST", f"/api/admin/wallet/tokens/{btc.get('id', 'x')}/features", {
        "coingecko_id": "bitcoin"}, token=admin_tok)
    check("set coingecko_id on token", s == 200, f"{s} {r}")
    s, r = req("PUT", "/api/admin/prices/BTC/bitcoin", {"price_usd": "67300.5"}, token=admin_tok)
    check("admin price override", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/prices", token=bob_tok)
    brow = next((p for p in r.get("prices", []) if p["asset"] == "BTC" and p["chain"] == "bitcoin"), {})
    check("user prices include BTC override",
          (brow.get("price_usd") or "").startswith("67300.5") and brow.get("source") == "admin",
          f"{s} {brow}")

    # order-book fallback: USDT has a USD offer -> price derived from our own market
    s, r = req("POST", "/api/p2p/offers", {
        "side": "sell", "asset": "USDT", "chain": "ethereum",
        "fiat_currency": "USD", "country_iso": "US", "price": "1.01",
        "min_amount": "10", "max_amount": "500", "payment_methods": ["Zelle"],
        "terms": "x"}, token=alice_tok)
    check("create USD offer for fallback", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/prices", token=bob_tok)
    urow = next((p for p in r.get("prices", []) if p["asset"] == "USDT" and p["chain"] == "ethereum"), {})
    check("orderbook fallback price",
          (urow.get("price_usd") or "").startswith("1.01") and urow.get("source") == "orderbook",
          f"{s} {urow}")

    # ---- fresh token for this run (isolation) ----
    s, r = req("POST", "/api/admin/wallet/tokens", {
        "symbol": ASYM, "name": "Stake Token", "chain": CHAIN, "is_native": True}, token=admin_tok)
    check("admin adds run token", s == 201, f"{s} {r}")
    fund(alice, ASYM, CHAIN, 2)

    # ---- staking asset management ----
    s, r = req("POST", "/api/admin/staking/treasury/move", {
        "direction": "in", "asset": ASYM, "chain": CHAIN,
        "amount": "0.2", "purpose": "seed staking reward pool"}, token=admin_tok)
    check("fund reward pool via treasury in", s == 201, f"{s} {r}")
    s, r = req("POST", "/api/admin/staking/assets", {
        "asset": ASYM, "chain": CHAIN, "apy": "12", "durations_days": [7, 30, 90, 180, 365],
        "min_amount": "0.01"}, token=admin_tok)
    check("create run staking asset (upsert)", s == 201 and r.get("id"), f"{s} {r}")
    asset_id = r.get("id")
    s, r = req("POST", "/api/admin/staking/assets", {
        "asset": ASYM, "chain": CHAIN, "apy": "12", "durations_days": [7, 30, 90, 180, 365],
        "min_amount": "0.01"}, token=admin_tok)
    check("re-create same asset idempotent", s == 201 and r.get("id") == asset_id, f"{s} {r}")

    s, r = req("GET", "/api/staking/assets", token=bob_tok)
    check("user lists staking assets",
          s == 200 and any(a["id"] == asset_id for a in r.get("assets", [])), f"{s} {r}")

    # price display embeds on staking assets (BTC asset has an override)
    s, r = req("POST", "/api/admin/staking/assets", {
        "asset": "BTC", "chain": "bitcoin", "apy": "8"}, token=admin_tok)
    check("create BTC staking asset for price display", s == 201, f"{s} {r}")
    s, r = req("GET", "/api/staking/assets", token=bob_tok)
    prow = next((a for a in r.get("assets", []) if a["asset"] == "BTC"), {})
    check("asset shows live price", (prow.get("price_usd") or "").startswith("67300.5"), f"{prow}")

    # ---- stake validation ----
    s, r = req("POST", "/api/staking/stake", {
        "asset_id": asset_id, "amount": "1", "duration_days": 14}, token=alice_tok)
    check("stake with disallowed duration rejected", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/staking/stake", {
        "asset_id": asset_id, "amount": "0.001", "duration_days": 7}, token=alice_tok)
    check("stake below min rejected", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/staking/stake", {
        "asset_id": asset_id, "amount": "1", "duration_days": 7}, token=bob_tok)
    check("stake without KYC rejected", s == 403, f"{s} {r}")

    before = balance(alice, ASYM, CHAIN)
    s, r = req("POST", "/api/staking/stake", {
        "asset_id": asset_id, "amount": "0.5", "duration_days": 7}, token=alice_tok)
    check("stake 0.5 @12% 7d", s == 201 and r.get("status") == "active", f"{s} {r}")
    pos1 = r.get("id")
    check("principal locked from balance", balance(alice, ASYM, CHAIN) == before - Decimal("0.5"),
          f"{balance(alice, ASYM, CHAIN)}")
    check("treasury holds pool + principal", treasury_balance(ASYM, CHAIN) == Decimal("0.7"),
          f"{treasury_balance(ASYM, CHAIN)}")

    s, r = req("GET", "/api/staking/positions", token=alice_tok)
    check("positions listed with accrued estimate",
          s == 200 and any(p["id"] == pos1 for p in r.get("positions", [])), f"{s} {r}")
    p1 = next(p for p in r.get("positions", []) if p["id"] == pos1)
    check("locked APY + reward math exposed",
          p1.get("apy") == "12.00" and bool(p1.get("reward")), f"{p1}")

    # maturity gating: must be denied before ends_at
    s, r = req("POST", f"/api/staking/positions/{pos1}/unlock", token=alice_tok)
    check("early unlock denied", s == 403 and bool(r.get("unlock_at")), f"{s} {r}")
    s, r = req("GET", "/api/staking/positions", token=alice_tok)
    check("still active after denied unlock",
          next(p for p in r.get("positions", []) if p["id"] == pos1).get("status") == "active",
          f"{s} {r}")

    # backdate past maturity, then unlock settles with reward
    db(f"UPDATE stake_positions SET started_at=started_at - interval '8 days', "
       f"ends_at=ends_at - interval '8 days' WHERE id='{pos1}'")
    s, r = req("POST", f"/api/staking/positions/{pos1}/unlock", token=alice_tok)
    check("mature unlock settles", s == 200 and r.get("status") == "closed", f"{s} {r}")
    r1 = (Decimal("0.5") * Decimal("0.12") * Decimal(7) / Decimal(365)).quantize(Q)
    after = balance(alice, ASYM, CHAIN)
    check("principal + simple reward returned",
          after == before + r1, f"{after} vs {before + r1}")
    check("treasury reduced by payout", treasury_balance(ASYM, CHAIN) == Decimal("0.2") - r1,
          f"{treasury_balance(ASYM, CHAIN)}")
    s, r = req("POST", f"/api/staking/positions/{pos1}/unlock", token=alice_tok)
    check("repeat unlock idempotent", s == 200 and r.get("status") == "closed", f"{s} {r}")
    check("no double payout", treasury_balance(ASYM, CHAIN) == Decimal("0.2") - r1,
          f"{treasury_balance(ASYM, CHAIN)}")

    # ---- treasury deploy + queued unlock ----
    s, r = req("POST", "/api/staking/stake", {
        "asset_id": asset_id, "amount": "0.5", "duration_days": 30}, token=alice_tok)
    check("second stake (30d)", s == 201, f"{s} {r}")
    pos2 = r.get("id")
    db(f"UPDATE stake_positions SET started_at=started_at - interval '31 days', "
       f"ends_at=ends_at - interval '31 days' WHERE id='{pos2}'")

    s, r = req("POST", "/api/admin/staking/treasury/move", {
        "direction": "out", "asset": ASYM, "chain": CHAIN,
        "amount": "0.4", "purpose": "buy treasury stock basket"}, token=admin_tok)
    check("superadmin deploys staked funds externally", s == 201, f"{s} {r}")
    s, r = req("POST", f"/api/staking/positions/{pos2}/unlock", token=alice_tok)
    check("unlock with illiquid treasury queues",
          s == 200 and r.get("status") == "unlock_requested", f"{s} {r}")
    s, r = req("GET", "/api/admin/staking/queue", token=admin_tok)
    check("queue visible to admin",
          s == 200 and any(p["id"] == pos2 for p in r.get("positions", [])), f"{s} {r}")
    s, r = req("POST", "/api/admin/staking/treasury/move", {
        "direction": "in", "asset": ASYM, "chain": CHAIN,
        "amount": "0.8", "purpose": "redeposit sale proceeds"}, token=admin_tok)
    check("redeposit auto-settles queue", s == 201 and r.get("settled", 0) >= 1, f"{s} {r}")
    s, r = req("GET", "/api/staking/positions", token=alice_tok)
    closed2 = next(p for p in r.get("positions", []) if p["id"] == pos2)
    check("queued position now closed",
          closed2.get("status") == "closed" and bool(closed2.get("closed_at")), f"{closed2}")
    r2 = (Decimal("0.5") * Decimal("0.12") * Decimal(30) / Decimal(365)).quantize(Q)
    final2 = balance(alice, ASYM, CHAIN)
    check("30d reward paid", final2 == after + r2, f"{final2} vs {after + r2}")

    # deploy beyond liquidity rejected
    s, r = req("POST", "/api/admin/staking/treasury/move", {
        "direction": "out", "asset": ASYM, "chain": CHAIN, "amount": "999"}, token=admin_tok)
    check("deploy beyond liquidity rejected", s == 400, f"{s} {r}")

    # ---- APY change: not retroactive; audit kept ----
    s, r = req("PUT", f"/api/admin/staking/assets/{asset_id}", {"apy": "20"}, token=admin_tok)
    check("admin changes APY", s == 200, f"{s} {r}")
    audit = query(f"SELECT COUNT(*) FROM staking_rates WHERE asset_id='{asset_id}' AND apy=20.00")
    check("APY change audited", audit == "1", f"{audit}")
    s, r = req("POST", "/api/staking/stake", {
        "asset_id": asset_id, "amount": "0.5", "duration_days": 7}, token=alice_tok)
    check("stake under new APY", s == 201, f"{s} {r}")
    pos3 = r.get("id")
    s, r = req("GET", "/api/staking/positions", token=alice_tok)
    check("new position locked at 20% APY",
          next(p for p in r.get("positions", []) if p["id"] == pos3).get("apy") == "20.00", f"{s} {r}")

    # ---- asset lifecycle ----
    s, r = req("DELETE", f"/api/admin/staking/assets/{asset_id}", token=admin_tok)
    check("delete with history blocked", s == 409, f"{s} {r}")
    s, r = req("PUT", f"/api/admin/staking/assets/{asset_id}", {"active": False}, token=admin_tok)
    check("deactivate asset", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/staking/assets", token=bob_tok)
    check("inactive asset hidden from users",
          all(a["id"] != asset_id for a in r.get("assets", [])), f"{s} {r}")

    # admin positions + moves listing
    s, r = req("GET", "/api/admin/staking/positions", token=admin_tok)
    check("admin sees all positions", s == 200 and len(r.get("positions", [])) >= 2, f"{s} {r}")
    s, r = req("GET", "/api/admin/staking/treasury", token=admin_tok)
    check("treasury moves listed", s == 200 and len(r.get("moves", [])) >= 2, f"{s} {r}")

    # ---- parity endpoints (admin dashboard shape; closed by gap handler work) ----
    s, r = req("GET", "/api/admin/staking/treasury/moves", token=admin_tok)
    check("treasury/moves alias", s == 200 and len(r.get("moves", [])) >= 2, f"{s} {r}")
    s, r = req("GET", "/api/admin/staking/audit", token=admin_tok)
    check("staking audit totals",
          s == 200 and "total_locked_usd" in r and "positions_active" in r, f"{s} {r}")
    s, r = req("GET", "/api/admin/prices", token=admin_tok)
    check("admin prices list", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/admin/staking/assets", token=admin_tok)
    check("admin assets list (for resolver test)", s == 200, f"{s} {r}")
    # dashboard-friendly update-by (asset, chain)
    s, r = req("PUT", f"/api/admin/staking/assets/{ASYM}/{CHAIN}", {"apy": "25"}, token=admin_tok)
    check("update asset by asset/chain", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/admin/staking/assets", token=admin_tok)
    check("updated asset APY 25", any(a["apy"] == "25.00" for a in r.get("assets", [])), f"{s} {r}")
    # settle-by position_id variant
    s, r = req("POST", "/api/admin/staking/settle", {"position_id": pos3}, token=admin_tok)
    check("settle by body position_id", s in (200, 409, 404), f"{s} {r}")
    # settle-by filter variant settles nothing when queue is empty
    s, r = req("POST", "/api/admin/staking/settle", {"asset": ASYM, "chain": CHAIN}, token=admin_tok)
    check("settle by filter empty queue ok", s == 200 and r.get("pending_total", 0) == 0, f"{s} {r}")


if __name__ == "__main__":
    main()
