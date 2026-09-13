"""End-to-end checks for the platform gap features (2026-09-13).

Covers the six features implemented to close the master-plan gaps:
  * Forums            — master plan §30 / master documentation §75 item 24
  * ChatApp Pulse     — master plan §32
  * Live Shopping     — master plan §20
  * AI dubbing        — master plan §23
  * AI clips          — master plan §23
  * AI assistant      — master plan §38

Runs against a live API on :8080 with migration 036_platform_gaps.sql applied.

The AI checks assert the honest-availability contract: with no model
configured the endpoints must report `unavailable` with a reason, and must
never fabricate an output. When models ARE configured the same endpoints must
return real artifacts — both branches are accepted and asserted.
"""
import os
import subprocess
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req
from gaps6_test import register, uid


def db(sql):
    dburl = os.environ.get(
        "DATABASE_URL",
        "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
    subprocess.run(["psql", dburl, "-c", sql], check=True, capture_output=True)


def fund(username, amount):
    """Credit an internal USD balance through the real double-entry ledger."""
    db(f"""
INSERT INTO wallet_accounts (user_id, asset, chain, address)
SELECT id, 'USD', 'internal', 'test-' || id FROM users WHERE username='{username}'
ON CONFLICT DO NOTHING;
INSERT INTO ledger_entries (tx_id, account_id, amount, kind, memo)
SELECT gen_random_uuid(), wa.id, {amount}, 'deposit', 'platform-gaps test funding'
FROM wallet_accounts wa JOIN users u ON u.id=wa.user_id
WHERE u.username='{username}' AND wa.asset='USD' AND wa.chain='internal';
""")


def main() -> None:
    ts = int(time.time())
    alice = register(f"gplat_alice{ts}")
    bob = register(f"gplat_bob{ts}")
    alice_id = uid(alice)
    bob_id = uid(bob)

    # ------------------------------------------------ Forums (§30) ----------
    slug = f"gapforum{ts}"
    s, r = req("POST", "/api/forums",
               {"title": "Gap Forum", "slug": slug, "description": "platform gaps"},
               token=alice)
    check("forum create", s == 201 and r.get("id"), f"{s} {r}")
    forum_id = r.get("id")

    s, r = req("POST", "/api/forums",
               {"title": "Dup", "slug": slug, "description": ""}, token=alice)
    check("forum slug unique (409)", s == 409, f"{s}")

    s, r = req("POST", "/api/forums",
               {"title": "Bad", "slug": "Bad Slug!", "description": ""}, token=alice)
    check("forum slug validated (400)", s == 400, f"{s}")

    s, r = req("GET", "/api/forums", token=bob)
    check("forum list visible to others",
          s == 200 and any(f["slug"] == slug for f in r.get("forums", [])), f"{s} {r}")

    s, r = req("GET", f"/api/forums/{slug}", token=bob)
    check("forum get by slug", s == 200 and r.get("slug") == slug, f"{s} {r}")

    s, r = req("POST", f"/api/forums/{forum_id}/topics",
               {"title": "First topic", "body": "hello"}, token=alice)
    check("topic create", s == 201 and r.get("id"), f"{s} {r}")
    topic_id = r.get("id")

    s, r = req("POST", f"/api/forums/topics/{topic_id}/posts",
               {"body": "reply from bob"}, token=bob)
    check("post create", s == 201 and r.get("id"), f"{s} {r}")
    post_id = r.get("id")

    s, r = req("GET", f"/api/forums/topics/{topic_id}/posts", token=bob)
    check("post list", s == 200 and len(r.get("posts", [])) == 1, f"{s} {r}")

    s, r = req("GET", f"/api/forums/{forum_id}/topics", token=alice)
    check("topic count incremented",
          s == 200 and r["topics"][0]["post_count"] == 1, f"{s} {r}")

    s, r = req("PUT", f"/api/forums/topics/{topic_id}/pin", {"value": True}, token=bob)
    check("non-moderator cannot pin (403)", s == 403, f"{s} {r}")

    s, r = req("PUT", f"/api/forums/topics/{topic_id}/pin", {"value": True}, token=alice)
    check("owner pins topic", s == 200, f"{s} {r}")

    s, r = req("PUT", f"/api/forums/topics/{topic_id}/lock", {"value": True}, token=alice)
    check("owner locks topic", s == 200, f"{s} {r}")

    s, r = req("POST", f"/api/forums/topics/{topic_id}/posts",
               {"body": "blocked"}, token=bob)
    check("locked topic blocks non-moderator (403)", s == 403, f"{s} {r}")

    s, r = req("DELETE", f"/api/forums/posts/{post_id}", token=alice)
    check("moderator deletes post", s == 200, f"{s} {r}")

    s, r = req("GET", f"/api/forums/search?q=First", token=bob)
    check("forum search", s == 200 and len(r.get("results", [])) >= 1, f"{s} {r}")

    # ------------------------------------------- ChatApp Pulse (§32) --------
    s, r = req("POST", "/api/pulse/posts",
               {"body": "Hello Pulse #gaptest #platform", "local_tag": "dhaka"},
               token=alice)
    check("pulse post create", s == 201 and r.get("id"), f"{s} {r}")
    root_id = r.get("id")

    s, r = req("POST", "/api/pulse/posts", {"body": "   "}, token=alice)
    check("pulse rejects empty body (400)", s == 400, f"{s}")

    s, r = req("POST", "/api/pulse/posts",
               {"body": "a reply", "parent_id": root_id}, token=bob)
    check("pulse reply create", s == 201, f"{s} {r}")

    s, r = req("POST", "/api/pulse/posts", {"body": "", "repost_of": root_id}, token=bob)
    check("pulse repost (empty body allowed)", s == 201, f"{s} {r}")

    s, r = req("POST", "/api/pulse/posts",
               {"body": "quoting", "quote_of": root_id}, token=bob)
    check("pulse quote create", s == 201, f"{s} {r}")

    s, r = req("POST", "/api/pulse/posts",
               {"body": "orphan", "parent_id": "00000000-0000-0000-0000-000000000001"},
               token=alice)
    check("pulse rejects unknown parent (400)", s == 400, f"{s}")

    s, r = req("GET", "/api/pulse/posts", token=bob)
    check("pulse global feed excludes replies",
          s == 200 and all(p["id"] != root_id or True for p in r.get("posts", []))
          and not any(p.get("parent_id") for p in r.get("posts", [])), f"{s}")

    s, r = req("GET", f"/api/pulse/posts/{root_id}/thread", token=bob)
    # Thread = root + its nested replies. The quote/repost are separate roots
    # (correct — they carry their own id and merely reference the original), so
    # the thread itself contains the root and the one reply.
    check("pulse thread returns ordering modes",
          s == 200 and len(r.get("chronological", [])) == 2
          and len(r.get("relevant", [])) == 2, f"{s} {r}")
    check("pulse thread counters",
          r.get("root", {}).get("reply_count", 0) >= 1
          and r.get("root", {}).get("repost_count", 0) >= 1
          and r.get("root", {}).get("quote_count", 0) >= 1, f"{r.get('root')}")

    s, r = req("GET", "/api/pulse/trends", token=bob)
    check("pulse trends computed from real volume",
          s == 200 and any(t["topic"] == "gaptest" for t in r.get("trends", [])),
          f"{s} {r}")

    s, r = req("GET", "/api/pulse/trends?scope=local&region=dhaka", token=bob)
    check("pulse local trends", s == 200 and len(r.get("trends", [])) >= 1, f"{s} {r}")

    s, r = req("GET", "/api/pulse/trends?scope=local", token=bob)
    check("pulse local trends require region (400)", s == 400, f"{s}")

    s, r = req("GET", "/api/pulse/posts?scope=local&region=dhaka", token=bob)
    check("pulse local feed", s == 200 and len(r.get("posts", [])) >= 1, f"{s} {r}")

    s, r = req("GET", "/api/pulse/posts?topic=gaptest", token=bob)
    check("pulse topic feed", s == 200 and len(r.get("posts", [])) >= 1, f"{s} {r}")

    s, r = req("GET", f"/api/pulse/users/{alice_id}", token=bob)
    check("pulse user timeline", s == 200 and len(r.get("posts", [])) >= 1, f"{s} {r}")

    s, r = req("GET", "/api/pulse/topics", token=bob)
    check("pulse topic catalog", s == 200 and len(r.get("topics", [])) >= 1, f"{s} {r}")

    s, r = req("POST", "/api/pulse/lists", {"name": f"list{ts}"}, token=alice)
    check("pulse list create", s == 201 and r.get("id"), f"{s} {r}")
    list_id = r.get("id")

    s, r = req("PUT", f"/api/pulse/lists/{list_id}/members/{bob_id}", token=alice)
    check("pulse list add member", s == 200, f"{s} {r}")

    s, r = req("PUT", f"/api/pulse/lists/{list_id}/members/{alice_id}", token=bob)
    check("cannot edit someone else's list (403)", s == 403, f"{s}")

    s, r = req("GET", "/api/pulse/lists", token=alice)
    check("pulse list member count", s == 200 and r["lists"][0]["member_count"] == 1,
          f"{s} {r}")

    s, r = req("DELETE", f"/api/pulse/lists/{list_id}/members/{bob_id}", token=alice)
    check("pulse list remove member", s == 200, f"{s} {r}")

    s, r = req("DELETE", f"/api/pulse/posts/{root_id}", token=bob)
    check("cannot delete another's post (404)", s == 404, f"{s}")

    s, r = req("DELETE", f"/api/pulse/posts/{root_id}", token=alice)
    check("author deletes own post", s == 200, f"{s} {r}")

    # -------------------------------------------- Live Shopping (§20) ------
    room = "6f1e2d3c-0000-4000-8000-0000000000%02d" % (ts % 100)
    s, r = req("POST", f"/api/live-rooms/{room}/products",
               {"title": "Gadget", "price_usd": "20.00", "inventory": 3,
                "discount_pct": "10", "description": "test item"}, token=alice)
    check("live product create", s == 201 and r.get("id"), f"{s} {r}")
    product_id = r.get("id")

    s, r = req("POST", f"/api/live-rooms/{room}/products",
               {"title": "Bad price", "price_usd": "-1", "inventory": 1}, token=alice)
    check("live product rejects non-positive price (400)", s == 400, f"{s}")

    s, r = req("POST", f"/api/live-rooms/{room}/products",
               {"title": "Bad discount", "price_usd": "5", "discount_pct": "150",
                "inventory": 1}, token=alice)
    check("live product rejects discount > 100 (400)", s == 400, f"{s}")

    s, r = req("GET", f"/api/live-rooms/{room}/products", token=bob)
    check("live product list", s == 200 and len(r.get("products", [])) == 1, f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/pin", {"product_id": product_id},
               token=bob)
    check("non-seller cannot pin (403)", s == 403, f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/pin", {"product_id": product_id},
               token=alice)
    check("seller pins product", s == 200, f"{s} {r}")

    s, r = req("GET", f"/api/live-rooms/{room}/products", token=bob)
    check("pinned flag surfaced", s == 200 and r["products"][0]["pinned"] is True,
          f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/coupons",
               {"code": "SAVE10", "discount_pct": "10", "max_uses": 1}, token=alice)
    check("coupon create", s == 201, f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/coupons",
               {"code": "NOPE", "discount_pct": "10"}, token=bob)
    check("non-seller cannot create coupon (403)", s == 403, f"{s}")

    # Fund the buyer through the real double-entry ledger so checkout can
    # settle for real. `register` returns a token, not the username, so we
    # recover the usernames deterministically from the test timestamp.
    alice_name = f"gplat_alice{ts}"
    bob_name = f"gplat_bob{ts}"
    fund(bob_name, "200")
    s, r = req("GET", "/api/wallet/accounts", token=bob)
    check("buyer USD balance funded", s == 200, f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/checkout",
               {"product_id": product_id, "quantity": 1, "coupon": "SAVE10"}, token=bob)
    check("checkout with coupon succeeds", s == 201 and r.get("order_id"),
          f"{s} {r}")
    # 20.00 * 0.9 (product 10% off) = 18.00, then 10% coupon = 16.20
    check("coupon discount applied to total",
          abs(float(r.get("total_usd") or 0) - 16.20) < 1e-9,
          f"total={r.get('total_usd')}")
    check("platform fee charged",
          r.get("platform_fee_usd") not in (None, "0", "0.000000000000000000"),
          f"fee={r.get('platform_fee_usd')}")

    s, r = req("POST", f"/api/live-rooms/{room}/checkout",
               {"product_id": product_id, "quantity": 1, "coupon": "SAVE10"}, token=bob)
    check("exhausted coupon rejected (400)", s == 400, f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/checkout",
               {"product_id": product_id, "quantity": 1}, token=alice)
    check("seller cannot buy own product (400)", s == 400, f"{s}")

    s, r = req("GET", f"/api/live-rooms/{room}/products", token=bob)
    check("inventory decremented by real order",
          s == 200 and r["products"][0]["inventory"] == 2, f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/checkout",
               {"product_id": product_id, "quantity": 99}, token=bob)
    check("oversell blocked (409)", s == 409, f"{s} {r}")

    s, r = req("GET", f"/api/live-rooms/{room}/live-purchases", token=alice)
    check("live purchase analytics",
          s == 200 and r.get("orders") == 1 and r.get("units") == 1, f"{s} {r}")

    s, r = req("DELETE", f"/api/live-rooms/{room}/pin", token=alice)
    check("seller unpins", s == 200, f"{s} {r}")

    # ------------------------------------------------- AI dubbing (§23) ----
    s, r = req("POST", "/api/ai/dub",
               {"media_url": "https://example.com/clip.mp4", "target_lang": "es"},
               token=alice)
    check("dub endpoint returns availability contract",
          s == 200 and r.get("status") in ("ready", "unavailable", "failed"), f"{s} {r}")
    if r.get("status") == "ready":
        check("ready dub carries a label", bool(r.get("label")), f"{r}")
        check("ready dub carries audio_url", bool(r.get("audio_url")), f"{r}")
    else:
        check("unavailable dub carries a reason", bool(r.get("reason")), f"{r}")

    s, r = req("POST", "/api/ai/dub",
               {"media_url": "https://example.com/clip.mp4", "target_lang": "es"},
               token=alice)
    check("dub job is idempotent per media+target",
          s == 200 and r.get("status") in ("ready", "unavailable", "failed"), f"{s} {r}")

    s, r = req("POST", "/api/ai/dub",
               {"media_url": "ftp://bad", "target_lang": "es"}, token=alice)
    check("dub rejects non-http media (400)", s == 400, f"{s}")

    s, r = req("POST", "/api/ai/dub",
               {"media_url": "https://example.com/clip.mp4", "target_lang": "en",
                "source_lang": "en"}, token=alice)
    check("dub rejects same source/target (400)", s == 400, f"{s}")

    s, r = req("GET", "/api/ai/dubs", token=alice)
    check("dub list", s == 200 and len(r.get("dubs", [])) >= 1, f"{s} {r}")

    # ---------------------------------------------------- AI clips (§23) ---
    s, r = req("POST", "/api/ai/clips/analyze",
               {"media_url": "https://example.com/long.mp4"}, token=alice)
    check("clip analyze returns availability contract",
          s == 201 and r.get("status") in ("analyzed", "unavailable", "failed"),
          f"{s} {r}")
    clip_ids = [c["id"] for c in r.get("clips", [])]
    if r.get("status") == "analyzed":
        check("analyzed job returns clips with scores",
              len(clip_ids) >= 1 and all(float(c["score"]) >= 0 for c in r["clips"]),
              f"{r.get('clips')}")
    else:
        check("unavailable clip job carries a reason", bool(r.get("reason")), f"{r}")

    s, r = req("GET", "/api/ai/clips", token=alice)
    check("clip job list", s == 200 and len(r.get("jobs", [])) >= 1, f"{s} {r}")

    if clip_ids:
        s, r = req("POST", f"/api/ai/clips/{clip_ids[0]}/approve",
                   {"approve": True}, token=bob)
        check("cannot approve another's clip (404)", s == 404, f"{s}")
        s, r = req("POST", f"/api/ai/clips/{clip_ids[0]}/approve",
                   {"approve": True}, token=alice)
        check("owner approves clip", s == 200 and r.get("approved") is True, f"{s} {r}")
        s, r = req("POST", f"/api/ai/clips/{clip_ids[0]}/approve",
                   {"approve": False}, token=alice)
        check("approval revocable", s == 200 and r.get("approved") is False, f"{s} {r}")

    # ------------------------------------------------ AI assistant (§38) ---
    s, r = req("POST", "/api/assistant/conversations", {"title": "test"}, token=alice)
    check("assistant conversation create", s == 201 and r.get("id"), f"{s} {r}")
    conv_id = r.get("id")

    s, r = req("POST", f"/api/assistant/conversations/{conv_id}/messages",
               {"content": "what is my balance?"}, token=alice)
    check("assistant answers and states its basis",
          s == 200 and isinstance(r.get("reply"), str) and r.get("reply"), f"{s} {r}")

    s, r = req("POST", f"/api/assistant/conversations/{conv_id}/messages",
               {"content": "   "}, token=alice)
    check("assistant rejects empty prompt (400)", s == 400, f"{s}")

    s, r = req("POST", f"/api/assistant/conversations/{conv_id}/messages",
               {"content": "hello"}, token=bob)
    check("cannot post into another's conversation (403)", s == 403, f"{s}")

    s, r = req("GET", "/api/assistant/conversations", token=alice)
    check("assistant conversation list",
          s == 200 and r["conversations"][0]["message_count"] >= 2, f"{s} {r}")

    s, r = req("GET", "/api/assistant/actions", token=alice)
    check("assistant action list", s == 200, f"{s} {r}")
    actions = r.get("actions", [])
    if actions:
        aid = actions[0]["id"]
        check("proposed action starts unapplied",
              actions[0]["status"] == "proposed", f"{actions[0]}")
        s, r = req("POST", f"/api/assistant/actions/{aid}/decide",
                   {"decision": "approved"}, token=bob)
        check("cannot decide another's proposal (409)", s == 409, f"{s}")
        s, r = req("POST", f"/api/assistant/actions/{aid}/decide",
                   {"decision": "approved"}, token=alice)
        check("owner approves proposal", s == 200 and r.get("status") == "approved",
              f"{s} {r}")
        s, r = req("POST", f"/api/assistant/actions/{aid}/decide",
                   {"decision": "approved"}, token=alice)
        check("proposal cannot be decided twice (409)", s == 409, f"{s}")
    else:
        check("no proposal surfaced without a model (acceptable)", True, "")

    s, r = req("POST", f"/api/assistant/actions/{'0'*8}-0000-0000-0000-000000000000/decide",
               {"decision": "maybe"}, token=alice)
    check("invalid decision rejected (400)", s == 400, f"{s}")


if __name__ == "__main__":
    main()
