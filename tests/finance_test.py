#!/usr/bin/env python3
"""End-to-end checks for the finance plane (migration 015+):
deterministic multi-chain deposit addresses, signed withdrawal pipeline with
sub-second auto-approval + superadmin review, escrowed P2P trades with local
payment methods, convert engine, per-token feature switches, dynamic roles.

Runs against a live API on :8080 with all migrations applied. No mocks.
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


def fund(username, asset, chain, amount):
    db(f"""
INSERT INTO wallet_accounts (user_id, asset, chain, address)
SELECT id, '{asset}', '{chain}', 'test-' || id FROM users WHERE username='{username}'
ON CONFLICT DO NOTHING;
INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
SELECT gen_random_uuid(), wa.id, {amount}, 'deposit', 'finance test funding'
FROM wallet_accounts wa JOIN users u ON u.id=wa.user_id
WHERE u.username='{username}' AND wa.asset='{asset}' AND wa.chain='{chain}';
""")


def main():
    ts = int(time.time())
    alice = f"finA{ts}"
    bob = f"finB{ts}"

    s, r = req("POST", "/api/auth/register", {
        "username": alice, "email": f"{alice}@test.dev", "password": "Passw0rd!123",
        "country_code": "US"})
    check("fin register alice", s in (200, 201), f"{s} {r}")
    alice_tok = r.get("access_token")

    s, r = req("POST", "/api/auth/register", {
        "username": bob, "email": f"{bob}@test.dev", "password": "Passw0rd!123",
        "country_code": "NG"})
    check("fin register bob", s in (200, 201), f"{s} {r}")
    bob_tok = r.get("access_token")

    grant_superadmin(alice)
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"{alice}@test.dev", "password": "Passw0rd!123"})
    check("admin login (superadmin)", s == 200 and r.get("access_token"), f"{s} {r}")
    admin_tok = r.get("access_token")

    db(f"UPDATE users SET kyc_status='verified' WHERE username IN ('{alice}','{bob}')")

    # --- deterministic deposit addresses ---
    s, a1 = req("POST", "/api/wallet/deposit-address", {"asset": "BTC", "chain": "bitcoin"}, alice_tok)
    s2, a2 = req("POST", "/api/wallet/deposit-address", {"asset": "BTC", "chain": "bitcoin"}, alice_tok)
    check("btc deposit address bech32", s == 200 and a1.get("address", "").startswith("bc1q"), f"{s} {a1}")
    check("deposit address deterministic", a1.get("address") == a2.get("address"))
    check("deposit uri for QR", a1.get("uri", "").startswith("bitcoin:"), f"{a1}")
    s, a3 = req("POST", "/api/wallet/deposit-address", {"asset": "USDT", "chain": "ethereum"}, alice_tok)
    check("evm deposit address", s == 200 and a3.get("address", "").startswith("0x"), f"{s} {a3}")
    s, a4 = req("POST", "/api/wallet/deposit-address", {"asset": "SOL", "chain": "solana"}, alice_tok)
    check("sol deposit address base58", s == 200 and len(a4.get("address", "")) >= 32, f"{s} {a4}")

    # --- funding (test bootstrap) ---
    fund(alice, "USD", "internal", 500)
    fund(alice, "USDT", "tron", 100)

    # --- withdrawal pipeline ---
    tron_addr = "TJCnKsPa7y5okkXvQAidZBzqxrQy6sjSxU"
    s, w = req("POST", "/api/wallet/withdraw",
               {"asset": "USDT", "chain": "tron", "to_address": tron_addr, "amount": "10"}, alice_tok)
    check("small withdrawal auto-approved", s == 201 and w.get("auto_approved") is True, f"{s} {w}")
    check("auto-approval under 1 second", w.get("approved_in_ms", 99999) < 1000, f"{w}")
    check("withdrawal signed", w.get("status") == "signed", f"{w}")

    s, w = req("POST", "/api/wallet/withdraw",
               {"asset": "USDT", "chain": "tron", "to_address": "notanaddress", "amount": "1"}, alice_tok)
    check("invalid destination rejected", s == 400, f"{s} {w}")

    fund(alice, "BTC", "bitcoin", 50)
    s, w = req("POST", "/api/wallet/withdraw",
               {"asset": "BTC", "chain": "bitcoin",
                "to_address": "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", "amount": "50"}, alice_tok)
    check("large withdrawal queued for review",
          s == 201 and w.get("auto_approved") is False and w.get("status") == "pending_review", f"{s} {w}")
    big_id = w.get("id")

    s, wl = req("GET", "/api/admin/withdrawals?status=pending_review", token=admin_tok)
    check("admin sees pending withdrawal",
          s == 200 and any(x.get("id") == big_id for x in wl.get("withdrawals", [])), f"{s} {wl}")
    s, r = req("POST", f"/api/admin/withdrawals/{big_id}/review", {"decision": "approve"}, admin_tok)
    check("superadmin signs+approves", s == 200 and r.get("status") in ("signed", "completed"), f"{s} {r}")
    s, r = req("POST", f"/api/admin/withdrawals/{big_id}/review", {"decision": "approve"}, bob_tok)
    check("non-admin cannot approve withdrawal", s in (401, 403), f"{s} {r}")

    s, wl = req("GET", "/api/wallet/withdrawals", token=alice_tok)
    check("user withdrawal history", s == 200 and len(wl.get("withdrawals", [])) >= 2, f"{s}")

    # --- convert ---
    s, q = req("GET", "/api/convert/quote?from_asset=USD&from_chain=internal&to_asset=USDT&to_chain=tron&amount=50",
               token=alice_tok)
    check("convert quote", s == 200 and q.get("to_amount"), f"{s} {q}")
    s, c = req("POST", "/api/convert", {
        "from_asset": "USD", "from_chain": "internal",
        "to_asset": "USDT", "to_chain": "tron", "amount": "50"}, alice_tok)
    check("convert executes", s == 201 and c.get("id"), f"{s} {c}")
    s, h = req("GET", "/api/convert/history", token=alice_tok)
    check("convert history", s == 200 and any(x.get("id") == c.get("id") for x in h.get("conversions", [])), f"{s}")

    # --- p2p: local rails for 200+ countries ---
    s, m = req("GET", "/api/p2p/payment-methods?country=NG", token=bob_tok)
    check("NG local rails", s == 200 and any(x["name"] == "OPay" for x in m.get("methods", [])), f"{s} {m}")
    s, m = req("GET", "/api/p2p/payment-methods?country=IN", token=bob_tok)
    check("IN local rails", s == 200 and any(x["name"] == "UPI" for x in m.get("methods", [])), f"{s} {m}")
    s, m = req("GET", "/api/p2p/payment-methods?country=BR", token=bob_tok)
    check("BR local rails", s == 200 and any(x["name"] == "Pix" for x in m.get("methods", [])), f"{s} {m}")

    s, o = req("POST", "/api/p2p/offers", {
        "side": "sell", "asset": "USDT", "chain": "tron", "fiat_currency": "NGN",
        "country_iso": "NG", "price": "1500", "min_amount": "10", "max_amount": "100",
        "payment_methods": ["OPay", "Bank transfer"], "terms": "fast"}, alice_tok)
    check("p2p offer create", s == 201 and o.get("id"), f"{s} {o}")
    oid = o.get("id")

    s, lst = req("GET", "/api/p2p/offers?country=NG&asset=USDT&chain=tron&side=sell", token=bob_tok)
    check("p2p offer listed", s == 200 and any(x.get("id") == oid for x in lst.get("offers", [])), f"{s}")

    s, t = req("POST", "/api/p2p/trades",
               {"offer_id": oid, "crypto_amount": "20", "payment_method": "OPay"}, bob_tok)
    check("p2p trade opens with escrow", s == 201 and t.get("status") == "open", f"{s} {t}")
    tid = t.get("id")

    s, acc = req("GET", "/api/wallet/accounts", token=alice_tok)
    usdt_tron = [a for a in acc.get("accounts", []) if a["asset"] == "USDT" and a["chain"] == "tron"]
    check("escrow locks seller funds",
          usdt_tron and float(usdt_tron[0]["balance"]) < 150, f"{acc}")

    s, r = req("POST", f"/api/p2p/trades/{tid}/paid", {}, bob_tok)
    check("buyer marks paid", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/p2p/trades/{tid}/release", {}, alice_tok)
    check("seller releases escrow", s == 200, f"{s} {r}")
    s, acc = req("GET", "/api/wallet/accounts", token=bob_tok)
    check("buyer received crypto",
          any(a["asset"] == "USDT" and float(a["balance"]) >= 20 for a in acc.get("accounts", [])), f"{acc}")

    # --- dispute flow ---
    s, t2 = req("POST", "/api/p2p/trades",
                {"offer_id": oid, "crypto_amount": "10", "payment_method": "Bank transfer"}, bob_tok)
    tid2 = t2.get("id")
    req("POST", f"/api/p2p/trades/{tid2}/paid", {}, bob_tok)
    s, r = req("POST", f"/api/p2p/trades/{tid2}/dispute", {}, bob_tok)
    check("dispute opens", s == 200, f"{s} {r}")
    s, d = req("GET", "/api/admin/p2p/disputes", token=admin_tok)
    check("admin sees dispute",
          s == 200 and any(x.get("id") == tid2 for x in d.get("trades", [])), f"{s}")
    s, r = req("POST", f"/api/admin/p2p/trades/{tid2}/resolve", {"to": "buyer"}, admin_tok)
    check("admin resolves to buyer", s == 200, f"{s} {r}")

    # --- token feature switches ---
    s, tl = req("GET", "/api/admin/wallet/tokens", token=admin_tok)
    tok = next(t for t in tl.get("tokens", []) if t["symbol"] == "USDT" and t["chain"] == "tron")
    s, r = req("POST", f"/api/admin/wallet/tokens/{tok['id']}/features",
               {"withdraw_enabled": False}, admin_tok)
    check("disable token withdrawals", s == 200, f"{s} {r}")
    s, r = req("POST", "/api/wallet/withdraw",
               {"asset": "USDT", "chain": "tron", "to_address": tron_addr, "amount": "1"}, alice_tok)
    check("withdraw blocked while disabled", s == 400, f"{s} {r}")
    s, r = req("POST", f"/api/admin/wallet/tokens/{tok['id']}/features",
               {"withdraw_enabled": True}, admin_tok)
    check("re-enable token withdrawals", s == 200, f"{s} {r}")

    # --- superadmin token management ---
    s, r = req("POST", "/api/admin/wallet/tokens", {
        "symbol": "DOGE", "name": "Dogecoin", "chain": "dogecoin",
        "decimals": 8, "is_native": True}, admin_tok)
    check("superadmin adds coin", s in (200, 201), f"{s} {r}")
    s, tl = req("GET", "/api/admin/wallet/tokens", token=admin_tok)
    doge = next((t for t in tl.get("tokens", []) if t["symbol"] == "DOGE"), None)
    check("new coin listed", doge is not None, f"{s}")
    if doge:
        s, r = req("DELETE", f"/api/admin/wallet/tokens/{doge['id']}", token=admin_tok)
        check("superadmin removes coin", s == 200, f"{s} {r}")

    # --- convert rates ---
    s, r = req("POST", "/api/admin/convert/rates",
               {"asset": "USDT", "chain": "tron", "usd_rate": "1.02"}, admin_tok)
    check("admin sets convert rate", s in (200, 201), f"{s} {r}")
    s, q = req("GET", "/api/convert/quote?from_asset=USDT&from_chain=tron&to_asset=USD&to_chain=internal&amount=100",
               token=alice_tok)
    check("quote reflects new rate", abs(float(q.get("to_amount", 0)) - 102) < 0.001, f"{q}")

    # --- dynamic roles ---
    s, r = req("POST", "/api/admin/role-defs",
               {"name": f"p2p_ops_{ts}", "permissions": ["p2p.resolve"]}, admin_tok)
    check("superadmin creates role", s == 201, f"{s} {r}")
    s, r = req("GET", "/api/admin/role-defs", token=admin_tok)
    check("role listed", s == 200 and any(x["name"] == f"p2p_ops_{ts}" for x in r.get("roles", [])), f"{s}")
    s, r = req("DELETE", f"/api/admin/role-defs/p2p_ops_{ts}", token=admin_tok)
    check("superadmin deletes role", s == 200, f"{s} {r}")

    print()
    return 0


if __name__ == "__main__":
    sys.exit(main())
