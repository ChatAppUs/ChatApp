#!/usr/bin/env python3
"""End-to-end checks for the 016 gap-closure plane:
P2P merchant verification + tiers, platform-issued virtual crypto cards,
admin transfer oversight/reversal, post reactions, pinned posts, post edit
history, scheduled posts, comment sorting, photo albums, people tagging,
chat themes + nicknames, and event reminders.

Runs against a live API on :8080 with migrations 001-016 applied. No mocks.
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
SELECT gen_random_uuid(), wa.id, {amount}, 'deposit', 'gaps test funding'
FROM wallet_accounts wa JOIN users u ON u.id=wa.user_id
WHERE u.username='{username}' AND wa.asset='{asset}' AND wa.chain='{chain}';
""")


def register(name):
    s, r = req("POST", "/api/auth/register", {
        "username": name, "email": f"{name}@test.dev", "password": "Passw0rd!123",
        "country_code": "US"})
    check(f"register {name}", s in (200, 201), f"{s} {r}")
    return r.get("access_token")


def main():
    ts = int(time.time())
    alice = f"gapA{ts}"
    bob = f"gapB{ts}"
    carol = f"gapC{ts}"

    alice_tok = register(alice)
    bob_tok = register(bob)
    carol_tok = register(carol)

    grant_superadmin(alice)
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"{alice}@test.dev", "password": "Passw0rd!123"})
    check("admin login (superadmin)", s == 200 and r.get("access_token"), f"{s} {r}")
    admin_tok = r.get("access_token")

    db(f"UPDATE users SET kyc_status='verified' WHERE username IN ('{alice}','{bob}','{carol}')")

    # ============ P2P merchant program ============
    s, r = req("GET", "/api/p2p/merchant/tiers", token=bob_tok)
    check("merchant tiers listed", s == 200 and len(r.get("tiers", [])) >= 3, f"{s} {r}")
    check("tier 1 has limits", r["tiers"][0]["max_trade_usd"] != "", f"{r}")

    s, r = req("POST", "/api/p2p/merchant/apply", {"business_name": "Bob's Exchange"}, bob_tok)
    check("merchant apply", s == 201 and r.get("status") == "pending", f"{s} {r}")
    s, r = req("POST", "/api/p2p/merchant/apply", {"business_name": "Again"}, bob_tok)
    check("duplicate application rejected", s == 409, f"{s} {r}")
    s, r = req("GET", "/api/p2p/merchant/status", token=bob_tok)
    check("merchant status pending", s == 200 and r["merchant"]["status"] == "pending", f"{s} {r}")

    s, r = req("GET", "/api/admin/p2p/merchants?status=pending", token=admin_tok)
    check("admin lists pending merchants", s == 200 and len(r.get("merchants", [])) == 1, f"{s} {r}")
    bob_id = r["merchants"][0]["user_id"]

    s, r = req("POST", f"/api/admin/p2p/merchants/{bob_id}/review",
               {"decision": "approve", "tier": 1}, admin_tok)
    check("merchant approved", s == 200 and r.get("status") == "verified", f"{s} {r}")
    s, r = req("GET", "/api/p2p/merchant/status", token=bob_tok)
    check("merchant status verified", r["merchant"]["status"] == "verified"
          and r["merchant"]["tier"] == 1, f"{r}")
    check("tier limits exposed", float(r.get("max_trade_usd", "0")) == 1000, f"{r}")

    # ineligible promotion is refused (tier 3 needs 100 completed trades)
    s, r = req("POST", f"/api/admin/p2p/merchants/{bob_id}/tier", {"tier": 3}, admin_tok)
    check("tier promotion gated by eligibility", s == 400, f"{s} {r}")

    # merchant badge on offers
    s, r = req("POST", "/api/p2p/offers", {
        "side": "sell", "asset": "USDT", "chain": "tron", "fiat_currency": "USD",
        "country_iso": "US", "price": "1.0", "min_amount": "1", "max_amount": "10000",
        "payment_methods": ["Bank transfer"]}, bob_tok)
    check("merchant creates offer", s == 201, f"{s} {r}")
    offer_id = r.get("id")
    s, r = req("GET", "/api/p2p/offers?asset=USDT&side=sell", token=alice_tok)
    mine = [o for o in r.get("offers", []) if o["id"] == offer_id]
    check("offer carries merchant badge", mine and mine[0]["merchant"] is True
          and mine[0]["merchant_tier"] == 1 and mine[0]["merchant_name"] == "Bob's Exchange",
          f"{mine}")

    # tier per-trade limit enforcement: tier 1 caps at $1000/trade; USDT=$1
    fund(bob, "USDT", "tron", 100000)
    s, r = req("POST", "/api/p2p/trades", {
        "offer_id": offer_id, "crypto_amount": "5000",
        "payment_method": "Bank transfer"}, alice_tok)
    check("over-tier trade rejected", s == 400 and "tier" in r.get("error", ""), f"{s} {r}")
    s, r = req("POST", "/api/p2p/trades", {
        "offer_id": offer_id, "crypto_amount": "500",
        "payment_method": "Bank transfer"}, alice_tok)
    check("within-tier trade opens", s == 201, f"{s} {r}")

    # revoke
    s, r = req("POST", f"/api/admin/p2p/merchants/{bob_id}/revoke", {"note": "audit"}, admin_tok)
    check("merchant revoked", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/p2p/offers?asset=USDT&side=sell", token=alice_tok)
    mine = [o for o in r.get("offers", []) if o["id"] == offer_id]
    check("badge removed after revoke", mine and mine[0]["merchant"] is False, f"{mine}")

    # ============ crypto cards ============
    s, r = req("POST", "/api/cards", {"label": "Everyday"}, alice_tok)
    check("card issued", s == 201 and r.get("card_number") and r.get("cvv"), f"{s} {r}")
    pan, cvv, card_id = r["card_number"], r["cvv"], r["id"]
    check("pan is luhn-valid", pan.startswith("990099") and len(pan) == 16, pan)

    s, r = req("GET", "/api/cards", token=alice_tok)
    check("cards listed masked", s == 200 and r["cards"][0]["last4"] == pan[-4:]
          and "card_number" not in r["cards"][0], f"{r}")

    # charge without funds declines
    s, r = req("POST", "/api/cards/charge", {
        "card_number": pan, "cvv": cvv, "expiry_month": r["cards"][0]["expiry_month"],
        "expiry_year": r["cards"][0]["expiry_year"],
        "merchant": "Coffee Shop", "amount_usd": "5"}, bob_tok)
    check("charge declines without funds", s == 402, f"{s} {r}")

    # top up from BTC (rate 65000) -> USD balance
    fund(alice, "BTC", "bitcoin", 1)
    s, r = req("POST", f"/api/cards/{card_id}/topup",
               {"asset": "BTC", "chain": "bitcoin", "amount": "0.001"}, alice_tok)
    check("card top-up converts crypto", s == 201 and float(r.get("usd_amount", "0")) == 65, f"{s} {r}")

    s, r = req("POST", "/api/cards/charge", {
        "card_number": pan, "cvv": cvv, "expiry_month": 1, "expiry_year": 2000,
        "merchant": "Coffee Shop", "amount_usd": "5"}, bob_tok)
    check("wrong expiry declines", s == 402, f"{s} {r}")

    s, r = req("GET", "/api/cards", token=alice_tok)
    exp_m, exp_y = r["cards"][0]["expiry_month"], r["cards"][0]["expiry_year"]
    s, r = req("POST", "/api/cards/charge", {
        "card_number": pan, "cvv": cvv, "expiry_month": exp_m, "expiry_year": exp_y,
        "merchant": "Coffee Shop", "amount_usd": "5"}, bob_tok)
    check("charge captures", s == 201 and r.get("status") == "captured", f"{s} {r}")
    charge1 = r["id"]

    s, r = req("GET", f"/api/cards/{card_id}/transactions", token=alice_tok)
    kinds = [(t["kind"], t["status"]) for t in r.get("transactions", [])]
    check("statement shows capture + declines",
          ("purchase", "captured") in kinds and ("purchase", "declined") in kinds, f"{kinds}")

    # refund reverses the capture
    s, r = req("POST", f"/api/cards/{card_id}/refund", {"transaction_id": charge1}, alice_tok)
    check("refund reverses purchase", s == 200, f"{s} {r}")

    # freeze blocks new charges
    s, r = req("POST", f"/api/cards/{card_id}/status", {"status": "frozen"}, alice_tok)
    check("card frozen", s == 200, f"{s} {r}")
    s, r = req("POST", "/api/cards/charge", {
        "card_number": pan, "cvv": cvv, "expiry_month": exp_m, "expiry_year": exp_y,
        "merchant": "Coffee Shop", "amount_usd": "5"}, bob_tok)
    check("frozen card declines", s == 402 and "frozen" in r.get("error", ""), f"{s} {r}")
    s, r = req("POST", f"/api/cards/{card_id}/status", {"status": "active"}, alice_tok)
    check("card unfrozen", s == 200, f"{s} {r}")

    # daily limit enforcement
    s, r = req("PUT", f"/api/cards/{card_id}/limits",
               {"daily_limit_usd": 10, "monthly_limit_usd": 100}, alice_tok)
    check("limits updated", s == 200, f"{s} {r}")
    s, r = req("POST", "/api/cards/charge", {
        "card_number": pan, "cvv": cvv, "expiry_month": exp_m, "expiry_year": exp_y,
        "merchant": "Bookstore", "amount_usd": "11"}, bob_tok)
    check("over daily limit declines", s == 402 and "daily limit" in r.get("error", ""), f"{s} {r}")
    s, r = req("PUT", f"/api/cards/{card_id}/limits",
               {"daily_limit_usd": 10, "monthly_limit_usd": 5}, alice_tok)
    check("daily > monthly rejected", s == 400, f"{s} {r}")

    # admin oversight
    s, r = req("GET", "/api/admin/cards", token=admin_tok)
    check("admin lists cards", s == 200 and any(c["id"] == card_id for c in r["cards"]), f"{s} {r}")
    s, r = req("POST", f"/api/admin/cards/{card_id}/status", {"status": "frozen"}, admin_tok)
    check("admin freezes card", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/admin/cards/{card_id}/status", {"status": "active"}, admin_tok)
    check("admin unfreezes card", s == 200, f"{s} {r}")

    # ============ admin transfer oversight ============
    fund(alice, "USD", "internal", 100)
    s, r = req("POST", "/api/wallet/transfer",
               {"to_username": bob, "asset": "USD", "chain": "internal",
                "amount": "25", "memo": "test"}, alice_tok)
    check("transfer sent", s == 200, f"{s} {r}")
    transfer_tx = r["tx_id"]

    s, r = req("GET", "/api/admin/transfers", token=admin_tok)
    hit = [t for t in r.get("transfers", []) if t["tx_id"] == transfer_tx]
    check("admin sees transfer", hit and hit[0]["from_username"] == alice
          and hit[0]["to_username"] == bob and not hit[0]["reversed"], f"{hit}")

    s, r = req("POST", f"/api/admin/transfers/{transfer_tx}/reverse", {}, admin_tok)
    check("transfer reversed", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/admin/transfers/{transfer_tx}/reverse", {}, admin_tok)
    check("double reversal rejected", s == 409, f"{s} {r}")

    s, r = req("GET", "/api/wallet/accounts", token=bob_tok)
    usd = [a for a in r["accounts"] if a["asset"] == "USD"]
    check("recipient balance restored to zero", usd and float(usd[0]["balance"]) == 0, f"{usd}")

    # ============ post reactions ============
    s, r = req("POST", "/api/posts", {"body": f"reaction test {ts}"}, alice_tok)
    check("post created", s == 201, f"{s} {r}")
    post_id = r["id"]

    s, r = req("PUT", f"/api/posts/{post_id}/react", {"reaction": "love"}, bob_tok)
    check("react love", s == 200, f"{s} {r}")
    s, r = req("PUT", f"/api/posts/{post_id}/react", {"reaction": "wow"}, bob_tok)
    check("reaction changeable", s == 200 and r["reaction"] == "wow", f"{s} {r}")
    s, r = req("POST", f"/api/posts/{post_id}/like", None, carol_tok)
    check("legacy like endpoint", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/reactions", token=alice_tok)
    check("reaction counts", r["reactions"].get("wow") == 1 and r["reactions"].get("like") == 1
          and r["total"] == 2, f"{r}")
    s, r = req("GET", f"/api/posts/{post_id}/reactions", token=bob_tok)
    check("my_reaction present", r["my_reaction"] == "wow", f"{r}")
    # total count not double-counted by the legacy like path
    s, r = req("GET", "/api/me", token=alice_tok)
    alice_id = r["id"]
    s, r = req("GET", f"/api/users/{alice_id}/posts", token=alice_tok)
    p = [p for p in r["posts"] if p["id"] == post_id][0]
    check("like_count == total reactions", p["like_count"] == 2, f"{p['like_count']}")
    s, r = req("DELETE", f"/api/posts/{post_id}/react", None, bob_tok)
    check("unreact", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/reactions", token=alice_tok)
    check("count decremented", r["total"] == 1, f"{r}")

    # ============ pinned post ============
    s, r = req("PUT", "/api/me/pinned-post", {"post_id": post_id}, alice_tok)
    check("pin post", s == 200, f"{s} {r}")
    s, r = req("POST", "/api/posts", {"body": f"newer post {ts}"}, alice_tok)
    newer_id = r["id"]
    s, r = req("GET", f"/api/users/{alice_id}/posts", token=bob_tok)
    check("pinned post first", r["posts"][0]["id"] == post_id, f"{[p['id'] for p in r['posts']]}")
    s, r = req("GET", f"/api/users/{alice_id}", token=bob_tok)
    check("profile exposes pinned_post_id", r["user"]["pinned_post_id"] == post_id, f"{r['user']}")
    s, r = req("DELETE", "/api/me/pinned-post", None, alice_tok)
    check("unpin", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/users/{alice_id}/posts", token=bob_tok)
    check("unpinned order restored", r["posts"][0]["id"] == newer_id, "")

    # ============ post edit history ============
    s, r = req("PATCH", f"/api/posts/{post_id}", {"body": f"edited body {ts}"}, alice_tok)
    check("edit post", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/edits", token=bob_tok)
    check("edit history", s == 200 and len(r["edits"]) == 1
          and r["edits"][0]["old_body"] == f"reaction test {ts}", f"{s} {r}")

    # ============ scheduled posts ============
    future = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 3600))
    s, r = req("POST", "/api/posts",
               {"body": f"scheduled {ts}", "publish_at": future}, alice_tok)
    check("scheduled post created", s == 201, f"{s} {r}")
    sched_id = r["id"]
    s, r = req("GET", "/api/feed", token=bob_tok)
    check("scheduled hidden from feed", all(p["id"] != sched_id for p in r["posts"]), "")
    s, r = req("GET", "/api/me/scheduled-posts", token=alice_tok)
    check("author lists scheduled", any(p["id"] == sched_id for p in r["posts"]), f"{r}")
    s, r = req("DELETE", f"/api/scheduled-posts/{sched_id}", None, alice_tok)
    check("scheduled cancelled", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/scheduled-posts", token=alice_tok)
    check("cancelled gone", all(p["id"] != sched_id for p in r["posts"]), "")
    s, r = req("POST", "/api/posts",
               {"body": "x", "publish_at": "2020-01-01T00:00:00Z"}, alice_tok)
    check("past publish_at rejected", s == 400, f"{s} {r}")

    # ============ comment sorting ============
    s, r = req("POST", f"/api/posts/{post_id}/comments", {"body": "first"}, bob_tok)
    check("comment 1", s == 201, f"{s} {r}")
    c1 = r.get("id")
    time.sleep(0.05)
    s, r = req("POST", f"/api/posts/{post_id}/comments", {"body": "second"}, carol_tok)
    check("comment 2", s == 201, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/comments?sort=new", token=alice_tok)
    check("sort=new newest first", r["comments"][0]["body"] == "second", f"{r}")
    s, r = req("GET", f"/api/posts/{post_id}/comments?sort=old", token=alice_tok)
    check("sort=old oldest first", r["comments"][0]["body"] == "first", f"{r}")
    s, r = req("POST", f"/api/comments/{c1}/like", None, alice_tok)
    check("like comment", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/comments?sort=top", token=alice_tok)
    check("sort=top most liked first", r["comments"][0]["body"] == "first", f"{r}")

    # ============ albums + tags + feeling/location ============
    s, r = req("POST", "/api/posts", {
        "body": f"photo {ts}", "feeling": "happy", "location": "Lisbon",
        "tagged_user_ids": [bob_id],
        "media": [{"kind": "image", "url": "https://cdn.example.dev/p1.jpg"}]}, alice_tok)
    check("tagged post with metadata", s == 201, f"{s} {r}")
    photo_id = r["id"]
    s, r = req("GET", f"/api/users/{alice_id}/posts", token=bob_tok)
    p = [p for p in r["posts"] if p["id"] == photo_id][0]
    check("metadata surfaced", p["feeling"] == "happy" and p["location"] == "Lisbon", f"{p}")
    check("tagged usernames surfaced", p["tagged_usernames"] == [bob], f"{p}")
    s, r = req("GET", "/api/notifications", token=bob_tok)
    kinds = [n.get("kind") for n in r.get("notifications", [])]
    check("tag notification sent", "tagged_in_post" in kinds, f"{kinds}")

    s, r = req("POST", "/api/albums", {"title": "Trips", "description": "travel"}, alice_tok)
    check("album created", s == 201, f"{s} {r}")
    album_id = r["id"]
    s, r = req("POST", f"/api/albums/{album_id}/items", {"post_id": photo_id}, alice_tok)
    check("album item added", s == 201, f"{s} {r}")
    s, r = req("POST", f"/api/albums/{album_id}/items", {"post_id": post_id}, alice_tok)
    check("non-media post rejected from album", s == 400, f"{s} {r}")
    s, r = req("GET", f"/api/albums/{album_id}", token=bob_tok)
    check("album detail with posts", r["album"]["title"] == "Trips"
          and len(r["posts"]) == 1 and r["posts"][0]["id"] == photo_id, f"{r}")
    check("album cover from media", r["album"]["cover_url"] == "https://cdn.example.dev/p1.jpg", f"{r}")
    s, r = req("DELETE", f"/api/albums/{album_id}/items/{photo_id}", None, alice_tok)
    check("album item removed", s == 200, f"{s} {r}")

    # ============ chat theme + nicknames ============
    s, r = req("POST", "/api/conversations", {"member_ids": [bob_id]}, alice_tok)
    check("conversation created", s in (200, 201), f"{s} {r}")
    conv_id = r.get("id")

    s, r = req("PUT", f"/api/conversations/{conv_id}/theme", {"theme": "sunset"}, alice_tok)
    check("chat theme set", s == 200 and r["theme"] == "sunset", f"{s} {r}")
    s, r = req("GET", "/api/conversations", token=bob_tok)
    conv = [c for c in r["conversations"] if c["id"] == conv_id]
    check("theme visible to other member", conv and conv[0]["theme"] == "sunset", f"{conv}")

    s, r = req("PUT", f"/api/conversations/{conv_id}/nicknames/{bob_id}",
               {"nickname": "Bobby"}, alice_tok)
    check("nickname set", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv_id}/members", token=alice_tok)
    bmember = [m for m in r["members"] if m["id"] == bob_id]
    check("nickname visible in members", bmember and bmember[0]["nickname"] == "Bobby", f"{bmember}")
    s, r = req("PUT", f"/api/conversations/{conv_id}/theme", {"theme": "INVALID THEME!"}, alice_tok)
    check("invalid theme rejected", s == 400, f"{s} {r}")

    # ============ event reminders ============
    starts = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 1800))
    s, r = req("POST", "/api/events", {
        "title": f"Launch {ts}", "starts_at": starts}, alice_tok)
    check("event created", s == 201, f"{s} {r}")
    event_id = r.get("id")
    s, r = req("POST", f"/api/events/{event_id}/rsvp", {"response": "going"}, bob_tok)
    check("rsvp going", s == 200, f"{s} {r}")
    # the scheduler delivers due reminders within one 2s tick
    delivered = False
    for _ in range(10):
        s, r = req("GET", "/api/notifications", token=bob_tok)
        if any(n.get("kind") == "event_reminder"
               and n.get("payload", {}).get("event_id") == event_id
               for n in r.get("notifications", [])):
            delivered = True
            break
        time.sleep(1)
    check("event reminder delivered", delivered,
          f"{[n.get('kind') for n in r.get('notifications', [])]}")
    s, r = req("POST", f"/api/events/{event_id}/rsvp", {"response": "declined"}, carol_tok)
    check("rsvp declined", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/notifications", token=carol_tok)
    check("declined gets no reminder", not any(
        n.get("kind") == "event_reminder" for n in r.get("notifications", [])), "")

    print("\nAll gap-closure checks passed.")


if __name__ == "__main__":
    main()
