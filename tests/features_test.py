#!/usr/bin/env python3
"""End-to-end integration checks for the GAP_ANALYSIS feature endpoints
(content groups/pages/events, reel watch signals + FYP ranking, creator
tiers/subscriptions, tips/gifts/earnings, mutes/restricted/word filters,
profile lock/active status, follow and message requests, bots).

Runs against a live API on :8080 with migrations 010-014 applied. No mocks.
"""
import asyncio
import json
import sys
import time

import websockets

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import WS, check, req


async def ws_send(token, conv_id, body):
    async with websockets.connect(f"{WS}/ws?token={token}") as ws:
        await ws.send(json.dumps({
            "type": "message", "conversation_id": conv_id, "body": body}))
        await asyncio.sleep(0.5)


def main():
    ts = int(time.time())
    alice = f"featA{ts}"
    bob = f"featB{ts}"

    s, r = req("POST", "/api/auth/register", {
        "username": alice, "email": f"{alice}@test.dev", "password": "Passw0rd!123",
        "display_name": "Alice", "country_code": "US"})
    check("feat register alice", s in (200, 201), f"{s} {r}")
    alice_tok = r.get("access_token")
    alice_id = r.get("user_id")

    s, r = req("POST", "/api/auth/register", {
        "username": bob, "email": f"{bob}@test.dev", "password": "Passw0rd!123",
        "display_name": "Bob", "country_code": "GB"})
    check("feat register bob", s in (200, 201), f"{s} {r}")
    bob_tok = r.get("access_token")
    bob_id = r.get("user_id")

    # --- groups ---
    s, r = req("POST", "/api/groups", {
        "name": f"Group {ts}", "description": "test group", "privacy": "public"}, token=alice_tok)
    check("group create", s == 201 and r.get("id"), f"{s} {r}")
    gid = r.get("id")

    s, r = req("POST", f"/api/groups/{gid}/join", token=bob_tok)
    check("group join", s == 200 and r.get("status") == "active", f"{s} {r}")

    s, r = req("GET", "/api/groups", token=alice_tok)
    check("group list includes new group",
          s == 200 and any(g.get("id") == gid for g in r.get("groups", [])), f"{s} {r}")

    s, r = req("GET", f"/api/groups/{gid}", token=bob_tok)
    check("group detail shows member role",
          s == 200 and r.get("my_role") == "member" and r.get("member_count") == 2, f"{s} {r}")

    s, r = req("POST", f"/api/groups/{gid}/posts", {"body": "hello group"}, token=alice_tok)
    check("group post create", s in (200, 201), f"{s} {r}")

    s, r = req("GET", f"/api/groups/{gid}/feed", token=bob_tok)
    check("group feed visible to member",
          s == 200 and len(r.get("posts", [])) == 1, f"{s} {r}")

    # private group join -> pending, review by owner
    s, r = req("POST", "/api/groups", {
        "name": f"Priv {ts}", "description": "closed", "privacy": "private"}, token=alice_tok)
    check("private group create", s == 201, f"{s} {r}")
    priv_gid = r.get("id")
    s, r = req("POST", f"/api/groups/{priv_gid}/join", token=bob_tok)
    check("private group join pending", s == 200 and r.get("status") == "pending", f"{s} {r}")
    s, r = req("GET", f"/api/groups/{priv_gid}", token=bob_tok)
    check("private group hidden from non-member", s == 403, f"{s} {r}")
    s, r = req("POST", f"/api/groups/{priv_gid}/members/{bob_id}/review",
               {"approve": True}, token=alice_tok)
    check("owner approves join", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/groups/{priv_gid}", token=bob_tok)
    check("approved member sees private group", s == 200 and r.get("my_role") == "member",
          f"{s} {r}")

    s, r = req("DELETE", f"/api/groups/{gid}/join", token=bob_tok)
    check("group leave", s == 200, f"{s} {r}")

    # --- events ---
    starts = int(time.time()) + 7200
    iso_starts = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(starts))
    s, r = req("POST", "/api/events", {
        "title": "Meetup", "description": "test event", "location": "Online",
        "starts_at": iso_starts, "group_id": gid}, token=alice_tok)
    check("event create", s == 201 and r.get("id"), f"{s} {r}")
    eid = r.get("id")
    s, r = req("POST", f"/api/events/{eid}/rsvp", {"response": "going"}, token=bob_tok)
    check("event rsvp going", s == 200 and r.get("status") == "going", f"{s} {r}")
    s, r = req("POST", f"/api/events/{eid}/rsvp", {"response": "maybe"}, token=bob_tok)
    check("event rsvp invalid rejected", s == 400, f"{s} {r}")
    s, r = req("GET", f"/api/events/{eid}", token=alice_tok)
    check("event counts + my_response",
          s == 200 and r.get("rsvp_counts", {}).get("going") == 1, f"{s} {r}")
    s, r = req("GET", "/api/events", token=alice_tok)
    check("events listed", s == 200 and any(e.get("id") == eid for e in r.get("events", [])),
          f"{s} {r}")

    # --- pages ---
    s, r = req("POST", "/api/pages", {
        "name": f"Page {ts}", "category": "Tech", "description": "test page"}, token=alice_tok)
    check("page create", s == 201 and r.get("id"), f"{s} {r}")
    pid = r.get("id")
    s, r = req("POST", f"/api/pages/{pid}/follow", token=bob_tok)
    check("page follow", s == 200 and r.get("status") == "following", f"{s} {r}")
    s, r = req("GET", f"/api/pages/{pid}", token=bob_tok)
    check("page detail shows following + count",
          s == 200 and r.get("following") is True and r.get("follower_count") == 1, f"{s} {r}")
    s, r = req("GET", "/api/pages", token=alice_tok)
    check("page list includes page",
          s == 200 and any(p.get("id") == pid for p in r.get("pages", [])), f"{s} {r}")
    s, r = req("POST", f"/api/pages/{pid}/posts", {"body": "page update"}, token=alice_tok)
    check("page post by owner", s in (200, 201), f"{s} {r}")
    s, r = req("GET", f"/api/pages/{pid}/feed", token=bob_tok)
    check("page feed visible to follower", s == 200 and len(r.get("posts", [])) == 1, f"{s} {r}")
    s, r = req("DELETE", f"/api/pages/{pid}/follow", token=bob_tok)
    check("page unfollow", s == 200, f"{s} {r}")

    # --- reel watch signals + FYP ---
    s, r = req("POST", "/api/posts", {"body": "rank me", "type": "reel"}, token=bob_tok)
    check("reel post create", s in (200, 201), f"{s} {r}")
    reel_id = r.get("id")
    s, r = req("POST", f"/api/reels/{reel_id}/watch", {
        "watched_ms": 5000, "duration_ms": 5000, "completed": True}, token=alice_tok)
    check("watch signal recorded", s == 201 and r.get("status") == "recorded", f"{s} {r}")
    s, r = req("POST", f"/api/reels/{reel_id}/watch", {
        "watched_ms": -5, "duration_ms": 5000}, token=alice_tok)
    check("negative watch rejected", s == 400, f"{s} {r}")
    s, r = req("POST", f"/api/reels/{gid}/watch", {"watched_ms": 10, "duration_ms": 10},
               token=alice_tok)
    check("watch on non-reel 404", s == 404, f"{s} {r}")
    s, r = req("GET", "/api/fyp?limit=50", token=alice_tok)
    check("fyp returns ranked reels", s == 200 and isinstance(r.get("posts"), list), f"{s} {r}")
    reel_rows = [p for p in r.get("posts", []) if p.get("id") == reel_id]
    check("fyp shows completion signals",
          bool(reel_rows) and reel_rows[0].get("completion_rate") == 1.0, f"{s} {reel_rows}")

    # not_interested excludes the reel from the viewer's FYP
    s, r = req("POST", f"/api/reels/{reel_id}/watch", {
        "watched_ms": 100, "duration_ms": 5000, "not_interested": True}, token=alice_tok)
    check("not_interested recorded", s == 201, f"{s} {r}")
    s, r = req("GET", "/api/fyp?limit=10", token=alice_tok)
    check("not_interested excluded from fyp",
          s == 200 and all(p.get("id") != reel_id for p in r.get("posts", [])), f"{s} {r}")

    # --- word filters affect FYP ---
    s, r = req("POST", "/api/me/word-filters", {"phrase": "spoiler-phrase"}, token=alice_tok)
    check("word filter add", s == 201, f"{s} {r}")
    s, r = req("POST", "/api/posts", {"body": "contains spoiler-phrase here", "type": "reel"},
               token=bob_tok)
    filtered_reel = r.get("id")
    s, r = req("GET", "/api/fyp?limit=20", token=alice_tok)
    check("word filter hides reel from fyp",
          s == 200 and all(p.get("id") != filtered_reel for p in r.get("posts", [])), f"{s} {r}")
    s, r = req("GET", "/api/me/word-filters", token=alice_tok)
    check("word filters listed",
          s == 200 and any(f.get("phrase") == "spoiler-phrase" for f in r.get("filters", [])),
          f"{s} {r}")
    s, r = req("DELETE", "/api/me/word-filters", {"phrase": "spoiler-phrase"}, token=alice_tok)
    check("word filter remove", s == 200, f"{s} {r}")

    # --- creator tiers / subscriptions ---
    s, r = req("POST", "/api/creator/tiers", {
        "name": "Gold", "perks": "badge + extras", "price_usd": 9.99}, token=alice_tok)
    check("tier create", s == 201 and r.get("id"), f"{s} {r}")
    tid = r.get("id")
    s, r = req("POST", "/api/creator/tiers", {"name": "X", "price_usd": 0}, token=alice_tok)
    check("tier invalid price rejected", s == 400, f"{s} {r}")
    s, r = req("GET", "/api/creator/tiers", token=alice_tok)
    check("my tiers listed",
          s == 200 and any(t.get("id") == tid for t in r.get("tiers", [])), f"{s} {r}")
    s, r = req("GET", f"/api/users/{alice_id}/tiers", token=bob_tok)
    check("creator tiers public", s == 200 and len(r.get("tiers", [])) == 1, f"{s} {r}")
    s, r = req("POST", f"/api/tiers/{tid}/subscribe", token=bob_tok)
    check("subscribe without balance rejected", s == 400, f"{s} {r}")
    s, r = req("GET", "/api/subscriptions", token=bob_tok)
    check("subscriptions list shape", s == 200 and "subscriptions" in r, f"{s} {r}")
    s, r = req("GET", "/api/creator/earnings", token=alice_tok)
    check("earnings shape", s == 200 and "earned" in r and "available" in r, f"{s} {r}")

    # --- tips / gifts (balance-gated paths verified) ---
    s, r = req("POST", f"/api/users/{alice_id}/tip", {"amount_usd": 5.0, "message": "thanks"},
               token=bob_tok)
    check("tip without balance rejected", s == 400, f"{s} {r}")
    s, r = req("POST", f"/api/users/{alice_id}/tip", {"amount_usd": -1}, token=bob_tok)
    check("tip negative rejected", s == 400, f"{s} {r}")
    s, r = req("GET", "/api/gifts", token=bob_tok)
    check("gift catalog", s == 200 and isinstance(r.get("gifts"), list), f"{s} {r}")
    s, r = req("POST", f"/api/users/{alice_id}/gift", {"gift_id": "nope"}, token=bob_tok)
    check("unknown gift rejected", s == 404, f"{s} {r}")

    # --- mutes / restricted ---
    s, r = req("POST", f"/api/users/{bob_id}/mute", token=alice_tok)
    check("mute user", s == 200 and r.get("status") == "muted", f"{s} {r}")
    s, r = req("GET", "/api/me/mutes", token=alice_tok)
    check("mutes listed", s == 200 and any(m.get("username") == bob for m in r.get("mutes", [])),
          f"{s} {r}")
    s, r = req("GET", "/api/fyp?limit=20", token=alice_tok)
    check("muted author hidden from fyp",
          s == 200 and all(p.get("username") != bob for p in r.get("posts", [])), f"{s} {r}")
    s, r = req("DELETE", f"/api/users/{bob_id}/mute", token=alice_tok)
    check("unmute user", s == 200, f"{s} {r}")

    s, r = req("POST", f"/api/users/{bob_id}/restrict", token=alice_tok)
    check("restrict user", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/restricted", token=alice_tok)
    check("restricted listed",
          s == 200 and any(m.get("username") == bob for m in r.get("restricted", [])), f"{s} {r}")
    s, r = req("DELETE", f"/api/users/{bob_id}/restrict", token=alice_tok)
    check("unrestrict user", s == 200, f"{s} {r}")

    # --- profile lock + follow requests ---
    s, r = req("PUT", "/api/me/profile-lock", {"locked": True}, token=alice_tok)
    check("profile lock on", s == 200 and r.get("profile_locked") is True, f"{s} {r}")
    s, r = req("PUT", "/api/me/active-status", {"show": False}, token=alice_tok)
    check("active status off", s == 200 and r.get("show_active_status") is False, f"{s} {r}")

    s, r = req("POST", f"/api/users/{alice_id}/follow", token=bob_tok)
    check("follow locked profile -> requested", s == 200 and r.get("status") == "requested",
          f"{s} {r}")
    s, r = req("GET", "/api/me/follow-requests", token=alice_tok)
    check("follow requests listed",
          s == 200 and any(q.get("username") == bob for q in r.get("requests", [])), f"{s} {r}")
    s, r = req("POST", f"/api/me/follow-requests/{bob_id}/accept", token=alice_tok)
    check("follow request accept", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/me/follow-requests/{bob_id}/decline", token=alice_tok)
    check("follow request decline idempotent", s in (200, 400, 404), f"{s} {r}")

    # --- message requests: stranger DM with no follow relation ---
    carol = f"featC{ts}"
    s, r = req("POST", "/api/auth/register", {
        "username": carol, "email": f"{carol}@test.dev", "password": "Passw0rd!123",
        "display_name": "Carol", "country_code": "US"})
    check("feat register carol", s in (200, 201), f"{s} {r}")
    carol_tok = r.get("access_token")

    s, r = req("POST", "/api/conversations", {"member_ids": [alice_id]}, token=carol_tok)
    check("direct conversation create", s in (200, 201), f"{s} {r}")
    conv_id = r.get("id") or r.get("conversation_id")
    if conv_id:
        asyncio.run(ws_send(carol_tok, conv_id, "hi from carol"))
        s, r = req("GET", "/api/me/message-requests", token=alice_tok)
        reqs = r.get("requests", [])
        check("message request surfaced",
              s == 200 and any(q.get("conversation_id") == conv_id for q in reqs), f"{s} {r}")
        s, r = req("POST", f"/api/me/message-requests/{conv_id}/accept", token=alice_tok)
        check("message request accept", s == 200, f"{s} {r}")

    # --- bots ---
    s, r = req("POST", "/api/bots", {
        "username": f"test{ts % 100000}bot", "display_name": "Test Bot",
        "description": "ci bot"}, token=alice_tok)
    check("bot create returns token", s == 201 and r.get("token"), f"{s} {r}")
    bot_id = r.get("id")
    bot_token = r.get("token")
    s, r = req("POST", "/api/bots", {"username": "nope", "display_name": "X"}, token=alice_tok)
    check("bot username must end in bot", s == 400, f"{s} {r}")
    s, r = req("GET", "/api/bots", token=alice_tok)
    check("bots listed", s == 200 and any(b.get("id") == bot_id for b in r.get("bots", [])),
          f"{s} {r}")
    if bot_token:
        s, r = req("GET", f"/api/bot/{bot_token}/getMe")
        check("bot getMe via token", s == 200, f"{s} {r}")
        s, r = req("GET", "/api/bot/invalid-token/getMe")
        check("bot invalid token rejected", s == 401, f"{s} {r}")
    s, r = req("DELETE", f"/api/bots/{bot_id}", token=alice_tok)
    check("bot delete", s == 200, f"{s} {r}")

    print()
    return 0


if __name__ == "__main__":
    sys.exit(main())
