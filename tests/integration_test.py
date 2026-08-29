#!/usr/bin/env python3
"""End-to-end integration test against a live ChatApp API (no mocks)."""
import asyncio
import base64
import hashlib
import hmac
import json
import struct
import sys
import time
import urllib.request
import urllib.error

import websockets

BASE = "http://localhost:8080"
WS = "ws://localhost:8080"
passed = failed = 0


def check(name, cond, detail=""):
    global passed, failed
    if cond:
        passed += 1
        print(f"  PASS {name}")
    else:
        failed += 1
        print(f"  FAIL {name} {detail}")


def req(method, path, body=None, token=None, expect=200):
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(BASE + path, data=data, method=method)
    r.add_header("Content-Type", "application/json")
    if token:
        r.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(r) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except Exception:
            return e.code, {}


def totp(secret_b32, counter):
    key = base64.b32decode(secret_b32)
    digest = hmac.new(key, struct.pack(">Q", counter), hashlib.sha1).digest()
    off = digest[-1] & 0x0F
    code = (struct.unpack(">I", digest[off:off + 4])[0] & 0x7FFFFFFF) % 1_000_000
    return f"{code:06d}"


def main():
    ts = int(time.time())
    alice = f"alice{ts}"
    bob = f"bob{ts}"

    # --- auth ---
    s, r = req("POST", "/api/auth/register", {
        "username": alice, "email": f"{alice}@test.dev", "password": "Passw0rd!123",
        "display_name": "Alice", "country_code": "US"})
    check("register alice", s in (200, 201) and r.get("access_token"), f"{s} {r}")
    alice_tok = r.get("access_token")
    alice_id = r.get("user_id")

    s, r = req("POST", "/api/auth/register", {
        "username": bob, "email": f"{bob}@test.dev", "password": "Passw0rd!123",
        "display_name": "Bob", "country_code": "GB"})
    check("register bob", s in (200, 201), f"{s} {r}")
    bob_tok = r.get("access_token")
    bob_id = r.get("user_id")

    s, r = req("POST", "/api/auth/login", {"identifier": alice, "password": "Passw0rd!123"})
    check("login alice", s == 200 and r.get("access_token"), f"{s} {r}")

    s, r = req("POST", "/api/auth/login", {"identifier": alice, "password": "wrong"})
    check("login wrong password rejected", s == 401, f"{s}")

    s, r = req("GET", "/api/me", token=alice_tok)
    check("GET /api/me", s == 200 and r.get("username") == alice, f"{s} {r}")

    s, r = req("GET", "/api/me")
    check("unauthenticated /api/me rejected", s == 401, f"{s}")

    # --- posts / social ---
    s, r = req("POST", "/api/posts", {
        "type": "post", "body": f"Hello @{bob} this is #launch day! #chatapp"}, token=alice_tok)
    check("create post with mention+hashtags", s in (200, 201) and r.get("id"), f"{s} {r}")
    post_id = r.get("id")

    s, r = req("GET", "/api/feed", token=bob_tok)
    check("bob feed", s == 200 and isinstance(r.get("posts"), list), f"{s}")

    s, r = req("POST", f"/api/posts/{post_id}/like", {}, token=bob_tok)
    check("bob likes post", s == 200, f"{s} {r}")

    s, r = req("POST", f"/api/posts/{post_id}/comments", {"body": "nice!"}, token=bob_tok)
    check("bob comments", s in (200, 201) and r.get("id"), f"{s} {r}")

    s, r = req("GET", f"/api/posts/{post_id}/comments", token=alice_tok)
    check("list comments", s == 200 and len(r.get("comments", [])) >= 1, f"{s} {r}")

    s, r = req("GET", "/api/hashtags/trending", token=alice_tok)
    tags = [t["tag"] for t in r.get("trending", [])]
    check("trending hashtags", s == 200 and "launch" in tags and "chatapp" in tags, f"{s} {r}")

    s, r = req("GET", "/api/hashtags/launch/posts", token=alice_tok)
    check("hashtag posts", s == 200 and any(p["id"] == post_id for p in r.get("posts", [])), f"{s} {r}")

    s, r = req("POST", f"/api/posts/{post_id}/bookmark", {}, token=bob_tok)
    check("bookmark", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/bookmarks", token=bob_tok)
    check("list bookmarks", s == 200 and any(p["id"] == post_id for p in r.get("posts", [])), f"{s} {r}")
    s, r = req("DELETE", f"/api/posts/{post_id}/bookmark", token=bob_tok)
    check("unbookmark", s == 200, f"{s} {r}")

    s, r = req("POST", f"/api/posts/{post_id}/repost", {}, token=bob_tok)
    check("repost", s in (200, 201), f"{s} {r}")

    # --- polls ---
    s, r = req("POST", "/api/posts", {
        "type": "post", "body": "Best feature?",
        "poll_options": ["chat", "calls", "wallet"]}, token=alice_tok)
    check("create poll post", s in (200, 201) and r.get("id"), f"{s} {r}")
    poll_post = r.get("id")
    s, r = req("GET", f"/api/posts/{poll_post}/poll", token=alice_tok)
    opts = r.get("options", [])
    check("poll options created", s == 200 and len(opts) == 3, f"{s} {r}")
    s, r = req("POST", f"/api/posts/{poll_post}/vote", {"option_id": opts[1]["id"]}, token=bob_tok)
    check("vote poll", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{poll_post}/poll", token=alice_tok)
    votes = [o.get("votes", 0) for o in r.get("options", [])]
    check("poll results", s == 200 and votes == [0, 1, 0], f"{s} {r}")
    s, r = req("POST", f"/api/posts/{poll_post}/vote", {"option_id": opts[2]["id"]}, token=bob_tok)
    s2, r2 = req("GET", f"/api/posts/{poll_post}/poll", token=alice_tok)
    votes = [o.get("votes", 0) for o in r2.get("options", [])]
    check("vote change (upsert)", s == 200 and votes == [0, 0, 1], f"{s} {r2}")

    # --- follow / block ---
    s, r = req("POST", f"/api/users/{bob_id}/follow", {}, token=alice_tok)
    check("alice follows bob", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/users/{alice_id}/block", {}, token=bob_tok)
    check("bob blocks alice", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/users/{bob_id}/follow", {}, token=alice_tok)
    check("blocked follow rejected", s in (400, 403, 409), f"{s} {r}")
    s, r = req("DELETE", f"/api/users/{alice_id}/block", token=bob_tok)
    check("unblock", s == 200, f"{s} {r}")

    # --- chat over websocket ---
    s, r = req("POST", "/api/conversations", {"member_ids": [bob_id]}, token=alice_tok)
    check("create DM conversation", s in (200, 201) and r.get("id"), f"{s} {r}")
    conv_id = r.get("id")
    msg_id = asyncio.run(chat_flow(alice_tok, bob_tok, conv_id, alice_id, bob_id))

    # message edit / reaction / delete over REST
    if msg_id:
        s, r = req("POST", f"/api/messages/{msg_id}/edit", {"body": "edited hello"}, token=alice_tok)
        check("edit message", s == 200, f"{s} {r}")
        s, r = req("POST", f"/api/messages/{msg_id}/reactions", {"emoji": "👍"}, token=bob_tok)
        check("react to message", s == 200, f"{s} {r}")
        s, r = req("GET", f"/api/conversations/{conv_id}/messages", token=bob_tok)
        msgs = r.get("messages", [])
        m = next((m for m in msgs if m["id"] == msg_id), None)
        reactions = m.get("reactions", {}) if m else {}
        check("edit+reaction persisted", bool(m) and m.get("body") == "edited hello"
              and reactions.get("👍", 0) >= 1, f"{s} {m}")
        s, r = req("DELETE", f"/api/messages/{msg_id}", token=alice_tok)
        check("delete message", s == 200, f"{s} {r}")

    # --- channels ---
    s, r = req("POST", "/api/conversations", {
        "is_channel": True, "title": f"News {ts}", "description": "test channel",
        "member_ids": []}, token=alice_tok)
    check("create channel", s in (200, 201) and r.get("id"), f"{s} {r}")
    chan_id = r.get("id")
    s, r = req("GET", "/api/channels?q=News", token=bob_tok)
    check("discover channel", s == 200 and any(c["id"] == chan_id for c in r.get("channels", [])), f"{s} {r}")
    s, r = req("POST", f"/api/channels/{chan_id}/subscribe", {}, token=bob_tok)
    check("subscribe channel", s == 200, f"{s} {r}")

    # --- 2FA ---
    s, r = req("POST", "/api/auth/2fa/setup", {}, token=alice_tok)
    check("2FA setup", s == 200 and r.get("secret"), f"{s} {r}")
    secret = r.get("secret")
    code = totp(secret, int(time.time()) // 30)
    s, r = req("POST", "/api/auth/2fa/enable", {"code": code}, token=alice_tok)
    check("2FA enable", s == 200, f"{s} {r}")
    s, r = req("POST", "/api/auth/login", {"identifier": alice, "password": "Passw0rd!123"})
    check("login without TOTP rejected", s == 401 and "totp" in json.dumps(r), f"{s} {r}")
    code = totp(secret, int(time.time()) // 30)
    s, r = req("POST", "/api/auth/login", {
        "identifier": alice, "password": "Passw0rd!123", "totp_code": code})
    check("login with TOTP", s == 200 and r.get("access_token"), f"{s} {r}")
    code = totp(secret, int(time.time()) // 30)
    s, r = req("POST", "/api/auth/2fa/disable", {"code": code}, token=alice_tok)
    check("2FA disable", s == 200, f"{s} {r}")

    # --- E2EE key relay ---
    s, r = req("PUT", "/api/e2e/key", {"identity_key": "BPub" + "A" * 120}, token=alice_tok)
    check("publish E2E key", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/e2e/keys?user_ids={alice_id}", token=bob_tok)
    check("fetch E2E keys", s == 200 and r.get("keys"), f"{s} {r}")

    # --- creator earnings ---
    s, r = req("GET", "/api/creator/earnings", token=alice_tok)
    check("creator earnings", s == 200 and "available" in r, f"{s} {r}")

    # --- password reset flow ---
    s, r = req("POST", "/api/auth/forgot-password", {"identifier": f"{bob}@test.dev"})
    check("forgot password accepted", s == 200, f"{s} {r}")

    print(f"\n{passed} passed, {failed} failed")
    sys.exit(1 if failed else 0)


async def chat_flow(alice_tok, bob_tok, conv_id, alice_id, bob_id):
    """Real WebSocket round-trip: message delivery, typing, receipts."""
    results = {"delivered": False, "typing": False, "msg_id": None}
    async with websockets.connect(f"{WS}/ws?token={alice_tok}") as wa, \
               websockets.connect(f"{WS}/ws?token={bob_tok}") as wb:
        await wa.send(json.dumps({"type": "typing", "conversation_id": conv_id}))
        await wa.send(json.dumps({
            "type": "message", "conversation_id": conv_id, "body": "hello bob"}))
        try:
            for _ in range(6):
                raw = await asyncio.wait_for(wb.recv(), timeout=5)
                evt = json.loads(raw)
                if evt.get("type") == "typing" and evt.get("conversation_id") == conv_id:
                    results["typing"] = True
                if evt.get("type") == "message" and evt.get("body") == "hello bob":
                    results["delivered"] = True
                    results["msg_id"] = evt.get("id")
                    break
        except asyncio.TimeoutError:
            pass
    check("ws typing indicator", results["typing"])
    check("ws message delivery", results["delivered"])
    return results["msg_id"]


if __name__ == "__main__":
    main()
