#!/usr/bin/env python3
"""End-to-end checks for gap-pack-2: account safety (trusted contacts,
legacy contact, profiles), chat extras (polls, video notes, live location,
pay-in-chat), reels (remix, analytics), community notes, calls (screenshare,
recordings), sanctions, derived rates, media moderation admin.

Runs against a live API on :8080 with migration 017 applied. No mocks.
"""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req, grant_superadmin
from finance_test import db, fund


def register(name):
    # Register is rate-limited (10/min); retry with backoff so back-to-back
    # suite runs don't cascade into spurious failures.
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


def main():
    ts = int(time.time())
    alice = register(f"g2a{ts}")
    bob = register(f"g2b{ts}")
    carol = register(f"g2c{ts}")
    dave = register(f"g2d{ts}")

    # --- user ids ---
    s, r = req("GET", "/api/me", token=bob)
    bob_id = r.get("id") or r.get("user", {}).get("id")
    s, r = req("GET", "/api/me", token=carol)
    carol_id = r.get("id") or r.get("user", {}).get("id")
    s, r = req("GET", "/api/me", token=alice)
    alice_id = r.get("id") or r.get("user", {}).get("id")
    check("user ids", bool(alice_id and bob_id and carol_id), f"{alice_id} {bob_id} {carol_id}")

    # --- trusted contacts + recovery ---
    for name in (f"g2b{ts}", f"g2c{ts}", f"g2d{ts}"):
        s, r = req("POST", "/api/me/trusted-contacts", {"username": name}, token=alice)
        check(f"add trusted contact {name}", s == 201, f"{s} {r}")
    s, r = req("GET", "/api/me/trusted-contacts", token=alice)
    check("list trusted contacts", s == 200 and len(r.get("contacts", [])) == 3, f"{s} {r}")
    s, r = req("POST", "/api/recovery/trusted/request", {"username": f"g2a{ts}"})
    check("recovery request", s == 200 and r.get("status") == "ok", f"{s} {r}")
    codes = []
    for name, tok in (("bob", bob), ("carol", carol)):
        s, r = req("GET", "/api/recovery/trusted/pending", token=tok)
        pend = r.get("pending", [])
        check(f"{name} sees pending recovery", s == 200 and len(pend) == 1, f"{s} {r}")
        s, r = req("POST", "/api/recovery/trusted/reveal", {"share_id": pend[0]["id"]}, token=tok)
        check(f"{name} reveals code", s == 200 and r.get("code"), f"{s} {r}")
        codes.append(r["code"])
    s, r = req("POST", "/api/recovery/trusted/redeem",
               {"username": f"g2a{ts}", "codes": codes})
    reset_tok = r.get("reset_token")
    check("redeem 2 recovery codes", s == 200 and reset_tok, f"{s} {r}")
    time.sleep(3)
    for _ in range(4):
        s, r = req("POST", "/api/recovery/trusted/redeem",
                   {"username": f"g2a{ts}", "codes": codes})
        if s == 429:
            time.sleep(8)
            continue
        break
    check("codes single-use", s == 400, f"{s} {r}")
    s, r = req("DELETE", f"/api/me/trusted-contacts/{bob_id}", token=alice)
    check("remove trusted contact", s == 200, f"{s} {r}")

    # --- legacy contact ---
    s, r = req("PUT", "/api/me/legacy-contact", {"username": f"g2b{ts}"}, token=alice)
    check("set legacy contact", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/legacy-contact", token=alice)
    check("get legacy contact", s == 200 and (r.get("legacy_contact") or {}).get("id") == bob_id, f"{s} {r}")
    s, r = req("GET", f"/api/legacy/{alice_id}/export", token=bob)
    check("legacy export blocked before memorialization", s == 403, f"{s} {r}")
    s, r = req("DELETE", "/api/me/legacy-contact", token=alice)
    check("remove legacy contact", s == 200, f"{s} {r}")

    # --- multiple profiles ---
    s, r = req("POST", "/api/me/profiles",
               {"name": "Work", "bio": "professional"}, token=alice)
    prof_id = r.get("id")
    check("create profile", s == 201 and prof_id, f"{s} {r}")
    s, r = req("GET", "/api/me/profiles", token=alice)
    check("list profiles", s == 200 and len(r.get("profiles", [])) == 1, f"{s} {r}")
    s, r = req("PUT", "/api/me/active-profile", {"profile_id": prof_id}, token=alice)
    check("switch profile", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/profiles", token=alice)
    check("profile active", s == 200 and r["profiles"][0].get("active") is True, f"{s} {r}")
    s, r = req("PUT", "/api/me/active-profile", {"profile_id": ""}, token=alice)
    check("switch back to main", s == 200, f"{s} {r}")
    s, r = req("DELETE", f"/api/me/profiles/{prof_id}", token=alice)
    check("delete profile", s == 200, f"{s} {r}")

    # --- digest toggle ---
    s, r = req("PUT", "/api/me/digest", {"enabled": False}, token=alice)
    check("digest opt-out", s == 200 and r.get("digest_enabled") is False, f"{s} {r}")

    # --- chat extras: conversation between alice and bob ---
    s, r = req("POST", "/api/conversations", {"type": "direct", "member_ids": [bob_id]}, token=alice)
    conv = r.get("id") or r.get("conversation_id")
    check("create dm", s in (200, 201) and conv, f"{s} {r}")

    s, r = req("POST", f"/api/conversations/{conv}/polls",
               {"question": "Lunch?", "options": ["pizza", "sushi"]}, token=alice)
    poll_id = r.get("id")
    check("create chat poll", s == 201 and poll_id and r.get("message_id"), f"{s} {r}")
    s, r = req("GET", f"/api/chat-polls/{poll_id}", token=bob)
    opt_id = r["options"][0]["id"]
    s, r = req("POST", f"/api/chat-polls/{poll_id}/vote", {"option_id": opt_id}, token=bob)
    check("vote chat poll", s == 200 and r.get("status") == "voted", f"{s} {r}")
    s, r = req("GET", f"/api/chat-polls/{poll_id}", token=bob)
    check("get chat poll", s == 200 and r.get("total_votes") == 1 and r["options"][0]["votes"] == 1, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice)
    kinds = [m.get("kind") for m in r.get("messages", [])]
    poll_msgs = [m for m in r.get("messages", []) if m.get("poll_id")]
    check("poll message in listing", "poll" in kinds and len(poll_msgs) == 1, f"{s} kinds={kinds}")

    s, r = req("POST", f"/api/conversations/{conv}/video-note",
               {"media_url": "/media/note.mp4", "duration_s": 9}, token=alice)
    check("video note", s == 201 and r.get("id"), f"{s} {r}")

    s, r = req("PUT", f"/api/conversations/{conv}/live-location",
               {"lat": 40.7128, "lng": -74.006, "duration_minutes": 30}, token=alice)
    check("share live location", s == 200 and r.get("status") == "sharing", f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/live-location", token=bob)
    check("get live locations", s == 200 and len(r.get("locations", [])) == 1, f"{s} {r}")
    s, r = req("DELETE", f"/api/conversations/{conv}/live-location", token=alice)
    check("stop live location", s == 200, f"{s} {r}")

    # --- pay-in-chat (fund alice via ledger bootstrap) ---
    db(f"UPDATE users SET kyc_status='verified' WHERE username IN ('g2a{ts}','g2b{ts}','g2c{ts}')")
    fund(f"g2a{ts}", "USDT", "tron", "100")
    s, r = req("POST", f"/api/conversations/{conv}/pay",
               {"to_user_id": bob_id, "asset": "USDT", "chain": "tron", "amount": "5"}, token=alice)
    pay_id = r.get("tx_id")
    check("pay-in-chat", s == 201 and pay_id, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    pay_msgs = [m for m in r.get("messages", []) if m.get("kind") == "payment"]
    check("payment message in listing", len(pay_msgs) == 1 and pay_msgs[0].get("payment_id") == pay_id,
          f"{s} {len(pay_msgs)}")

    # --- reels: remix + analytics ---
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "original reel", "media": [{"kind": "video", "url": "/media/r1.mp4"}]},
               token=alice)
    reel_id = r.get("id")
    check("create reel", s == 201 and reel_id, f"{s} {r}")
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "my remix", "remix_of": reel_id,
                "media": [{"kind": "video", "url": "/media/r2.mp4"}]}, token=bob)
    remix_id = r.get("id")
    check("remix reel", s == 201 and remix_id, f"{s} {r}")
    s, r = req("POST", "/api/posts", {"type": "post", "body": "x", "remix_of": reel_id}, token=bob)
    check("remix of non-reel type rejected", s == 400, f"{s} {r}")
    s, r = req("GET", f"/api/reels/{reel_id}/remixes", token=alice)
    check("remixes listed", s == 200 and len(r.get("remixes", [])) == 1, f"{s} {r}")
    s, r = req("GET", f"/api/reels/{reel_id}/analytics", token=alice)
    check("reel analytics (author)", s == 200 and r.get("remixes") == 1, f"{s} {r}")
    s, r = req("GET", f"/api/reels/{reel_id}/analytics", token=bob)
    check("reel analytics forbidden for others", s == 403, f"{s} {r}")

    # --- text story with composer fields ---
    s, r = req("POST", "/api/posts",
               {"type": "story", "body": "hello", "story_background": "sunset",
                "story_stickers": '[{"emoji":"fire","x":0.5,"y":0.5}]'}, token=alice)
    check("text story with composer", s == 201, f"{s} {r}")
    s, r = req("POST", "/api/posts", {"type": "story", "body": "x", "story_background": "bogus"}, token=alice)
    check("invalid story background rejected", s == 400, f"{s} {r}")

    # --- community notes ---
    s, r = req("POST", "/api/posts", {"type": "post", "body": "breaking claim"}, token=alice)
    post_id = r.get("id")
    s, r = req("POST", f"/api/posts/{post_id}/notes",
               {"body": "This claim is missing important context about timing."}, token=bob)
    note_id = r.get("id")
    check("create community note", s == 201 and note_id, f"{s} {r}")
    s, r = req("POST", f"/api/posts/{post_id}/notes", {"body": "Author cannot note own post..."}, token=alice)
    check("author cannot note own post", s == 400, f"{s} {r}")
    s, r = req("POST", f"/api/notes/{note_id}/vote", {"helpful": True}, token=carol)
    check("vote note", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/notes", token=alice)
    check("list notes", s == 200 and len(r.get("notes", [])) == 1, f"{s} {r}")
    s, r = req("DELETE", f"/api/notes/{note_id}", token=bob)
    check("delete own note", s == 200, f"{s} {r}")

    # --- calls: screenshare + recordings ---
    s, r = req("POST", "/api/calls/rooms", {"conversation_id": conv, "mode": "meeting"}, token=alice)
    room_id = r.get("room_id")
    check("create call room", s == 200 and room_id, f"{s} {r}")
    s, r = req("POST", f"/api/calls/rooms/{room_id}/screenshare", {"on": True}, token=alice)
    check("screenshare on", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/calls/rooms/{room_id}/recordings",
               {"media_url": "/media/rec.mp4", "duration_s": 120}, token=alice)
    rec_id = r.get("id")
    check("save recording", s == 201 and rec_id, f"{s} {r}")
    s, r = req("GET", f"/api/calls/rooms/{room_id}/recordings", token=bob)
    check("list recordings", s == 200 and len(r.get("recordings", [])) == 1, f"{s} {r}")
    s, r = req("DELETE", f"/api/calls/recordings/{rec_id}", token=bob)
    check("non-owner cannot delete recording", s == 404, f"{s} {r}")
    s, r = req("DELETE", f"/api/calls/recordings/{rec_id}", token=alice)
    check("owner deletes recording", s == 200, f"{s} {r}")

    # --- admin: sanctions, derived rates, media moderation ---
    grant_superadmin(f"g2a{ts}")
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"g2a{ts}@test.dev", "password": "Passw0rd!123"})
    admin = r.get("access_token")
    check("admin login", s == 200 and admin, f"{s} {r}")

    import json
    import urllib.request

    csv_body = "source,name,program\nofac,Jon Badguy,SDN\nun,Jane Evildoer,UNSC\n"
    rq = urllib.request.Request(
        "http://localhost:8080/api/admin/sanctions/import",
        data=csv_body.encode(), method="POST")
    rq.add_header("Content-Type", "text/csv")
    rq.add_header("Authorization", f"Bearer {admin}")
    try:
        with urllib.request.urlopen(rq) as resp:
            s, r = resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e:
        s, r = e.code, json.loads(e.read() or b"{}")
    check("sanctions import", s == 200 and r.get("imported", 0) + r.get("skipped", 0) == 2, f"{s} {r}")
    s, r = req("GET", "/api/admin/sanctions/stats", token=admin)
    check("sanctions stats", s == 200 and r.get("total", 0) >= 2, f"{s} {r}")

    s, r = req("GET", "/api/admin/convert/rates/derived", token=admin)
    check("derived rates endpoint", s == 200 and "derived" in r, f"{s} {r}")
    s, r = req("POST", "/api/admin/convert/rates/apply-derived", {}, token=admin)
    check("apply derived rates", s == 200 and "applied" in r, f"{s} {r}")

    s, r = req("POST", "/api/admin/moderation/block-hash",
               {"sha256": "a" * 64, "reason": "test"}, token=admin)
    check("block media hash", s in (201, 409), f"{s} {r}")
    s, r = req("GET", "/api/admin/moderation/media", token=admin)
    check("media moderation log", s == 200 and "entries" in r, f"{s} {r}")

    # --- memorialize + legacy export (admin-gated) ---
    s, r = req("PUT", "/api/me/legacy-contact", {"username": f"g2b{ts}"}, token=alice)
    check("re-set legacy contact", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/admin/users/{alice_id}/memorialize",
               {"memorialize": True}, token=admin)
    check("admin memorializes account", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/legacy/{alice_id}/export", token=bob)
    check("legacy export (designated contact)", s == 200 and "posts" in r, f"{s} {r}")
    s, r = req("GET", f"/api/legacy/{alice_id}/export", token=carol)
    check("legacy export denied for others", s == 403, f"{s} {r}")
    s, r = req("POST", f"/api/admin/users/{alice_id}/memorialize",
               {"memorialize": False}, token=admin)
    check("admin un-memorializes", s == 200, f"{s} {r}")

    print("\nAll gap-pack-2 checks passed.")


if __name__ == "__main__":
    main()
