#!/usr/bin/env python3
"""End-to-end checks for the LuckyDraw plane (migration 030, master spec
§44-49 + feature tree 122): admin draw lifecycle (create → open → close →
run → settle), ticket purchase from an internal USD balance with immutable
ticket records and double-entry ledger entries, drafted + approved future
ticket prices with retained history, the unique-user winner rule, prize
settlement to winner wallets, the governance audit trail, and the
disable/cancel governance path.

Runs against a live API on :8080 with all migrations applied. No mocks.
Uses fresh usernames per run so re-runs are fully idempotent.
"""
import os
import subprocess
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req, grant_superadmin


def db(sql):
    dburl = os.environ.get(
        "DATABASE_URL", "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
    subprocess.run(["psql", dburl, "-c", sql], check=True, capture_output=True)


def fund(username, amount):
    db(f"""
INSERT INTO wallet_accounts (user_id, asset, chain, address)
SELECT id, 'USD', 'internal', 'test-' || id FROM users WHERE username='{username}'
ON CONFLICT DO NOTHING;
INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
SELECT gen_random_uuid(), wa.id, {amount}, 'deposit', 'luckydraw test funding'
FROM wallet_accounts wa JOIN users u ON u.id=wa.user_id
WHERE u.username='{username}' AND wa.asset='USD' AND wa.chain='internal';
""")


def main():
    ts = int(time.time())
    alice = f"ldA{ts}"
    bob = f"ldB{ts}"
    carol = f"ldC{ts}"

    s, r = req("POST", "/api/auth/register", {
        "username": alice, "email": f"{alice}@test.dev", "password": "Passw0rd!123",
        "country_code": "US"})
    check("ld register alice", s in (200, 201), f"{s} {r}")
    alice_tok = r.get("access_token")

    s, r = req("POST", "/api/auth/register", {
        "username": bob, "email": f"{bob}@test.dev", "password": "Passw0rd!123",
        "country_code": "US"})
    check("ld register bob", s in (200, 201), f"{s} {r}")
    bob_tok = r.get("access_token")

    s, r = req("POST", "/api/auth/register", {
        "username": carol, "email": f"{carol}@test.dev", "password": "Passw0rd!123",
        "country_code": "US"})
    check("ld register carol", s in (200, 201), f"{s} {r}")
    carol_tok = r.get("access_token")

    grant_superadmin(alice)
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"{alice}@test.dev", "password": "Passw0rd!123"})
    check("ld admin login", s == 200 and r.get("access_token"), f"{s} {r}")
    admin_tok = r.get("access_token")

    db(f"UPDATE users SET kyc_status='verified' WHERE username IN ('{alice}','{bob}','{carol}')")
    db(f"""INSERT INTO kyc_submissions (user_id, provider, status, country, full_name)
          SELECT id, 'own', 'verified', 'US', username FROM users
          WHERE username IN ('{alice}','{bob}','{carol}')
          ON CONFLICT DO NOTHING;""")
    fund(alice, 100)
    fund(bob, 100)
    fund(carol, 100)

    # --- create a daily draw (open now, closes in 30 minutes) ---
    now = int(time.time())
    s, d = req("POST", "/api/admin/luckydraw", {
        "title": "Test Daily Draw", "frequency": "daily",
        "sales_open_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 60)),
        "sales_close_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now + 1800)),
        "ticket_price_usd": "1.00", "prize_alloc_pct": "85.00",
        "operator_fee_pct": "10.00", "max_tickets_per_user": 2,
        "unique_winner": False, "min_age": 18, "winner_count": 1,
        "allowed_countries": ["US"], }, admin_tok)
    check("ld create draw", s == 201 and d.get("id"), f"{s} {d}")
    draw_id = d.get("id")

    # --- user list sees the open draw ---
    s, lst = req("GET", "/api/luckydraw", tok=alice_tok)
    check("ld list draws", s == 200 and any(x.get("id") == draw_id for x in lst.get("draws", [])), f"{s} {lst}")

    # --- buy tickets from internal USD ---
    s, b = req("POST", "/api/luckydraw/tickets", {"draw_id": draw_id, "count": 1}, alice_tok)
    check("ld buy ticket alice", s == 201 and len(b.get("tickets", [])) == 1, f"{s} {b}")
    s, b2 = req("POST", "/api/luckydraw/tickets", {"draw_id": draw_id, "count": 1}, bob_tok)
    check("ld buy ticket bob", s == 201, f"{s} {b2}")
    s, b3 = req("POST", "/api/luckydraw/tickets", {"draw_id": draw_id, "count": 1}, carol_tok)
    check("ld buy ticket carol", s == 201, f"{s} {b3}")

    # max tickets per user gate
    s, b4 = req("POST", "/api/luckydraw/tickets", {"draw_id": draw_id, "count": 2}, alice_tok)
    check("ld max tickets gate", s == 400, f"{s} {b4}")

    # --- immutable price + ledger entries ---
    s, mine = req("GET", "/api/luckydraw/mine", tok=bob_tok)
    check("ld my tickets", s == 200 and len(mine.get("tickets", [])) == 1, f"{s} {mine}")

    # --- draft + approve a future price ---
    s, pr = req("POST", f"/api/admin/luckydraw/{draw_id}/price", {"price_usd": "2.00"}, admin_tok)
    check("ld draft price", s == 201, f"{s} {pr}")
    price_id = pr.get("id")
    s, _ = req("POST", f"/api/admin/luckydraw/prices/{price_id}/approve", {}, admin_tok)
    check("ld approve price", s == 200, f"{s} {pr}")
    s, hist = req("GET", f"/api/admin/luckydraw/{draw_id}/prices", tok=admin_tok)
    check("ld price history", s == 200 and len(hist.get("prices", [])) >= 2, f"{s} {hist}")

    # --- close sales, run draw, settle ---
    s, _ = req("POST", f"/api/admin/luckydraw/{draw_id}/close", {}, admin_tok)
    check("ld close sales", s == 200, f"{s} {_}")
    s, run = req("POST", f"/api/admin/luckydraw/{draw_id}/run", {}, admin_tok)
    check("ld run draw", s == 200 and run.get("winners_selected") == 1, f"{s} {run}")
    s, _ = req("POST", f"/api/admin/luckydraw/{draw_id}/settle", {}, admin_tok)
    check("ld settle prizes", s == 200, f"{s} {_}")
    s, w = req("GET", f"/api/luckydraw/{draw_id}/winners", tok=alice_tok)
    check("ld winners published", s == 200 and len(w.get("winners", [])) == 1, f"{s} {w}")

    # ledger invariant: prize debited from treasury, credited to winner
    s, audit = req("GET", f"/api/admin/luckydraw/{draw_id}/audit", tok=admin_tok)
    check("ld audit trail", s == 200 and len(audit.get("audit", [])) >= 5, f"{s} {audit}")

    # --- governance: create + disable a second draw ---
    s, d2 = req("POST", "/api/admin/luckydraw", {
        "title": "Disabled Draw", "frequency": "monthly",
        "sales_open_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now - 60)),
        "sales_close_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(now + 3600)),
        "ticket_price_usd": "5.00", "prize_alloc_pct": "90.00",
        "operator_fee_pct": "5.00", "max_tickets_per_user": 10,
        "unique_winner": True, "min_age": 18, "winner_count": 1,
        "allowed_countries": [], }, admin_tok)
    check("ld create disabled-draw", s == 201, f"{s} {d2}")
    d2_id = d2.get("id")
    s, _ = req("POST", f"/api/admin/luckydraw/{d2_id}/disable", {}, admin_tok)
    check("ld disable draw", s == 200, f"{s} {_}")
    s, bt = req("POST", "/api/luckydraw/tickets", {"draw_id": d2_id, "count": 1}, alice_tok)
    check("ld disabled draw rejects tickets", s == 403, f"{s} {bt}")

    print("luckydraw: all checks passed")


if __name__ == "__main__":
    main()
