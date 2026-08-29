#!/usr/bin/env python3
"""End-to-end integration test against a live ChatApp API (no mocks)."""
import asyncio
import os
import base64
import hashlib
import hmac
import json
import struct
import sys
import time
from datetime import datetime, timedelta, timezone
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

    # --- QR login (Telegram-style): new device -> approve from authed device ---
    s, r = req("POST", "/api/auth/qr/new", {})
    check("qr token created", s in (200, 201) and r.get("token"), f"{s} {r}")
    qr_token = r.get("token")
    s, r = req("GET", f"/api/auth/qr/{qr_token}")
    check("qr status pending", s == 200 and r.get("status") == "pending", f"{s} {r}")
    s, r = req("POST", f"/api/auth/qr/{qr_token}/approve", {}, token=bob_tok)
    check("qr approve", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/auth/qr/{qr_token}")
    check("qr login issues tokens", s == 200 and r.get("access_token"), f"{s} {r}")
    qr_tok = r.get("access_token")
    s, r = req("GET", "/api/me", token=qr_tok)
    check("qr session works", s == 200 and r.get("username") == bob, f"{s} {r}")
    s, r = req("GET", f"/api/auth/qr/{qr_token}")
    check("qr token one-shot", s == 200 and r.get("status") == "consumed"
          and not r.get("access_token"), f"{s} {r}")
    s, r = req("POST", f"/api/auth/qr/{qr_token}/approve", {}, token=bob_tok)
    check("qr re-approve rejected", s == 410, f"{s} {r}")

    # --- passkeys: full WebAuthn ceremony with a software authenticator ---
    passkey_flow(alice_tok, alice)

    # --- cluster engine: heartbeat, routing, admin management ---
    grant_superadmin(alice)
    s, r = req("POST", "/api/auth/login", {"identifier": alice, "password": "Passw0rd!123"})
    alice_tok = r.get("access_token")  # fresh user-plane token

    # --- admin plane separation ---
    s, r = req("GET", "/api/admin/stats", token=alice_tok)
    check("user token rejected on admin route", s == 401, f"{s} {r}")
    s, r = req("POST", "/api/admin/login", {"identifier": bob, "password": "Passw0rd!123"})
    check("non-admin cannot admin-login", s == 403, f"{s} {r}")
    s, r = req("POST", "/api/admin/login", {"identifier": alice, "password": "Passw0rd!123"})
    check("admin login issues admin-scoped token", s == 200 and r.get("access_token")
          and "superadmin" in (r.get("roles") or []), f"{s} {r}")
    admin_tok = r.get("access_token")
    s, r = req("GET", "/api/admin/stats", token=admin_tok)
    check("admin token accepted on admin route", s == 200 and "users" in r, f"{s} {r}")
    s, r = req("GET", "/api/me", token=admin_tok)
    check("admin token rejected on user route", s == 401, f"{s} {r}")
    cluster_flow(admin_tok)

    # --- self-built OTP engine (no external verification service) ---
    otp_flow()

    # --- admin-managed platform tokens for the built-in wallet ---
    wallet_tokens_flow(admin_tok, alice_tok)

    # --- self-built SFU: meeting + live broadcast tickets ---
    calls_flow(alice_tok, conv_id)

    # --- competitor-parity batch: unrepost, edit, threads, pins, forward,
    #     saved messages, story engagement ---
    social2_flow(alice_tok, bob_tok, conv_id)

    print(f"\n{passed} passed, {failed} failed")
    sys.exit(1 if failed else 0)


def otp_flow():
    """Self-built OTP engine: salted hashes, expiry, attempts, cooldown."""
    phone = f"+1555{int(time.time()) % 10000000:07d}"
    s, r = req("POST", "/api/auth/phone/send-code", {"phone": phone})
    check("otp send", s == 200 and r.get("dev_code"), f"{s} {r}")
    code = r.get("dev_code")
    s, r = req("POST", "/api/auth/phone/send-code", {"phone": phone})
    check("otp resend cooldown enforced", s == 429, f"{s} {r}")
    s, r = req("POST", "/api/auth/phone/check-code", {"phone": phone, "code": "000000"
                                                      if code != "000000" else "111111"})
    check("otp wrong code rejected", s == 401, f"{s} {r}")
    s, r = req("POST", "/api/auth/phone/check-code", {"phone": phone, "code": code})
    check("otp correct code verified", s == 200, f"{s} {r}")
    s, r = req("POST", "/api/auth/phone/check-code", {"phone": phone, "code": code})
    check("otp code one-shot", s == 401, f"{s} {r}")


def wallet_tokens_flow(admin_tok, user_tok):
    """Admins manage platform tokens; the user wallet mirrors enabled rows."""
    s, r = req("GET", "/api/wallet/assets", token=user_tok)
    check("wallet assets from platform tokens", s == 200
          and "BTC" in r.get("assets", {}) and "USDT" in r.get("assets", {}), f"{s} {r}")
    s, r = req("GET", "/api/admin/wallet/tokens", token=user_tok)
    check("user token cannot list platform tokens", s == 401, f"{s} {r}")
    s, r = req("POST", "/api/admin/wallet/tokens", {
        "symbol": "CHAT", "name": "ChatApp Token", "chain": "polygon",
        "contract_address": "0x1234567890abcdef1234567890abcdef12345678",
        "decimals": 18}, token=admin_tok)
    check("admin adds platform token", s in (200, 201) and r.get("id"), f"{s} {r}")
    token_id = r.get("id")
    s, r = req("GET", "/api/wallet/assets", token=user_tok)
    check("new token visible in wallet", s == 200
          and "polygon" in (r.get("assets", {}).get("CHAT") or []), f"{s} {r}")
    s, r = req("POST", "/api/wallet/accounts", {"asset": "CHAT", "chain": "polygon"},
               token=user_tok)
    check("user creates account for platform token", s in (200, 201), f"{s} {r}")
    s, r = req("POST", f"/api/admin/wallet/tokens/{token_id}/status",
               {"enabled": False}, token=admin_tok)
    check("admin disables token", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/wallet/assets", token=user_tok)
    check("disabled token hidden from wallet", s == 200
          and not (r.get("assets", {}).get("CHAT")), f"{s} {r}")
    s, r = req("DELETE", f"/api/admin/wallet/tokens/{token_id}", token=admin_tok)
    check("token with accounts cannot be deleted", s == 409, f"{s} {r}")
    s, r = req("POST", f"/api/admin/wallet/tokens/{token_id}/status",
               {"enabled": True}, token=admin_tok)
    check("admin re-enables token", s == 200, f"{s} {r}")


def calls_flow(user_tok, conv_id):
    """Self-built SFU: room tickets, TURN credentials, live discovery."""
    s, r = req("POST", "/api/calls/rooms", {
        "conversation_id": conv_id, "mode": "meeting"}, token=user_tok)
    ice = r.get("ice_servers") or []
    check("meeting room ticket issued", s == 200 and r.get("ticket")
          and r.get("sfu_url") and any("stun:" in str(i.get("urls")) for i in ice)
          and any("turn:" in str(i.get("urls")) for i in ice), f"{s} {r}")
    room_id = r.get("room_id")
    s, r = req("POST", f"/api/calls/rooms/{room_id}/join", {}, token=user_tok)
    check("meeting join ticket", s == 200 and r.get("role") == "publisher", f"{s} {r}")
    s, r = req("POST", "/api/calls/rooms", {
        "conversation_id": conv_id, "mode": "live"}, token=user_tok)
    check("live broadcast room created", s == 200 and r.get("mode") == "live", f"{s} {r}")
    live_room = r.get("room_id")
    s, r = req("POST", f"/api/calls/rooms/{live_room}/join", {}, token=user_tok)
    check("live join hands subscriber ticket", s == 200
          and r.get("role") == "subscriber", f"{s} {r}")
    s, r = req("GET", "/api/live", token=user_tok)
    check("live discovery", s == 200 and isinstance(r.get("live"), list), f"{s} {r}")


def social2_flow(alice_tok, bob_tok, conv):
    # unrepost: bob reposts then unreposts alice's post
    s, r = req("POST", "/api/posts", {"body": "original for repost test"}, token=alice_tok)
    post_id = r.get("id")
    s, r = req("POST", f"/api/posts/{post_id}/repost", {"quote": ""}, token=bob_tok)
    check("repost still works", s in (200, 201), f"{s} {r}")
    s, r = req("DELETE", f"/api/posts/{post_id}/repost", token=bob_tok)
    check("unrepost", s == 200 and r.get("removed"), f"{s} {r}")
    s, r = req("GET", f"/api/users/{post_id}", token=bob_tok)  # 404 expected (post id not user)
    s, r = req("GET", "/api/feed", token=bob_tok)
    feed_post = next((p for p in r.get("posts", []) if p["id"] == post_id), None)
    check("share_count back to 0", feed_post and feed_post["share_count"] == 0,
          f"{feed_post}")

    # edit post (author only)
    s, r = req("PATCH", f"/api/posts/{post_id}", {"body": "edited body"}, token=bob_tok)
    check("edit post by non-author rejected", s == 404, f"{s} {r}")
    s, r = req("PATCH", f"/api/posts/{post_id}", {"body": "edited body"}, token=alice_tok)
    check("edit post by author", s == 200, f"{s} {r}")

    # threads
    s, r = req("POST", "/api/posts",
               {"body": "thread reply 1", "thread_parent_id": post_id}, token=alice_tok)
    t1 = r.get("id")
    check("thread post created", s in (200, 201) and t1, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/thread", token=bob_tok)
    bodies = [p["body"] for p in r.get("posts", [])]
    check("thread lists parent + reply",
          s == 200 and "edited body" in bodies and "thread reply 1" in bodies,
          f"{s} {bodies}")

    # saved messages
    s, r = req("POST", "/api/conversations/saved", {}, token=alice_tok)
    saved = r.get("conversation_id")
    check("saved messages created", s == 200 and saved, f"{s} {r}")
    s, r2 = req("POST", "/api/conversations/saved", {}, token=alice_tok)
    check("saved messages idempotent", s == 200 and r2.get("conversation_id") == saved,
          f"{s} {r2}")
    # bob cannot read alice's saved chat
    s, r = req("GET", f"/api/conversations/{saved}/messages", token=bob_tok)
    check("saved messages private", s == 403, f"{s} {r}")

    # pins + forward use the story-reply DM (fresh, non-deleted messages)
    s, r = req("POST", "/api/posts", {"type": "story", "body": "story for pins"},
               token=alice_tok)
    pin_story = r.get("id")
    s, r = req("POST", f"/api/stories/{pin_story}/reply", {"body": "pin me"}, token=bob_tok)
    conv = r.get("conversation_id")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice_tok)
    msgs = r.get("messages", [])
    msg_id = msgs[0]["id"] if msgs else None
    check("messages available for pin test", msg_id is not None)
    s, r = req("POST", f"/api/conversations/{conv}/pins/{msg_id}", {}, token=alice_tok)
    check("pin message", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/pins", token=bob_tok)
    check("pins listed", s == 200 and any(p["id"] == msg_id for p in r.get("pins", [])),
          f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice_tok)
    pinned_msg = next((m for m in r.get("messages", []) if m["id"] == msg_id), None)
    check("message shows pinned flag", pinned_msg and pinned_msg.get("pinned"), f"{pinned_msg}")
    s, r = req("DELETE", f"/api/conversations/{conv}/pins/{msg_id}", token=alice_tok)
    check("unpin message", s == 200, f"{s} {r}")

    # forward with attribution
    s, r = req("POST", f"/api/messages/{msg_id}/forward",
               {"conversation_id": saved}, token=alice_tok)
    fwd_id = r.get("message_id")
    check("forward message", s == 200 and fwd_id, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{saved}/messages", token=alice_tok)
    fwd = next((m for m in r.get("messages", []) if m["id"] == fwd_id), None)
    check("forwarded attribution present", fwd and fwd.get("forwarded_from") == msg_id,
          f"{fwd}")

    # story engagement
    s, r = req("POST", "/api/posts", {"type": "story", "body": "story for engagement"},
               token=alice_tok)
    story_id = r.get("id")
    check("story created", s in (200, 201) and story_id, f"{s} {r}")
    s, r = req("POST", f"/api/stories/{story_id}/view", {}, token=bob_tok)
    check("story view recorded", s == 200 and r.get("recorded"), f"{s} {r}")
    s, r = req("POST", f"/api/stories/{story_id}/view", {}, token=bob_tok)
    check("story view deduped", s == 200 and not r.get("recorded"), f"{s} {r}")
    s, r = req("GET", f"/api/stories/{story_id}/viewers", token=bob_tok)
    check("viewers hidden from non-author", s == 403, f"{s} {r}")
    s, r = req("GET", f"/api/stories/{story_id}/viewers", token=alice_tok)
    check("viewers visible to author", s == 200 and len(r.get("viewers", [])) == 1, f"{s} {r}")
    s, r = req("POST", f"/api/stories/{story_id}/react", {"emoji": "🔥"}, token=bob_tok)
    check("story reaction", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/stories/{story_id}/reply", {"body": "nice story!"}, token=bob_tok)
    dm = r.get("conversation_id")
    check("story reply opens DM", s == 200 and dm, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{dm}/messages", token=alice_tok)
    dm_msgs = r.get("messages", [])
    check("story reply delivered with story ref",
          dm_msgs and dm_msgs[0].get("story_id") == story_id, f"{dm_msgs}")
    s, r = req("POST", f"/api/stories/{story_id}/reply", {"body": "self reply"}, token=alice_tok)
    check("self story reply rejected", s == 400, f"{s} {r}")

    # scheduled messages
    send_at = (datetime.now(timezone.utc) + timedelta(seconds=6)).isoformat()
    s, r = req("POST", f"/api/conversations/{conv}/schedule",
               {"body": "scheduled hello", "send_at": send_at}, token=alice_tok)
    sched_id = r.get("id")
    check("schedule message", s == 201 and sched_id, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/scheduled", token=alice_tok)
    check("scheduled listed", s == 200 and any(x["id"] == sched_id for x in r.get("scheduled", [])),
          f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/scheduled", token=bob_tok)
    check("scheduled private to sender",
          s == 200 and not any(x["id"] == sched_id for x in r.get("scheduled", [])), f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob_tok)
    check("not delivered before send_at",
          not any(m["body"] == "scheduled hello" for m in r.get("messages", [])), f"{s}")
    time.sleep(8)
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob_tok)
    check("delivered after send_at",
          any(m["body"] == "scheduled hello" for m in r.get("messages", [])), f"{s}")
    # cancel flow
    send_at2 = (datetime.now(timezone.utc) + timedelta(hours=1)).isoformat()
    s, r = req("POST", f"/api/conversations/{conv}/schedule",
               {"body": "cancel me", "send_at": send_at2}, token=alice_tok)
    cancel_id = r.get("id")
    s, r = req("DELETE", f"/api/scheduled/{cancel_id}", token=bob_tok)
    check("cancel by non-sender rejected", s == 404, f"{s} {r}")
    s, r = req("DELETE", f"/api/scheduled/{cancel_id}", token=alice_tok)
    check("cancel scheduled", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/conversations/{conv}/schedule",
               {"body": "too soon", "send_at": datetime.now(timezone.utc).isoformat()},
               token=alice_tok)
    check("past send_at rejected", s == 400, f"{s} {r}")

    # comment likes
    s, r = req("POST", f"/api/posts/{post_id}/comments", {"body": "likeable comment"},
               token=alice_tok)
    cid = r.get("id")
    check("comment created", s in (200, 201) and cid, f"{s} {r}")
    s, r = req("POST", f"/api/comments/{cid}/like", {}, token=bob_tok)
    check("like comment", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/comments", token=bob_tok)
    lc = next((c for c in r.get("comments", []) if c["id"] == cid), None)
    check("comment like count + liked_by_me",
          lc and lc.get("like_count") == 1 and lc.get("liked_by_me"), f"{lc}")
    s, r = req("GET", f"/api/notifications", token=alice_tok)
    check("comment like notifies author",
          any(n["kind"] == "comment_like" for n in r.get("notifications", [])), f"{s}")
    s, r = req("DELETE", f"/api/comments/{cid}/like", token=bob_tok)
    check("unlike comment", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post_id}/comments", token=bob_tok)
    lc = next((c for c in r.get("comments", []) if c["id"] == cid), None)
    check("comment unlike reflected", lc and lc.get("like_count") == 0 and not lc.get("liked_by_me"),
          f"{lc}")
    # nested reply shows parent_id
    s, r = req("POST", f"/api/posts/{post_id}/comments",
               {"body": "nested reply", "parent_id": cid}, token=bob_tok)
    rid = r.get("id")
    s, r = req("GET", f"/api/posts/{post_id}/comments", token=alice_tok)
    rc = next((c for c in r.get("comments", []) if c["id"] == rid), None)
    check("reply has parent_id", rc and rc.get("parent_id") == cid, f"{rc}")

    # notifications mark read
    s, r = req("POST", "/api/notifications/read", {}, token=alice_tok)
    check("mark notifications read", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/notifications", token=alice_tok)
    check("all notifications read",
          all(n.get("read_at") for n in r.get("notifications", [])), f"{s}")

    # message search
    s, r = req("GET", f"/api/conversations/{conv}/search?q=pin", token=alice_tok)
    check("message search finds hit",
          s == 200 and any("pin me" in m["body"] for m in r.get("messages", [])), f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/search?q=zzzznope", token=alice_tok)
    check("message search no hit", s == 200 and r.get("messages") == [], f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/search?q=x", token=alice_tok)
    check("message search short query rejected", s == 400, f"{s} {r}")

    # share post to chat
    s, r = req("POST", f"/api/posts/{post_id}/share", {"conversation_id": conv}, token=bob_tok)
    check("share post to chat", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice_tok)
    check("shared post appears in chat",
          any(f"post:{post_id}" in m["body"] for m in r.get("messages", [])), f"{s}")

    # disappearing messages (TTL)
    s, r = req("PUT", f"/api/conversations/{conv}/ttl", {"ttl_seconds": 999}, token=alice_tok)
    check("invalid ttl rejected", s == 400, f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{conv}/ttl", {"ttl_seconds": 3600}, token=alice_tok)
    check("set ttl", s == 200 and r.get("ttl_seconds") == 3600, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/ttl", token=bob_tok)
    check("get ttl", s == 200 and r.get("ttl_seconds") == 3600, f"{s} {r}")
    s, r = req("POST", f"/api/posts/{post_id}/share", {"conversation_id": conv}, token=bob_tok)
    check("share with ttl active", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice_tok)
    ttl_msgs = [m for m in r.get("messages", []) if m.get("expires_at")]
    check("new message has expires_at", len(ttl_msgs) >= 1, f"{r.get('messages', [])[:1]}")
    old_msgs = [m for m in r.get("messages", []) if not m.get("expires_at")]
    check("pre-ttl messages unchanged", len(old_msgs) >= 1, f"{s}")
    # expire one manually; it must disappear from list + search
    exp_id = ttl_msgs[0]["id"]
    dburl = os.environ.get("DATABASE_URL",
                           "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
    import subprocess as sp
    sp.run(["psql", dburl, "-c",
            f"UPDATE messages SET expires_at = now() - interval '1 minute' WHERE id='{exp_id}'"],
           check=True, capture_output=True)
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice_tok)
    check("expired message hidden from list",
          all(m["id"] != exp_id for m in r.get("messages", [])), f"{s}")
    s, r = req("GET", f"/api/conversations/{conv}/search?q=Shared", token=alice_tok)
    check("expired message hidden from search",
          all(m["id"] != exp_id for m in r.get("messages", [])), f"{s}")
    s, r = req("PUT", f"/api/conversations/{conv}/ttl", {"ttl_seconds": 0}, token=alice_tok)
    check("disable ttl", s == 200, f"{s} {r}")

    # member list
    s, r = req("GET", f"/api/conversations/{conv}/members", token=alice_tok)
    check("members listed",
          s == 200 and len(r.get("members", [])) == 2
          and any(m["role"] == "owner" for m in r.get("members", [])), f"{s} {r}")


def grant_superadmin(username):
    """Grant superadmin directly in the DB (test bootstrap; the product path is
    first-user bootstrap + superadmin grants)."""
    import subprocess
    dburl = os.environ.get("DATABASE_URL",
                           "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
    sql = (f"INSERT INTO admin_roles (user_id, role, granted_by) "
           f"SELECT id, 'superadmin', id FROM users WHERE username='{username}' "
           f"ON CONFLICT DO NOTHING")
    subprocess.run(["psql", dburl, "-c", sql], check=True,
                   capture_output=True)


def cluster_flow(admin_tok):
    secret = os.environ.get("CLUSTER_SECRET", "test-cluster-secret")

    def creq(method, path, body=None, hdr_secret=None):
        url = BASE + path
        data = json.dumps(body).encode() if body is not None else None
        r = urllib.request.Request(url, data=data, method=method)
        r.add_header("Content-Type", "application/json")
        if hdr_secret:
            r.add_header("X-Cluster-Secret", hdr_secret)
        try:
            with urllib.request.urlopen(r) as resp:
                return resp.status, json.loads(resp.read() or b"{}")
        except urllib.error.HTTPError as e:
            try:
                return e.code, json.loads(e.read() or b"{}")
            except Exception:
                return e.code, {}

    # bad secret rejected
    s, r = creq("POST", "/api/cluster/heartbeat",
                {"node_id": "n-evil", "region": "us", "api_url": "http://evil"},
                hdr_secret="wrong")
    check("cluster heartbeat bad secret rejected", s == 403, f"{s} {r}")

    # register two sibling nodes
    for nid, region, load in (("n-us-1", "us", 10), ("n-eu-1", "eu", 50)):
        s, r = creq("POST", "/api/cluster/heartbeat",
                    {"node_id": nid, "region": region,
                     "api_url": f"http://{nid}.internal", "load": load},
                    hdr_secret=secret)
        check(f"cluster heartbeat {nid}", s == 200, f"{s} {r}")

    # region routing prefers least-loaded node in region
    s, r = req("GET", "/api/cluster/route?region=us")
    check("cluster route region", s == 200 and r.get("node_id") == "n-us-1", f"{s} {r}")
    # unknown region falls back to global least-loaded
    s, r = req("GET", "/api/cluster/route?region=zz")
    check("cluster route fallback", s == 200 and r.get("node_id") == "n-us-1", f"{s} {r}")
    # shard key routing is deterministic
    s, r1 = req("GET", "/api/cluster/route?key=conv-42")
    s2, r2 = req("GET", "/api/cluster/route?key=conv-42")
    check("cluster shard deterministic",
          s == 200 and r1.get("node_id") == r2.get("node_id"), f"{r1} {r2}")

    # admin node list + drain + remove
    s, r = req("GET", "/api/cluster/nodes", token=admin_tok)
    check("cluster nodes listed (admin)",
          s == 200 and len(r.get("nodes", [])) >= 2, f"{s} {r}")
    s, r = req("POST", "/api/cluster/nodes/n-eu-1/drain", {}, token=admin_tok)
    check("cluster drain", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/cluster/route?region=eu")
    check("drained node not routed", s == 200 and r.get("node_id") != "n-eu-1", f"{s} {r}")
    s, r = req("DELETE", "/api/cluster/nodes/n-eu-1", token=admin_tok)
    check("cluster remove", s == 200, f"{s} {r}")

    # non-admin cannot manage fleet
    s, r = req("POST", "/api/auth/register",
               {"username": f"plain{int(time.time())}", "email": f"plain{int(time.time())}@test.dev",
                "password": "Passw0rd!x", "display_name": "Plain"})
    plain_tok = r.get("access_token")
    s, r = req("GET", "/api/cluster/nodes", token=plain_tok)
    check("cluster nodes forbidden for non-admin", s == 401, f"{s} {r}")


# ---- minimal CBOR encoder (definite-length only) ----

def cbor_encode(v):
    if isinstance(v, bool):
        return b"\xf5" if v else b"\xf4"
    if isinstance(v, int):
        if v >= 0:
            return cbor_head(0, v)
        return cbor_head(1, -1 - v)
    if isinstance(v, bytes):
        return cbor_head(2, len(v)) + v
    if isinstance(v, str):
        return cbor_head(3, len(v.encode())) + v.encode()
    if isinstance(v, (list, tuple)):
        return cbor_head(4, len(v)) + b"".join(cbor_encode(x) for x in v)
    if isinstance(v, dict):
        return cbor_head(5, len(v)) + b"".join(
            cbor_encode(k) + cbor_encode(x) for k, x in v.items())
    raise TypeError(v)


def cbor_head(major, n):
    if n < 24:
        return bytes([(major << 5) | n])
    if n < 256:
        return bytes([(major << 5) | 24, n])
    if n < 65536:
        return bytes([(major << 5) | 25]) + n.to_bytes(2, "big")
    return bytes([(major << 5) | 26]) + n.to_bytes(4, "big")


def passkey_flow(token, username):
    """Real WebAuthn register+login using a software ECDSA P-256 authenticator."""
    from cryptography.hazmat.primitives.asymmetric import ec, utils
    from cryptography.hazmat.primitives import hashes

    RP_ID = "localhost"
    ORIGIN = "http://localhost:3000"
    rp_hash = hashlib.sha256(RP_ID.encode()).digest()

    priv = ec.generate_private_key(ec.SECP256R1())
    nums = priv.public_key().public_numbers()
    x = nums.x.to_bytes(32, "big")
    y = nums.y.to_bytes(32, "big")
    cose_key = cbor_encode({1: 2, 3: -7, -1: 1, -2: x, -3: y})
    cred_id = os.urandom(32)

    # registration
    s, r = req("POST", "/api/auth/passkey/register/begin", {}, token=token)
    check("passkey register begin", s == 200 and r.get("challenge"), f"{s} {r}")
    challenge = r.get("challenge")
    client_data = json.dumps({
        "type": "webauthn.create", "challenge": challenge, "origin": ORIGIN,
        "crossOrigin": False}).encode()
    auth_data = (rp_hash + bytes([0x01 | 0x40]) + (1).to_bytes(4, "big")
                 + os.urandom(16) + len(cred_id).to_bytes(2, "big") + cred_id + cose_key)
    att_obj = cbor_encode({"fmt": "none", "attStmt": {}, "authData": auth_data})
    s, r = req("POST", "/api/auth/passkey/register/finish", {
        "name": "test-key",
        "response": {
            "clientDataJSON": b64u(client_data),
            "attestationObject": b64u(att_obj),
            "transports": ["internal"]}}, token=token)
    check("passkey register finish", s in (200, 201), f"{s} {r}")

    s, r = req("GET", "/api/auth/passkeys", token=token)
    check("passkey listed", s == 200 and len(r.get("passkeys", [])) == 1, f"{s} {r}")
    pk_id = r["passkeys"][0]["id"] if r.get("passkeys") else None

    # tampered challenge must fail
    s, r = req("POST", "/api/auth/passkey/login/begin", {"username": username})
    check("passkey login begin", s == 200 and r.get("challenge"), f"{s} {r}")
    bad_client = json.dumps({
        "type": "webauthn.get", "challenge": "wrong", "origin": ORIGIN}).encode()
    bad_auth = rp_hash + b"\x01" + (2).to_bytes(4, "big")
    bad_sig = priv.sign(bad_auth + hashlib.sha256(bad_client).digest(), ec.ECDSA(hashes.SHA256()))
    s, r = req("POST", "/api/auth/passkey/login/finish", {
        "id": b64u(cred_id),
        "response": {"clientDataJSON": b64u(bad_client),
                     "authenticatorData": b64u(bad_auth),
                     "signature": b64u(bad_sig)}})
    check("passkey bad challenge rejected", s == 400, f"{s} {r}")

    # valid login
    s, r = req("POST", "/api/auth/passkey/login/begin", {"username": username})
    challenge = r.get("challenge")
    client_data = json.dumps({
        "type": "webauthn.get", "challenge": challenge, "origin": ORIGIN,
        "crossOrigin": False}).encode()
    auth_data = rp_hash + b"\x01" + (3).to_bytes(4, "big")
    sig = priv.sign(auth_data + hashlib.sha256(client_data).digest(), ec.ECDSA(hashes.SHA256()))
    s, r = req("POST", "/api/auth/passkey/login/finish", {
        "id": b64u(cred_id),
        "response": {"clientDataJSON": b64u(client_data),
                     "authenticatorData": b64u(auth_data),
                     "signature": b64u(sig)}})
    check("passkey login issues tokens", s == 200 and r.get("access_token"), f"{s} {r}")

    # forged signature must fail
    s, r = req("POST", "/api/auth/passkey/login/begin", {"username": username})
    challenge = r.get("challenge")
    client_data = json.dumps({
        "type": "webauthn.get", "challenge": challenge, "origin": ORIGIN}).encode()
    auth_data = rp_hash + b"\x01" + (4).to_bytes(4, "big")
    other = ec.generate_private_key(ec.SECP256R1())
    forged = other.sign(auth_data + hashlib.sha256(client_data).digest(), ec.ECDSA(hashes.SHA256()))
    s, r = req("POST", "/api/auth/passkey/login/finish", {
        "id": b64u(cred_id),
        "response": {"clientDataJSON": b64u(client_data),
                     "authenticatorData": b64u(auth_data),
                     "signature": b64u(forged)}})
    check("passkey forged signature rejected", s == 401, f"{s} {r}")

    # delete
    if pk_id:
        s, r = req("DELETE", f"/api/auth/passkeys/{pk_id}", token=token)
        check("passkey delete", s == 200, f"{s} {r}")


def b64u(b):
    return base64.urlsafe_b64encode(b).rstrip(b"=").decode()


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
