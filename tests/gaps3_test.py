#!/usr/bin/env python3
"""End-to-end checks for gap-pack-3 (migration 018): privacy suite depth
(presence/phone granularity, data saver, account TTL), sessions management,
chat archive, sticker packs, chat folders, lists + list feed, bookmark
folders, profile visitors, playlists, paid verification flow, reply policy,
content warnings, alt text, hidden replies, creator comment pinning, public
group handles, granular group-admin permissions.

Runs against a live API on :8080 with migrations 001-018 applied. No mocks.
"""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req, grant_superadmin


def register(name):
    # Register is rate-limited (10/min); retry with backoff so back-to-back
    # suite runs don't cascade into spurious failures.
    for _ in range(6):
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
    alice = register(f"g3a{ts}")
    bob = register(f"g3b{ts}")
    carol = register(f"g3c{ts}")

    s, r = req("GET", "/api/me", token=bob)
    bob_id = r.get("id") or r.get("user", {}).get("id")
    s, r = req("GET", "/api/me", token=carol)
    carol_id = r.get("id") or r.get("user", {}).get("id")
    s, r = req("GET", "/api/me", token=alice)
    alice_id = r.get("id") or r.get("user", {}).get("id")
    check("user ids", bool(alice_id and bob_id and carol_id), f"{alice_id} {bob_id} {carol_id}")

    # ---------- privacy settings ----------
    s, r = req("GET", "/api/me/privacy", token=alice)
    check("privacy defaults", s == 200 and r.get("last_seen_privacy") == "everyone", f"{s} {r}")
    s, r = req("PUT", "/api/me/privacy", {"last_seen_privacy": "nobody", "data_saver": True}, token=alice)
    check("set privacy", s == 200 and r.get("data_saver") is True, f"{s} {r}")
    s, r = req("GET", f"/api/users/{alice_id}/presence", token=bob)
    check("presence hidden by nobody", s == 200 and r.get("last_seen") is None and r.get("online") is False,
          f"{s} {r}")
    s, r = req("PUT", "/api/me/privacy", {"last_seen_privacy": "contacts"}, token=alice)
    check("presence contacts: non-contact hidden", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/users/{alice_id}/presence", token=bob)
    check("non-contact sees nothing", s == 200 and r.get("last_seen") is None, f"{s} {r}")
    s, r = req("POST", f"/api/users/{alice_id}/follow", token=bob)
    check("bob follows alice", s in (200, 201), f"{s} {r}")
    s, r = req("GET", f"/api/users/{alice_id}/presence", token=bob)
    check("contact sees presence row", s == 200, f"{s} {r}")
    s, r = req("PUT", "/api/me/privacy", {"last_seen_privacy": "everyone", "account_ttl_days": 90}, token=alice)
    check("account ttl set", s == 200 and r.get("account_ttl_days") == 90, f"{s} {r}")
    s, r = req("PUT", "/api/me/privacy", {"account_ttl_days": 45}, token=alice)
    check("invalid ttl rejected", s == 400, f"{s} {r}")
    s, r = req("PUT", "/api/me/privacy", {"last_seen_privacy": "aliens"}, token=alice)
    check("invalid privacy value rejected", s == 400, f"{s} {r}")

    # ---------- sessions ----------
    s, r = req("GET", "/api/me/sessions", token=alice)
    check("list sessions", s == 200 and len(r.get("sessions", [])) >= 1, f"{s} {r}")
    sid = r["sessions"][0]["id"]
    s, r = req("DELETE", f"/api/me/sessions/{sid}", token=alice)
    check("revoke session", s == 200, f"{s} {r}")
    s, r = req("DELETE", f"/api/me/sessions/{sid}", token=alice)
    check("revoke again 404", s == 404, f"{s} {r}")

    # ---------- conversations: group, handle, archive, sticker ----------
    s, r = req("POST", "/api/conversations", {"is_group": True, "title": f"g3 group {ts}",
                                              "member_ids": [bob_id]}, token=alice)
    check("create group", s == 201, f"{s} {r}")
    conv = r.get("id") or r.get("conversation", {}).get("id")

    s, r = req("PUT", f"/api/conversations/{conv}/handle", {"handle": f"g3pub{ts}"}, token=alice)
    check("set public handle", s == 200 and r.get("handle") == f"g3pub{ts}", f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{conv}/handle", {"handle": "x"}, token=alice)
    check("invalid handle rejected", s == 400, f"{s} {r}")
    s, r = req("GET", f"/api/handles/g3pub{ts}", token=carol)
    check("lookup by handle", s == 200 and r.get("id") == conv, f"{s} {r}")
    s, r = req("POST", f"/api/handles/g3pub{ts}/join", token=carol)
    check("join by handle", s == 201 and r.get("conversation_id") == conv, f"{s} {r}")
    s, r = req("PUT", f"/api/conversations/{conv}/handle", {"handle": f"stolen{ts}"}, token=carol)
    check("non-owner cannot set handle", s == 403, f"{s} {r}")

    # granular admin perms: promote bob to admin, grant explicit perms
    s, r = req("PUT", f"/api/conversations/{conv}/members/{bob_id}/role", {"role": "admin"}, token=alice)
    check("promote bob to admin", s == 200, f"{s} {r}")
    promoted = s == 200
    s, r = req("PUT", f"/api/conversations/{conv}/members/{alice_id}/role", {"role": "admin"}, token=bob)
    check("admin cannot change roles", s == 403, f"{s} {r}")
    if promoted:
        s, r = req("PUT", f"/api/conversations/{conv}/members/{bob_id}/permissions",
                   {"can_invite": True, "can_pin": True}, token=alice)
        check("set admin permissions", s == 200, f"{s} {r}")
        s, r = req("PUT", f"/api/conversations/{conv}/members/{carol_id}/permissions",
                   {"can_invite": True}, token=alice)
        check("perms on non-admin 404", s == 404, f"{s} {r}")

    s, r = req("PUT", f"/api/conversations/{conv}/archive", token=bob)
    check("archive conversation", s == 200 and r.get("archived") is True, f"{s} {r}")
    s, r = req("GET", "/api/conversations", token=bob)
    arch = [c for c in r.get("conversations", []) if c.get("id") == conv]
    check("archived flag in list", s == 200 and arch and arch[0].get("archived") is True, f"{s} {r}")
    s, r = req("DELETE", f"/api/conversations/{conv}/archive", token=bob)
    check("unarchive conversation", s == 200 and r.get("archived") is False, f"{s} {r}")

    # stickers
    s, r = req("POST", "/api/sticker-packs", {"name": f"g3pack{ts}", "title": "Gap3 Pack"}, token=alice)
    check("create sticker pack", s == 201, f"{s} {r}")
    pack = r.get("id")
    s, r = req("POST", f"/api/sticker-packs/{pack}/stickers",
               {"emoji": "\U0001f680", "media_url": "https://cdn.test/sticker1.webp"}, token=alice)
    check("add sticker", s == 201, f"{s} {r}")
    sticker = r.get("id")
    s, r = req("POST", f"/api/sticker-packs/{pack}/stickers",
               {"emoji": "x", "media_url": "https://cdn.test/x.webp"}, token=bob)
    check("non-owner cannot add sticker", s == 403, f"{s} {r}")
    s, r = req("GET", f"/api/sticker-packs/{pack}/stickers", token=bob)
    check("list stickers", s == 200 and len(r.get("stickers", [])) == 1, f"{s} {r}")
    s, r = req("POST", f"/api/conversations/{conv}/sticker", {"sticker_id": sticker}, token=bob)
    check("send sticker message", s == 201, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice)
    kinds = [m.get("kind") for m in r.get("messages", [])]
    check("sticker message in history", s == 200 and "sticker" in kinds, f"{s} {kinds}")

    # chat folders
    s, r = req("POST", "/api/me/chat-folders", {"name": "Work"}, token=alice)
    check("create chat folder", s == 201, f"{s} {r}")
    folder = r.get("id")
    s, r = req("PUT", f"/api/me/chat-folders/{folder}/conversations", {"conversation_ids": [conv]}, token=alice)
    check("set folder conversations", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/chat-folders", token=alice)
    fs = [f for f in r.get("folders", []) if f.get("id") == folder]
    check("folder lists conversation", s == 200 and fs and conv in fs[0].get("conversation_ids", []), f"{s} {r}")
    s, r = req("DELETE", f"/api/me/chat-folders/{folder}", token=alice)
    check("delete chat folder", s == 200, f"{s} {r}")

    # ---------- posts: reply policy, content warning, alt text ----------
    s, r = req("POST", "/api/posts", {
        "body": "reply-limited post", "reply_policy": "following",
        "content_warning": "spoiler", "sensitive": True,
        "media": [{"kind": "image", "url": "https://cdn.test/p.png", "alt_text": "a test image"}]}, token=alice)
    check("create post with cw+policy", s == 201, f"{s} {r}")
    post = r.get("id") or r.get("post", {}).get("id")
    s, r = req("GET", "/api/feed", token=bob)
    mine = [p for p in r.get("posts", []) if p.get("id") == post]
    check("cw/sensitive/policy in feed payload",
          s == 200 and mine and mine[0].get("content_warning") == "spoiler"
          and mine[0].get("sensitive") is True and mine[0].get("reply_policy") == "following",
          f"{s} {mine[:1]}")
    check("alt text in feed media", mine and mine[0]["media"][0].get("alt_text") == "a test image",
          f"{mine[:1]}")

    s, r = req("POST", f"/api/posts/{post}/comments", {"body": "nice"}, token=bob)
    check("non-followed reply blocked", s == 403, f"{s} {r}")
    s, r = req("POST", f"/api/users/{bob_id}/follow", token=alice)
    check("alice follows bob", s in (200, 201), f"{s} {r}")
    s, r = req("POST", f"/api/posts/{post}/comments", {"body": "nice"}, token=bob)
    check("followed user can reply", s == 201, f"{s} {r}")
    comment = r.get("id") or r.get("comment", {}).get("id")

    s, r = req("POST", "/api/posts", {"body": "nobody can reply", "reply_policy": "nobody"}, token=alice)
    post2 = r.get("id") or r.get("post", {}).get("id")
    s, r = req("POST", f"/api/posts/{post2}/comments", {"body": "x"}, token=bob)
    check("nobody policy blocks replies", s == 403, f"{s} {r}")
    s, r = req("POST", f"/api/posts/{post2}/comments", {"body": "author ok"}, token=alice)
    check("author can always reply", s == 201, f"{s} {r}")

    # hidden replies
    s, r = req("POST", f"/api/comments/{comment}/hide", token=alice)
    check("author hides reply", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post}/comments", token=carol)
    check("hidden reply invisible to others", s == 200 and all(c.get("id") != comment for c in r.get("comments", [])),
          f"{s} {r}")
    s, r = req("GET", f"/api/posts/{post}/comments", token=alice)
    check("hidden reply visible to post author", s == 200 and any(c.get("id") == comment for c in r.get("comments", [])),
          f"{s} {r}")
    s, r = req("POST", f"/api/comments/{comment}/hide", token=carol)
    check("stranger cannot hide", s == 403, f"{s} {r}")
    s, r = req("POST", f"/api/comments/{comment}/unhide", token=alice)
    check("unhide reply", s == 200, f"{s} {r}")

    # creator comment pinning
    s, r = req("PUT", f"/api/posts/{post}/pinned-comment", {"comment_id": comment}, token=alice)
    check("pin comment", s == 200 and r.get("pinned_comment_id") == comment, f"{s} {r}")
    s, r = req("PUT", f"/api/posts/{post}/pinned-comment", {"comment_id": comment}, token=bob)
    check("non-author cannot pin", s == 403, f"{s} {r}")
    s, r = req("PUT", f"/api/posts/{post}/pinned-comment", {"comment_id": ""}, token=alice)
    check("clear pinned comment", s == 200, f"{s} {r}")

    # ---------- lists ----------
    s, r = req("POST", "/api/me/lists", {"name": "Favs"}, token=alice)
    check("create list", s == 201, f"{s} {r}")
    lst = r.get("id")
    s, r = req("PUT", f"/api/lists/{lst}/members/{bob_id}", token=alice)
    check("add list member", s == 201, f"{s} {r}")
    s, r = req("POST", "/api/posts", {"body": "bob list post"}, token=bob)
    bpost = r.get("id") or r.get("post", {}).get("id")
    s, r = req("GET", f"/api/lists/{lst}/feed", token=alice)
    check("list feed shows member post", s == 200 and any(p.get("id") == bpost for p in r.get("posts", [])),
          f"{s} {r}")
    s, r = req("GET", f"/api/lists/{lst}/feed", token=bob)
    check("others cannot read my list feed", s == 404, f"{s} {r}")
    s, r = req("DELETE", f"/api/lists/{lst}/members/{bob_id}", token=alice)
    check("remove list member", s == 200, f"{s} {r}")
    s, r = req("DELETE", f"/api/me/lists/{lst}", token=alice)
    check("delete list", s == 200, f"{s} {r}")

    # ---------- bookmark folders ----------
    s, r = req("POST", "/api/me/bookmark-folders", {"name": "Read later"}, token=alice)
    check("create bookmark folder", s == 201, f"{s} {r}")
    bf = r.get("id")
    s, r = req("POST", f"/api/posts/{post2}/bookmark", token=alice)
    check("bookmark post", s in (200, 201), f"{s} {r}")
    s, r = req("PUT", f"/api/bookmarks/{post2}/folder", {"folder_id": bf}, token=alice)
    check("file bookmark into folder", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/bookmark-folders", token=alice)
    fs = [f for f in r.get("folders", []) if f.get("id") == bf]
    check("folder count updated", s == 200 and fs and fs[0].get("bookmark_count") == 1, f"{s} {r}")
    s, r = req("PUT", f"/api/bookmarks/{post2}/folder", {"folder_id": bf}, token=bob)
    check("cannot file others' folder", s == 404, f"{s} {r}")

    # ---------- profile visitors ----------
    s, r = req("GET", f"/api/users/{alice_id}", token=bob)
    check("view alice profile", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me/profile-visitors", token=alice)
    check("visitor recorded", s == 200 and any(v.get("id") == bob_id for v in r.get("visitors", [])), f"{s} {r}")
    s, r = req("GET", f"/api/users/{alice_id}", token=alice)
    s, r = req("GET", "/api/me/profile-visitors", token=alice)
    check("self view not recorded", all(v.get("id") != alice_id for v in r.get("visitors", [])), f"{s} {r}")

    # ---------- playlists ----------
    s, r = req("POST", "/api/me/playlists", {"title": "Best reels"}, token=alice)
    check("create playlist", s == 201, f"{s} {r}")
    pl = r.get("id")
    s, r = req("POST", "/api/posts", {"type": "reel", "body": "reel one",
                                      "media": [{"kind": "video", "url": "https://cdn.test/r.mp4"}]}, token=alice)
    reel = r.get("id") or r.get("post", {}).get("id")
    s, r = req("POST", f"/api/playlists/{pl}/items", {"post_id": reel}, token=alice)
    check("add reel to playlist", s == 201, f"{s} {r}")
    s, r = req("GET", f"/api/playlists/{pl}", token=bob)
    check("playlist readable by others", s == 200 and any(p.get("id") == reel for p in r.get("posts", [])),
          f"{s} {r}")
    s, r = req("GET", f"/api/users/{alice_id}/playlists", token=bob)
    check("public playlist list", s == 200 and any(p.get("id") == pl for p in r.get("playlists", [])), f"{s} {r}")
    s, r = req("POST", f"/api/playlists/{pl}/items", {"post_id": bpost}, token=bob)
    check("cannot add to others' playlist", s == 404, f"{s} {r}")

    # ---------- paid verification flow ----------
    s, r = req("POST", "/api/me/verification-requests", {"tier": "blue", "note": "creator"}, token=bob)
    check("request verification", s == 201 and r.get("status") == "pending", f"{s} {r}")
    vreq = r.get("id")
    s, r = req("POST", "/api/me/verification-requests", {"tier": "blue"}, token=bob)
    check("duplicate pending rejected", s == 409, f"{s} {r}")
    grant_superadmin(f"g3b{ts}")
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"g3b{ts}@test.dev", "password": "Passw0rd!123"})
    admin = r.get("access_token")
    check("admin login (superadmin)", s == 200 and bool(admin), f"{s} {r}")
    if admin:
        s, r = req("GET", "/api/admin/verification-requests?status=pending", token=admin)
        check("admin sees request", s == 200 and any(q.get("id") == vreq for q in r.get("requests", [])), f"{s} {r}")
        s, r = req("POST", f"/api/admin/verification-requests/{vreq}/review",
                   {"decision": "approved"}, token=admin)
        check("approve verification", s == 200, f"{s} {r}")
        s, r = req("GET", "/api/me", token=bob)
        verified = (r.get("is_verified") if "is_verified" in r else r.get("user", {}).get("is_verified"))
        check("badge granted", verified is True, f"{s} {r}")
        s, r = req("POST", f"/api/admin/verification-requests/{vreq}/review",
                   {"decision": "approved"}, token=admin)
        check("re-review conflicts", s == 409, f"{s} {r}")

    print("\nDONE")


if __name__ == "__main__":
    main()
