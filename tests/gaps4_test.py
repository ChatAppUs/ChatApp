#!/usr/bin/env python3
"""End-to-end checks for gap-pack-4 (migration 019): drafts, topics/interests,
verified organizations, who-to-follow, audio rooms (Spaces), premium plans,
GIF catalog + GIF/contact messages, message entities, channel discussion
groups + stats, anonymous admins, sounds library, share ledger, paywalled
series, content ratings, marketplace, fundraisers, restricted mode, family
pairing, XP/levels, people nearby + group discovery, chat export, screenshot
alerts, bot invoices, inline bots, live gifts + leaderboard, creator
marketplace, professional dashboard.

Runs against a live API on :8080 with migration 019 applied. No mocks.
"""
import asyncio
import json
import sys
import time

import websockets

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import integration_test
from integration_test import WS, check, req, grant_superadmin
from finance_test import db, fund


def register(name):
    for attempt in range(6):
        s, r = req("POST", "/api/auth/register", {
            "username": name, "email": f"{name}@test.dev", "password": "Passw0rd!123",
            "country_code": "US"})
        if s == 429:
            time.sleep(12)
            continue
        check(f"register {name}", s in (200, 201), f"{s} {r}")
        return r.get("access_token")
    check(f"register {name}", False, "persistent 429")
    return None


def uid(tok):
    s, r = req("GET", "/api/me", token=tok)
    return r.get("id") or r.get("user", {}).get("id")


def make_dm(tok_a, tok_b):
    _, ra = req("GET", "/api/me", token=tok_b)
    b_id = ra.get("id") or ra.get("user", {}).get("id")
    s, r = req("POST", "/api/conversations", {"member_ids": [b_id]}, token=tok_a)
    return r.get("id") or r.get("conversation", {}).get("id")


def main():
    ts = int(time.time())
    alice = register(f"g4a{ts}")
    bob = register(f"g4b{ts}")
    carol = register(f"g4c{ts}")
    alice_id, bob_id, carol_id = uid(alice), uid(bob), uid(carol)
    check("user ids", bool(alice_id and bob_id and carol_id), "")

    # --- drafts (X/TikTok) ---
    s, r = req("POST", "/api/me/drafts",
               {"body": "draft hello", "media": [{"kind": "image", "url": "m1"}]}, token=alice)
    check("create draft", s == 201 and r.get("id"), f"{s} {r}")
    draft_id = r.get("id")
    s, r = req("GET", "/api/me/drafts", token=alice)
    check("list drafts", s == 200 and any(d.get("id") == draft_id for d in r.get("drafts", [])),
          f"{s} {r}")
    s, r = req("DELETE", f"/api/me/drafts/{draft_id}", token=alice)
    check("delete draft", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/drafts", token=alice)
    check("draft gone", s == 200 and not any(d.get("id") == draft_id for d in r.get("drafts", [])),
          f"{s} {r}")

    # --- topics / interests (X) ---
    s, r = req("POST", "/api/topics", {"name": f"g4space{ts}", "description": "space talk"},
               token=alice)
    check("create topic", s == 201 and r.get("id"), f"{s} {r}")
    topic_id = r.get("id")
    s, r = req("POST", f"/api/topics/{topic_id}/follow", {}, token=bob)
    check("follow topic", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/topics", token=carol)
    check("topics directory", s == 200 and any(t.get("id") == topic_id for t in r.get("topics", [])),
          f"{s} {r}")
    s, r = req("DELETE", f"/api/topics/{topic_id}/follow", token=bob)
    check("unfollow topic", s == 200, f"{s} {r}")

    # --- verified organizations (X) ---
    s, r = req("POST", "/api/organizations", {"name": f"Acme {ts}", "handle": f"acme{ts}"},
               token=alice)
    check("create org", s == 201 and r.get("id"), f"{s} {r}")
    org_id = r.get("id")
    s, r = req("POST", f"/api/organizations/{org_id}/members", {"username": f"g4b{ts}"},
               token=alice)
    check("org add member", s in (200, 201), f"{s} {r}")
    grant_superadmin(f"g4c{ts}")
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"g4c{ts}@test.dev", "password": "Passw0rd!123"})
    check("admin login", s == 200 and r.get("access_token"), f"{s} {r}")
    admin_tok = r.get("access_token")
    s, r = req("POST", f"/api/admin/organizations/{org_id}/verify", {}, token=admin_tok)
    check("admin verify org", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/organizations/{org_id}", token=bob)
    check("org shows verified", s == 200 and r.get("is_verified") is True, f"{s} {r}")
    s, r = req("DELETE", f"/api/organizations/{org_id}/members/{bob_id}", token=alice)
    check("org remove member", s == 200, f"{s} {r}")

    # --- who-to-follow ---
    s, r = req("POST", f"/api/users/{carol_id}/follow", {}, token=alice)
    check("alice follows carol", s in (200, 201), f"{s} {r}")
    s, r = req("POST", f"/api/users/{bob_id}/follow", {}, token=carol)
    check("carol follows bob", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/me/suggestions", token=alice)
    check("who-to-follow suggests bob",
          s == 200 and any(x.get("username") == f"g4b{ts}" for x in r.get("suggestions", [])),
          f"{s} {r}")

    # --- audio rooms (Spaces) ---
    s, r = req("POST", "/api/audio-rooms", {"title": f"g4 space {ts}"}, token=alice)
    check("create audio room", s == 201 and r.get("id"), f"{s} {r}")
    room_id = r.get("id")
    s, r = req("POST", f"/api/audio-rooms/{room_id}/start", {}, token=alice)
    check("start audio room", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/audio-rooms", token=bob)
    check("discover live room",
          s == 200 and any(x.get("id") == room_id for x in r.get("rooms", [])), f"{s} {r}")
    s, r = req("POST", f"/api/audio-rooms/{room_id}/join", {}, token=bob)
    check("join audio room", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/audio-rooms/{room_id}/hand", {}, token=bob)
    check("raise hand", s == 200, f"{s} {r}")
    s, r = req("PUT", f"/api/audio-rooms/{room_id}/speakers/{bob_id}", {"role": "speaker"},
               token=alice)
    check("promote speaker", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/audio-rooms/{room_id}", token=carol)
    parts = r.get("participants", [])
    check("speaker promoted",
          s == 200 and any(p.get("user_id") == bob_id and p.get("role") == "speaker" for p in parts),
          f"{s} {r}")
    s, r = req("DELETE", f"/api/audio-rooms/{room_id}/speakers/{bob_id}", {}, token=alice)
    check("demote speaker", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/audio-rooms/{room_id}/end", {}, token=alice)
    check("end audio room", s == 200, f"{s} {r}")

    # --- premium plans ---
    s, r = req("GET", "/api/premium/plans", token=alice)
    plans = r.get("plans", [])
    check("premium plans seeded", s == 200 and len(plans) >= 2, f"{s} {r}")
    s, r = req("POST", "/api/premium/subscribe", {"plan_id": plans[0]["id"]}, token=alice)
    check("subscribe without funds fails", s == 400, f"{s} {r}")
    fund(f"g4a{ts}", "USD", "internal", "100")
    s, r = req("POST", "/api/premium/subscribe", {"plan_id": plans[0]["id"]}, token=alice)
    check("premium subscribe", s == 201, f"{s} {r}")
    s, r = req("GET", "/api/me", token=alice)
    me = r.get("user", r)
    check("is_premium flag", me.get("is_premium") is True, f"{s} {list(me.keys())[:20]}")

    # --- GIF catalog + gif/contact messages ---
    conv_id = make_dm(alice, bob)
    check("dm created", bool(conv_id), "")
    s, r = req("POST", "/api/gifs",
               {"title": "g4 dancing cat", "tags": ["cat", "dance"], "media_url": "gif-1"},
               token=alice)
    check("upload gif", s == 201 and r.get("id"), f"{s} {r}")
    gif_id = r.get("id")
    s, r = req("GET", "/api/gifs?q=cat", token=bob)
    check("search gifs", s == 200 and any(g.get("id") == gif_id for g in r.get("gifs", [])),
          f"{s} {r}")
    s, r = req("POST", f"/api/conversations/{conv_id}/gif", {"gif_id": gif_id}, token=alice)
    check("send gif message", s == 201, f"{s} {r}")
    s, r = req("POST", f"/api/conversations/{conv_id}/contact",
               {"username": f"g4c{ts}"}, token=alice)
    check("send contact card", s == 201, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv_id}/messages", token=bob)
    msgs = r.get("messages", [])
    kinds = {m.get("kind") for m in msgs}
    check("gif + contact kinds listed",
          s == 200 and "gif" in kinds and "contact" in kinds, f"{s} {kinds}")
    contact_msg = next((m for m in msgs if m.get("kind") == "contact"), {})
    ents = contact_msg.get("entities") or {}
    check("contact entities present", ents.get("username") == f"g4c{ts}", f"{ents}")

    # --- message entities (spoiler/bold) over WS ---
    async def send_entities():
        async with websockets.connect(f"{WS}/ws?token={alice}") as ws:
            await ws.send(json.dumps({
                "type": "message", "conversation_id": conv_id,
                "body": "spoiler alert", "entities": [
                    {"type": "spoiler", "offset": 0, "length": 7},
                    {"type": "bogus", "offset": -1, "length": 3},
                ]}))
            await asyncio.sleep(0.6)
    asyncio.run(send_entities())
    s, r = req("GET", f"/api/conversations/{conv_id}/messages", token=bob)
    msg = next((m for m in r.get("messages", []) if m.get("body") == "spoiler alert"), {})
    ents = msg.get("entities") or []
    check("ws entities stored", any(e.get("type") == "spoiler" for e in ents), f"{ents}")
    check("bogus entity stripped", not any(e.get("type") == "bogus" for e in ents), f"{ents}")

    # --- channel discussion group + stats + anonymous admin ---
    s, r = req("POST", "/api/conversations", {
        "is_channel": True, "title": f"g4 channel {ts}", "handle": f"g4chan{ts}"}, token=alice)
    check("create channel", s == 201, f"{s} {r}")
    chan_id = r.get("id") or r.get("conversation", {}).get("id")
    s, r = req("POST", "/api/conversations", {
        "is_group": True, "title": f"g4 group {ts}", "member_ids": [bob_id]}, token=alice)
    check("create group", s == 201, f"{s} {r}")
    grp_id = r.get("id") or r.get("conversation", {}).get("id")
    s, r = req("PUT", f"/api/channels/{chan_id}/discussion", {"group_id": grp_id}, token=alice)
    check("link discussion group", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/channels/{chan_id}/stats", token=alice)
    check("channel stats", s == 200 and "members" in r, f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{grp_id}/anonymous-admin", {"enabled": True},
               token=alice)
    check("anonymous admin on", s == 200, f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{grp_id}/category", {"category": "tech"}, token=alice)
    check("set group category", s == 200, f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{grp_id}/handle", {"handle": f"g4grp{ts}"}, token=alice)
    check("set group handle", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/discover/groups?category=tech", token=bob)
    check("group discovery by category",
          s == 200 and any(g.get("id") == grp_id for g in r.get("groups", [])), f"{s} {r}")

    # --- sounds library ---
    s, r = req("POST", "/api/sounds", {
        "title": "g4 original sound", "artist": "alice", "media_url": "snd-1",
        "duration_s": 15}, token=alice)
    check("publish sound", s == 201 and r.get("id"), f"{s} {r}")
    s, r = req("GET", "/api/sounds?q=original", token=bob)
    check("search sounds", s == 200 and len(r.get("sounds", [])) >= 1, f"{s} {r}")

    # --- posts: share ledger, paywall, rating ---
    s, r = req("POST", "/api/posts", {"body": "g4 post #g4tag", "visibility": "public"},
               token=alice)
    check("create post", s == 201 and r.get("id"), f"{s} {r}")
    post_id = r.get("id")
    s, r = req("POST", f"/api/posts/{post_id}/share", {"conversation_id": conv_id}, token=bob)
    check("share to chat", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/feed", token=bob)
    shared_post = next((p for p in r.get("posts", []) if p.get("id") == post_id), {})
    check("share_count bumped", s == 200 and shared_post.get("share_count", 0) >= 1,
          f"{s} {shared_post}")
    s, r = req("PUT", f"/api/posts/{post_id}/price", {"price_usd": 2.5}, token=alice)
    check("set post price", s == 200, f"{s} {r}")
    fund(f"g4b{ts}", "USD", "internal", "50")
    s, r = req("POST", f"/api/posts/{post_id}/purchase", {}, token=bob)
    check("purchase paywalled post", s in (200, 201), f"{s} {r}")
    s, r = req("POST", f"/api/posts/{post_id}/purchase", {}, token=bob)
    check("double purchase rejected", s == 409, f"{s} {r}")
    s, r = req("PUT", f"/api/posts/{post_id}/rating", {"rating": "mature"}, token=alice)
    check("set content rating", s == 200, f"{s} {r}")
    s, r = req("PUT", f"/api/posts/{post_id}/rating", {"rating": "mature"}, token=bob)
    check("rating by non-author rejected", s in (403, 404), f"{s} {r}")

    # --- interest vector from authoring ---
    s, r = req("GET", "/api/topics", token=alice)
    check("topics still listable", s == 200, f"{s} {r}")

    # --- marketplace ---
    s, r = req("POST", "/api/marketplace", {
        "title": "g4 bike", "description": "road bike", "price_usd": 120,
        "category": "vehicles", "photos": ["p1"]}, token=alice)
    check("create listing", s == 201 and r.get("id"), f"{s} {r}")
    listing_id = r.get("id")
    s, r = req("GET", "/api/marketplace?category=vehicles", token=bob)
    check("list listings", s == 200 and any(l.get("id") == listing_id for l in r.get("listings", [])),
          f"{s} {r}")
    s, r = req("PUT", f"/api/marketplace/{listing_id}/status", {"status": "sold"}, token=alice)
    check("mark sold", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/marketplace?category=vehicles", token=bob)
    check("sold listing hidden",
          s == 200 and not any(l.get("id") == listing_id for l in r.get("listings", [])), f"{s} {r}")

    # --- fundraisers ---
    s, r = req("POST", "/api/fundraisers", {
        "title": "g4 cause", "description": "help", "goal_usd": 100}, token=alice)
    check("create fundraiser", s == 201 and r.get("id"), f"{s} {r}")
    fr_id = r.get("id")
    s, r = req("POST", f"/api/fundraisers/{fr_id}/donate", {"amount_usd": 10}, token=bob)
    check("donate", s in (200, 201), f"{s} {r}")
    s, r = req("GET", f"/api/fundraisers/{fr_id}", token=carol)
    check("raised_usd updated", s == 200 and float(r.get("raised_usd", 0)) == 10.0, f"{s} {r}")

    # --- restricted mode + family pairing ---
    s, r = req("PUT", "/api/me/restricted-mode", {"enabled": True}, token=alice)
    check("restricted mode on", s == 200, f"{s} {r}")
    s, r = req("POST", "/api/family/link", {"child_username": f"g4b{ts}"}, token=alice)
    check("family link request", s == 201, f"{s} {r}")
    s, r = req("POST", "/api/family/accept", {"guardian_username": f"g4a{ts}"}, token=bob)
    check("family accept", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/family", token=alice)
    check("family list", s == 200 and len(r.get("links", [])) == 1
          and r["links"][0]["status"] == "active", f"{s} {r}")

    # --- XP / levels ---
    s, r = req("GET", "/api/me/level", token=alice)
    check("level endpoint", s == 200 and r.get("xp", 0) >= 1 and r.get("level", 0) >= 1,
          f"{s} {r}")
    s, r = req("GET", f"/api/users/g4b{ts}/level", token=alice)
    check("public level", s == 200 and "level" in r, f"{s} {r}")

    # --- people nearby + discoverable ---
    s, r = req("PUT", "/api/me/discoverable", {"enabled": True}, token=bob)
    check("discoverable on", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/nearby", token=alice)
    check("nearby requires live location", s == 400, f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{conv_id}/live-location",
               {"lat": 40.7128, "lng": -74.0060, "duration_minutes": 30}, token=alice)
    check("alice live location", s in (200, 201), f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{conv_id}/live-location",
               {"lat": 40.7130, "lng": -74.0062, "duration_minutes": 30}, token=bob)
    check("bob live location", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/nearby", token=alice)
    check("people nearby finds bob",
          s == 200 and any(p.get("username") == f"g4b{ts}" for p in r.get("people", [])),
          f"{s} {r}")

    # --- export + screenshot alert ---
    s, r = req("GET", "/api/me/export", token=alice)
    check("chat export", s == 200 and len(r.get("messages", [])) >= 3, f"{s}")
    s, r = req("POST", f"/api/conversations/{conv_id}/screenshot", {}, token=bob)
    check("screenshot alert", s == 201, f"{s} {r}")

    # --- bot invoices + inline bots ---
    s, r = req("POST", "/api/bots", {
        "username": f"g4pay{ts % 100000}bot", "display_name": "G4 Pay Bot"}, token=alice)
    check("create bot", s == 201 and r.get("token"), f"{s} {r}")
    bot_token = r.get("token")
    bot_username = f"g4pay{ts % 100000}bot"
    s, r = req("POST", f"/api/bot/{bot_token}/createInvoice",
               {"user_id": bob_id, "title": "g4 invoice", "amount_usd": 5}, )
    check("bot creates invoice", s == 201 and r.get("id"), f"{s} {r}")
    inv_id = r.get("id")
    s, r = req("POST", f"/api/bots/invoices/{inv_id}/pay", {}, token=bob)
    check("pay invoice", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/bots/invoices/{inv_id}/pay", {}, token=bob)
    check("double pay rejected", s == 409, f"{s} {r}")
    s, r = req("GET", f"/api/bots/inline?bot={bot_username}&q=", token=carol)
    check("inline query", s == 200 and "results" in r, f"{s} {r}")
    s, r = req("GET", "/api/bots/inline?bot=nobody&q=x", token=carol)
    check("inline unknown bot 404", s == 404, f"{s} {r}")

    # --- live gifts + leaderboard ---
    s, r = req("GET", "/api/gifts", token=alice)
    gifts = r.get("gifts", [])
    check("gift catalog", s == 200 and len(gifts) > 0, f"{s} {r}")
    gift_id = gifts[0]["id"]
    s, r = req("POST", f"/api/live/g4room{ts}/gifts",
               {"gift_id": gift_id, "to_user": alice_id}, token=bob)
    check("send live gift", s == 201, f"{s} {r}")
    s, r = req("GET", f"/api/live/g4room{ts}/leaderboard", token=carol)
    board = r.get("leaderboard", [])
    check("gift leaderboard",
          s == 200 and len(board) == 1 and board[0].get("username") == f"g4b{ts}", f"{s} {r}")

    # --- creator marketplace ---
    s, r = req("POST", "/api/brand-deals", {
        "title": "g4 campaign", "brief": "make a video", "budget_usd": 500}, token=alice)
    check("create brand deal", s == 201 and r.get("id"), f"{s} {r}")
    deal_id = r.get("id")
    s, r = req("GET", "/api/brand-deals", token=bob)
    check("list brand deals", s == 200 and any(d.get("id") == deal_id for d in r.get("deals", [])),
          f"{s} {r}")
    s, r = req("POST", f"/api/brand-deals/{deal_id}/accept", {}, token=bob)
    check("accept brand deal", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/brand-deals/{deal_id}/accept", {}, token=carol)
    check("double accept rejected", s == 404, f"{s} {r}")

    # --- professional dashboard ---
    s, r = req("GET", "/api/me/analytics", token=alice)
    check("pro dashboard",
          s == 200 and r.get("posts", 0) >= 1 and "earnings_usd" in r, f"{s} {r}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    sys.exit(1 if integration_test.failed else 0)


if __name__ == "__main__":
    main()
