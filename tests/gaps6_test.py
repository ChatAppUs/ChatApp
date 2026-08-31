"""End-to-end checks for gap-pack-6: custom audience lists on posts, long-form
articles, the post edit window, bio links, voice-note waveforms, typing
actions, admin-managed custom emoji + shortcode reactions, cached message
translation, discoverable live rooms, safety-mode auto-blocks, qualified-view
creator earnings, and the FYP negative-feedback filter + exploration slot.

Runs against a live API on :8080 with migration 021 applied. No mocks.
"""
import asyncio
import json
import os
import subprocess
import sys
import time

import websockets

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import integration_test
from integration_test import WS, check, req, grant_superadmin
from finance_test import db


def dbq(sql):
    dburl = os.environ.get(
        "DATABASE_URL", "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
    out = subprocess.run(["psql", dburl, "-At", "-c", sql],
                         check=True, capture_output=True, text=True)
    return out.stdout.strip()


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


def make_dm(tok_a, other_id):
    s, r = req("POST", "/api/conversations", {"member_ids": [other_id]}, token=tok_a)
    return r.get("id") or r.get("conversation", {}).get("id")


def ws_send(token, payload, settle=0.6):
    async def go():
        async with websockets.connect(f"{WS}/ws?token={token}") as ws:
            await ws.send(json.dumps(payload))
            await asyncio.sleep(settle)
    asyncio.run(go())


def main():
    ts = int(time.time())
    names = {k: f"g5{k}{ts}" for k in "abcdef"}
    alice = register(names["a"])
    bob = register(names["b"])
    carol = register(names["c"])
    dave = register(names["d"])
    eve = register(names["e"])
    frank = register(names["f"])
    alice_id, bob_id, carol_id = uid(alice), uid(bob), uid(carol)
    dave_id, eve_id, frank_id = uid(dave), uid(eve), uid(frank)
    check("user ids", all([alice_id, bob_id, carol_id, dave_id, eve_id, frank_id]), "")

    # ---------- custom audience lists on posts ----------
    s, r = req("POST", "/api/me/lists", {"name": "inner circle"}, token=alice)
    check("create list", s == 201, f"{s} {r}")
    list_id = r.get("id") or r.get("list", {}).get("id")
    s, r = req("PUT", f"/api/lists/{list_id}/members/{bob_id}", {}, token=alice)
    check("add bob to list", s in (200, 201), f"{s} {r}")

    s, r = req("POST", "/api/posts", {
        "body": f"list-only post {ts}", "visibility": "list", "audience_list_id": list_id}, token=alice)
    check("list post created", s == 201, f"{s} {r}")
    list_post = r.get("id") or r.get("post", {}).get("id")
    s, r = req("GET", f"/api/posts/{list_post}", token=alice)
    check("post echoes audience list", r.get("post", {}).get("audience_list_id") == list_id, f"{s} {r}")

    s, r = req("POST", "/api/posts", {
        "body": "bad combo", "visibility": "public", "audience_list_id": list_id}, token=alice)
    check("audience_list_id requires list visibility", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/posts", {
        "body": "thief", "visibility": "list", "audience_list_id": list_id}, token=bob)
    check("cannot target another user's list", s == 400, f"{s} {r}")

    time.sleep(0.3)
    s, r = req("GET", "/api/feed", token=bob)
    check("list member sees list post in feed",
          any(p.get("id") == list_post for p in r.get("posts", [])), f"{s}")
    s, r = req("GET", "/api/feed", token=carol)
    check("non-member does not see list post",
          not any(p.get("id") == list_post for p in r.get("posts", [])), f"{s}")

    # ---------- long-form articles ----------
    article = {"title": f"Deep dive {ts}", "subtitle": "pack 5",
               "body": "word " * 600, "cover_url": "https://cdn.example.com/cover.png"}
    s, r = req("POST", "/api/posts", {"article": article}, token=alice)
    check("article-only post created", s == 201, f"{s} {r}")
    art_post = r.get("id") or r.get("post", {}).get("id")
    s, r = req("GET", f"/api/posts/{art_post}", token=bob)
    got = r.get("post", {}).get("article") or {}
    check("article round-trips", got.get("title") == article["title"]
          and len(got.get("body", "")) > 1000, f"{s} {str(got)[:120]}")
    s, r = req("POST", "/api/posts", {"article": {"body": "no title"}}, token=alice)
    check("article without title rejected", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/posts", {"article": {
        "title": "x", "body": "y", "cover_url": "javascript:alert(1)"}}, token=alice)
    check("article cover must be http(s)", s == 400, f"{s} {r}")

    # ---------- post edit window ----------
    s, r = req("POST", "/api/posts", {"body": f"edit me {ts}"}, token=alice)
    edit_post = r.get("id") or r.get("post", {}).get("id")
    s, r = req("PATCH", f"/api/posts/{edit_post}", {"body": "edited in time"}, token=alice)
    check("edit within window ok", s == 200, f"{s} {r}")
    db(f"UPDATE posts SET created_at = now() - interval '49 hours' WHERE id='{edit_post}';")
    s, r = req("PATCH", f"/api/posts/{edit_post}", {"body": "too late"}, token=alice)
    check("edit after 48h window rejected", s == 403 and "window" in str(r), f"{s} {r}")

    # ---------- bio links ----------
    links = [{"title": "Site", "url": "https://example.com"},
             {"title": "Shop", "url": "https://shop.example.com"}]
    s, r = req("PATCH", "/api/me", {"bio_links": links}, token=alice)
    check("bio links saved", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me", token=alice)
    me = r if "bio_links" in r else r.get("user", {})
    check("bio links returned", len(me.get("bio_links") or []) == 2, f"{s} {str(me)[:160]}")
    s, r = req("PATCH", "/api/me", {"bio_links": [
        {"title": f"l{i}", "url": "https://e.com"} for i in range(6)]}, token=alice)
    check("max 5 bio links", s == 400, f"{s} {r}")
    s, r = req("PATCH", "/api/me", {"bio_links": [
        {"title": "xss", "url": "javascript:alert(1)"}]}, token=alice)
    check("bio link scheme validated", s == 400, f"{s} {r}")

    # ---------- voice notes + typing actions (WS) ----------
    conv = make_dm(alice, bob_id)
    check("dm created", bool(conv), "")
    ws_send(alice, {"type": "message", "conversation_id": conv,
                    "media_url": "/media/voice-note.ogg", "kind": "voice",
                    "waveform": [10, 999, -5, 42]})
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    voice = next((m for m in r.get("messages", []) if m.get("kind") == "voice"), None)
    check("voice note stored with kind", voice is not None, f"{s}")
    check("waveform clamped to 0..100",
          voice is not None and voice.get("waveform") == [10, 100, 0, 42],
          f"{voice and voice.get('waveform')}")

    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    msg_count_before = len(r.get("messages", []))

    async def typing_flow():
        async with websockets.connect(f"{WS}/ws?token={bob}") as bws:
            async with websockets.connect(f"{WS}/ws?token={alice}") as aws:
                await aws.send(json.dumps({
                    "type": "typing", "conversation_id": conv,
                    "action": "recording_voice"}))
                deadline = time.time() + 4
                while time.time() < deadline:
                    try:
                        raw = await asyncio.wait_for(bws.recv(), timeout=1)
                    except asyncio.TimeoutError:
                        continue
                    ev = json.loads(raw)
                    if ev.get("type") == "typing":
                        return ev
        return None
    ev = asyncio.run(typing_flow())
    check("typing action fanned out",
          ev is not None and ev.get("action") == "recording_voice" and ev.get("user_id") == alice_id,
          f"{ev}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    check("typing events never persisted",
          len(r.get("messages", [])) == msg_count_before, f"{s}")

    # ---------- custom emoji ----------
    s, r = req("POST", "/api/admin/custom-emoji", {
        "shortcode": "party_blob", "media_url": "https://cdn.example.com/pb.png"}, token=bob)
    check("non-admin cannot add custom emoji", s in (401, 403), f"{s} {r}")
    grant_superadmin(names["c"])
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"{names['c']}@test.dev", "password": "Passw0rd!123"})
    admin = r.get("access_token")
    check("admin login", s == 200 and bool(admin), f"{s} {r}")
    s, r = req("POST", "/api/admin/custom-emoji", {
        "shortcode": "party_blob", "media_url": "https://cdn.example.com/pb.png"}, token=admin)
    check("admin adds custom emoji", s == 201, f"{s} {r}")
    s, r = req("GET", "/api/custom-emoji", token=bob)
    check("custom emoji listed",
          any(e.get("shortcode") == "party_blob" for e in r.get("emoji", [])), f"{s} {r}")

    ws_send(alice, {"type": "message", "conversation_id": conv, "body": f"react target {ts}"})
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    target = next(m for m in r.get("messages", []) if m.get("body") == f"react target {ts}")
    s, r = req("POST", f"/api/messages/{target['id']}/reactions", {"emoji": "party_blob"}, token=bob)
    check("custom emoji reaction accepted", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    target = next(m for m in r.get("messages", []) if m.get("id") == target["id"])
    check("custom emoji in reaction map",
          (target.get("reactions") or {}).get("party_blob") == 1, f"{target.get('reactions')}")
    s, r = req("POST", f"/api/messages/{target['id']}/reactions", {"emoji": "not_registered"}, token=bob)
    check("unregistered shortcode rejected", s == 400, f"{s} {r}")
    s, r = req("DELETE", "/api/admin/custom-emoji/party_blob", token=admin)
    check("admin deletes custom emoji", s == 200, f"{s} {r}")

    # ---------- message translation ----------
    ws_send(alice, {"type": "message", "conversation_id": conv, "body": "hello friend"})
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    hello = next(m for m in r.get("messages", []) if m.get("body") == "hello friend")
    s, r = req("POST", f"/api/messages/{hello['id']}/translate", {"target_lang": "es"}, token=bob)
    tr = r.get("translation") or {}
    check("message translated",
          s == 200 and "hola" in tr.get("translated", "") and "amigo" in tr.get("translated", ""),
          f"{s} {r}")
    check("translation provider tagged", tr.get("provider") == "local-dict-v1", f"{tr}")
    s, r = req("POST", f"/api/messages/{hello['id']}/translate", {"target_lang": "es"}, token=bob)
    check("translation cached", (r.get("translation") or {}).get("provider") == "cache", f"{s} {r}")
    s, r = req("GET", f"/api/messages/{hello['id']}/translations", token=bob)
    check("translation history",
          any(t.get("target_lang") == "es" for t in r.get("translations", [])), f"{s} {r}")
    s, r = req("POST", f"/api/messages/{hello['id']}/translate", {"target_lang": "bad!"}, token=bob)
    check("invalid lang rejected", s == 400, f"{s} {r}")
    s, r = req("POST", f"/api/messages/{hello['id']}/translate", {"target_lang": "zu"}, token=bob)
    check("unsupported lang rejected", s == 400, f"{s} {r}")
    s, r = req("POST", f"/api/messages/{hello['id']}/translate", {"target_lang": "es"}, token=carol)
    check("non-member cannot translate", s == 404, f"{s} {r}")

    # ---------- live rooms ----------
    s, r = req("POST", "/api/live-rooms", {"title": f"g5 live {ts}"}, token=alice)
    check("live room created", s == 201 and r.get("room", {}).get("id"), f"{s} {r}")
    slug = (r.get("room") or {}).get("id")
    check("room is live with zero viewers",
          r.get("room", {}).get("status") == "live"
          and r.get("room", {}).get("viewer_count") == 0, f"{r}")
    s, r = req("POST", "/api/live-rooms", {"title": "second"}, token=alice)
    check("one live room per host", s == 409, f"{s} {r}")
    s, r = req("GET", "/api/live-rooms", token=bob)
    check("room listed", any(x.get("id") == slug for x in r.get("rooms", [])), f"{s}")
    s, r = req("POST", f"/api/live-rooms/{slug}/join", {}, token=bob)
    check("viewer joins", s == 201 and r.get("viewer_count") == 1, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/join", {}, token=carol)
    check("second viewer joins", s == 201 and r.get("viewer_count") == 2, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/join", {}, token=bob)
    check("rejoin is idempotent", s == 201 and r.get("viewer_count") == 2, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/like", {}, token=bob)
    check("room liked", s == 200 and r.get("like_count") == 1, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/leave", {}, token=carol)
    check("viewer leaves", s == 200 and r.get("viewer_count") == 1, f"{s} {r}")
    s, r = req("GET", f"/api/live-rooms/{slug}", token=dave)
    check("room detail", r.get("room", {}).get("viewer_count") == 1
          and r.get("room", {}).get("like_count") == 1, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/end", {}, token=bob)
    check("non-host cannot end", s == 404, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/end", {}, token=alice)
    check("host ends room", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/join", {}, token=dave)
    check("cannot join ended room", s == 404, f"{s} {r}")
    s, r = req("POST", f"/api/live-rooms/{slug}/like", {}, token=dave)
    check("cannot like ended room", s == 404, f"{s} {r}")
    s, r = req("GET", "/api/live-rooms", token=bob)
    check("ended room unlisted", not any(x.get("id") == slug for x in r.get("rooms", [])), f"{s}")

    # ---------- safety mode auto-blocks ----------
    s, r = req("PUT", "/api/me/privacy", {"safety_mode": True}, token=dave)
    check("safety mode on", s == 200, f"{s} {r}")
    # eve is a brand-new account (<7 days) -> auto-blocked
    conv_ed = make_dm(eve, dave_id)
    ws_send(eve, {"type": "message", "conversation_id": conv_ed, "body": "hey stranger"})
    s, r = req("GET", "/api/me/message-requests", token=dave)
    reqs = r.get("requests") or r.get("message_requests") or []
    check("no request from risky stranger",
          not any(q.get("conversation_id") == conv_ed for q in reqs), f"{s} {r}")
    blocked = dbq(f"SELECT COUNT(*) FROM user_blocks b JOIN users u ON u.id=b.blocker_id "
                  f"WHERE u.username='{names['d']}' AND b.blocked_id='{eve_id}';")
    check("safety mode auto-blocked new account", blocked == "1", blocked)

    # frank is 30 days old but has 3+ reports -> also auto-blocked
    db(f"UPDATE users SET created_at = now() - interval '30 days' WHERE username='{names['f']}';")
    for reporter in (alice, bob, carol):
        s, r = req("POST", "/api/reports", {
            "target_type": "user", "target_id": frank_id, "reason": "spam"}, token=reporter)
        check("report filed", s in (200, 201), f"{s} {r}")
    conv_fd = make_dm(frank, dave_id)
    ws_send(frank, {"type": "message", "conversation_id": conv_fd, "body": "not spam trust me"})
    blocked = dbq(f"SELECT COUNT(*) FROM user_blocks b JOIN users u ON u.id=b.blocker_id "
                  f"WHERE u.username='{names['d']}' AND b.blocked_id='{frank_id}';")
    check("safety mode auto-blocks 3+ reports", blocked == "1", blocked)
    # control: frank can still request carol (safety mode off)
    conv_fc = make_dm(frank, carol_id)
    ws_send(frank, {"type": "message", "conversation_id": conv_fc, "body": "hi carol"})
    s, r = req("GET", "/api/me/message-requests", token=carol)
    reqs = r.get("requests") or r.get("message_requests") or []
    check("request lands when safety mode off",
          any(q.get("conversation_id") == conv_fc for q in reqs), f"{s} {r}")

    # ---------- qualified-view creator earnings ----------
    s, r = req("POST", "/api/posts", {"body": f"earn reel {ts}", "type": "reel"}, token=frank)
    check("reel created", s == 201, f"{s} {r}")
    earn_reel = r.get("id") or r.get("post", {}).get("id")
    s, r = req("GET", "/api/creator/earnings", token=frank)
    check("no earnings without qualified views", s == 200 and r.get("earned") == 0, f"{s} {r}")
    s, r = req("POST", f"/api/reels/{earn_reel}/watch", {
        "watched_ms": 5000, "duration_ms": 5000, "completed": True}, token=bob)
    check("completion recorded", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/creator/earnings", token=frank)
    check("completed view pays RPM",
          s == 200 and abs(r.get("earned", 0) - 0.0005) < 1e-9, f"{s} {r}")
    s, r = req("POST", f"/api/reels/{earn_reel}/watch", {
        "watched_ms": 300, "duration_ms": 5000, "not_interested": True}, token=carol)
    check("skip recorded", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/creator/earnings", token=frank)
    check("skips do not pay", abs(r.get("earned", 0) - 0.0005) < 1e-9, f"{s} {r}")

    # ---------- FYP: negative feedback + exploration ----------
    reel_ids = []
    for i in range(12):
        author, author_id = (frank, frank_id) if i < 9 else (alice, alice_id)
        s, r = req("POST", "/api/posts", {"body": f"fyp reel {ts} {i}", "type": "reel"}, token=author)
        rid = r.get("id") or r.get("post", {}).get("id")
        reel_ids.append(rid)
        s, r = req("POST", f"/api/reels/{rid}/watch", {
            "watched_ms": 4000, "duration_ms": 4000, "completed": True}, token=bob)
        check(f"watch signal {i}", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/fyp?limit=9", token=eve)
    posts = r.get("posts", [])
    check("fyp returns ranked page", s == 200 and len(posts) >= 9
          and not any(p.get("explore") for p in posts[:9]), f"{s} {len(posts)}")
    check("exploration slot injected", len(posts) == 10 and posts[-1].get("explore") is True,
          f"{len(posts)} {[p.get('explore') for p in posts]}")

    report_target = posts[0]["id"]
    s, r = req("POST", "/api/reports", {
        "target_type": "post", "target_id": report_target, "reason": "spam"}, token=eve)
    check("reel reported", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/fyp?limit=9", token=eve)
    check("reported reel filtered from fyp",
          all(p.get("id") != report_target for p in r.get("posts", [])), f"{s}")
    s, r = req("GET", "/api/fyp?limit=20", token=bob)
    check("report is viewer-scoped",
          any(p.get("id") == report_target for p in r.get("posts", [])), f"{s}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    return 1 if integration_test.failed else 0


if __name__ == "__main__":
    sys.exit(main())
