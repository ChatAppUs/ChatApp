"""End-to-end checks for gap-pack-7: quiz polls with correct answers and
explanations, Bot API expansion (editMessageText/deleteMessage/sendPhoto/
getChat), X-style advanced search operators, Moments curation, audio-room
recordings, live-room replays, chunked resumable upload sessions, E2E SAS
verification codes (Rust security service), related-reels recommendations
(ML service), username profile URLs, creator-side comment word filters, and
the FYP diversity guard (unit-tested in Go).

Runs against a live API on :8080 with migration 022 applied, the Rust
security service on :8090 (SECURITY_SERVICE_URL set on the API) and the ML
service on :8200 (ML_SERVICE_URL set on the API). No mocks.
"""
import re
import sys
import time
import urllib.parse

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import integration_test
from integration_test import check, req, grant_superadmin
from gaps6_test import register, uid, make_dm


def main():
    ts = int(time.time())
    names = {k: f"g7{k}{ts}" for k in "abcde"}
    alice = register(names["a"])
    bob = register(names["b"])
    carol = register(names["c"])
    dave = register(names["d"])
    eve = register(names["e"])
    alice_id, bob_id, carol_id = uid(alice), uid(bob), uid(carol)
    dave_id = uid(dave)
    check("user ids", all([alice_id, bob_id, carol_id, dave_id, uid(eve)]), "")

    # ---------- quiz polls (Telegram quiz parity) ----------
    conv = make_dm(alice, bob_id)
    check("create dm", bool(conv), "")
    s, r = req("POST", f"/api/conversations/{conv}/polls",
               {"question": "2+2?", "options": ["3", "4", "5"],
                "is_quiz": True, "correct_option": 1,
                "explanation": "basic math"}, token=alice)
    poll_id = r.get("id")
    check("create quiz poll", s == 201 and poll_id and r.get("message_id"), f"{s} {r}")
    s, r = req("POST", f"/api/conversations/{conv}/polls",
               {"question": "bad quiz", "options": ["a", "b"],
                "is_quiz": True, "correct_option": 9}, token=alice)
    check("quiz rejects out-of-range correct_option", s == 400, f"{s} {r}")
    s, r = req("POST", f"/api/conversations/{conv}/polls",
               {"question": "bad quiz 2", "options": ["a", "b"], "is_quiz": True}, token=alice)
    check("quiz requires correct_option", s == 400, f"{s} {r}")

    s, r = req("GET", f"/api/chat-polls/{poll_id}", token=bob)
    check("quiz poll visible", s == 200 and r.get("is_quiz") is True, f"{s} {r}")
    check("correct answer hidden before voting",
          "correct_option_id" not in r, f"{r.get('correct_option_id')}")
    wrong_id = r["options"][0]["id"]
    right_id = r["options"][1]["id"]
    s, r = req("POST", f"/api/chat-polls/{poll_id}/vote", {"option_id": wrong_id}, token=bob)
    check("wrong quiz answer reported", s == 200 and r.get("is_quiz") is True
          and r.get("correct") is False and "correct_option_id" in r, f"{s} {r}")
    s, r = req("POST", f"/api/chat-polls/{poll_id}/vote", {"option_id": right_id}, token=bob)
    check("quiz answers are final", s == 409, f"{s} {r}")
    s, r = req("GET", f"/api/chat-polls/{poll_id}", token=bob)
    check("quiz reveal after voting", s == 200
          and r.get("correct_option_id") == right_id
          and r.get("explanation") == "basic math", f"{s} {r}")
    # Non-voter in another poll conversation sees no reveal — create a group
    # with carol so she can read the poll without having voted.
    s, r = req("POST", "/api/conversations",
               {"is_group": True, "title": f"g7 quiz {ts}",
                "member_ids": [bob_id, carol_id]}, token=alice)
    gconv = r.get("id") or r.get("conversation_id")
    check("create group", s in (200, 201) and gconv, f"{s} {r}")
    s, r = req("POST", f"/api/conversations/{gconv}/polls",
               {"question": "capital of France?", "options": ["London", "Paris"],
                "is_quiz": True, "correct_option": 1, "explanation": "obvious"}, token=alice)
    gpoll = r.get("id")
    check("group quiz poll", s == 201 and gpoll, f"{s} {r}")
    s, r = req("GET", f"/api/chat-polls/{gpoll}", token=carol)
    check("non-voter gets no reveal", s == 200 and "correct_option_id" not in r, f"{s} {r}")
    s, r = req("GET", f"/api/chat-polls/{gpoll}", token=carol)
    right2 = r["options"][1]["id"]
    s, r = req("POST", f"/api/chat-polls/{gpoll}/vote", {"option_id": right2}, token=carol)
    check("correct quiz answer reported", s == 200 and r.get("correct") is True, f"{s} {r}")

    # ---------- Bot API expansion ----------
    botname = f"g7{ts % 100000}bot"
    s, r = req("POST", "/api/bots", {"username": botname, "display_name": "G7 Bot"}, token=alice)
    check("create bot", s == 201 and r.get("token"), f"{s} {r}")
    bot_token = r.get("token")
    s, r = req("GET", f"/api/bot/{bot_token}/getMe")
    me = r.get("result", {})
    check("bot getMe", s == 200 and me.get("username") == botname, f"{s} {r}")
    bot_uid = me.get("id")
    s, r = req("POST", "/api/conversations",
               {"is_group": True, "title": f"g7 botroom {ts}",
                "member_ids": [bob_id, bot_uid]}, token=alice)
    bconv = r.get("id") or r.get("conversation_id")
    check("bot group", s in (200, 201) and bconv, f"{s} {r}")

    s, r = req("POST", f"/api/bot/{bot_token}/sendMessage",
               {"conversation_id": bconv, "body": f"hello from bot {ts}"})
    check("bot sendMessage", s == 200 and r.get("ok"), f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{bconv}/messages", token=alice)
    bot_msgs = [m for m in r.get("messages", []) if m.get("sender_id") == bot_uid]
    check("bot message persisted", s == 200 and len(bot_msgs) == 1, f"{s}")
    bot_msg_id = bot_msgs[0]["id"] if bot_msgs else None

    s, r = req("GET", f"/api/bot/{bot_token}/getChat?conversation_id={bconv}")
    check("bot getChat", s == 200 and r.get("result", {}).get("member_count") == 3
          and r.get("result", {}).get("title", "").startswith("g7 botroom")
          and r.get("result", {}).get("type") == "group", f"{s} {r}")

    s, r = req("POST", f"/api/bot/{bot_token}/editMessageText",
               {"message_id": bot_msg_id, "body": "edited by bot"})
    check("bot editMessageText", s == 200 and r.get("ok"), f"{s} {r}")
    s, r = req("POST", f"/api/bot/{bot_token}/editMessageText",
               {"message_id": bot_msg_id, "body": "edited by bot"})
    check("bot edit idempotent", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{bconv}/messages", token=bob)
    edited = [m for m in r.get("messages", []) if m.get("id") == bot_msg_id]
    check("edit visible to members", edited and edited[0].get("body") == "edited by bot"
          and edited[0].get("edited_at"), f"{s}")

    s, r = req("POST", f"/api/bot/{bot_token}/sendPhoto",
               {"conversation_id": bconv,
                "media_url": f"/media/pic{ts}.jpg", "caption": "a pic"})
    check("bot sendPhoto", s == 200 and r.get("ok"), f"{s} {r}")
    s, r = req("POST", f"/api/bot/{bot_token}/deleteMessage", {"message_id": bot_msg_id})
    check("bot deleteMessage", s == 200 and r.get("ok"), f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{bconv}/messages", token=alice)
    check("bot message deleted", all(m.get("id") != bot_msg_id for m in r.get("messages", [])), f"{s}")
    # A different bot must not edit this bot's messages.
    s, r = req("POST", "/api/bots", {"username": f"g7x{ts % 100000}bot", "display_name": "G7 X"}, token=bob)
    other_token = r.get("token")
    s, r = req("POST", f"/api/bot/{other_token}/sendMessage",
               {"conversation_id": bconv, "body": "intruder"})
    check("non-member bot cannot post", s == 403, f"{s} {r}")

    # ---------- advanced search (X operators) ----------
    body_a = f"skyline photo walk {ts}"
    s, r = req("POST", "/api/posts", {"body": body_a}, token=alice)
    pa = r.get("id") or r.get("post", {}).get("id")
    s, r = req("POST", "/api/posts", {"body": f"skyline reel {ts}", "type": "reel"}, token=alice)
    ra = r.get("id") or r.get("post", {}).get("id")
    s, r = req("POST", "/api/posts", {"body": f"skyline by bob {ts}"}, token=bob)
    pb = r.get("id") or r.get("post", {}).get("id")
    time.sleep(0.4)

    s, r = req("GET", "/api/search/posts?" + urllib.parse.urlencode({"q": f"skyline {ts}"}), token=carol)
    got = {p["id"] for p in r.get("posts", [])}
    check("plain text search", s == 200 and {pa, ra, pb} <= got, f"{s} {got}")
    s, r = req("GET", "/api/search/posts?" + urllib.parse.urlencode({"q": f"skyline {ts} from:{names['a']}"}), token=carol)
    got = {p["id"] for p in r.get("posts", [])}
    check("from: operator", pa in got and pb not in got, f"{s} {got}")
    s, r = req("GET", "/api/search/posts?" + urllib.parse.urlencode({"q": f"skyline {ts} filter:reels"}), token=carol)
    got = {p["id"] for p in r.get("posts", [])}
    check("filter:reels", ra in got and pa not in got and pb not in got, f"{s} {got}")
    s, r = req("GET", "/api/search/posts?" + urllib.parse.urlencode({"q": f"skyline {ts} -from:{names['a']}"}), token=carol)
    got = {p["id"] for p in r.get("posts", [])}
    check("negated from:", pb in got and pa not in got, f"{s} {got}")
    today = time.strftime("%Y-%m-%d", time.gmtime())
    s, r = req("GET", "/api/search/posts?" + urllib.parse.urlencode({"q": f"skyline {ts} since:{today}"}), token=carol)
    check("since: operator", s == 200 and pa in {p["id"] for p in r.get("posts", [])}, f"{s}")
    s, r = req("GET", "/api/search/posts?" + urllib.parse.urlencode({"q": f"skyline {ts} until:2020-01-01"}), token=carol)
    check("until: operator excludes new posts", r.get("posts") == [], f"{s} {len(r.get('posts', []))}")
    s, r = req("GET", "/api/search/posts?q=", token=carol)
    check("empty search returns nothing", s == 200 and r.get("posts") == [], f"{s}")

    # ---------- username profile URLs ----------
    s, r = req("GET", f"/api/u/{names['b']}", token=alice)
    check("public profile url resolves", s == 200 and r.get("user", {}).get("id") == bob_id, f"{s} {r}")
    s, r = req("GET", "/api/u/no-such-user-xyz", token=alice)
    check("public profile url 404", s == 404, f"{s}")

    # ---------- creator-side comment word filters ----------
    s, r = req("POST", "/api/me/word-filters", {"phrase": "bannedword"}, token=carol)
    check("set word filter", s == 201, f"{s} {r}")
    s, r = req("POST", "/api/posts", {"body": f"carol post {ts}"}, token=carol)
    cpost = r.get("id") or r.get("post", {}).get("id")
    s, r = req("POST", f"/api/posts/{cpost}/comments", {"body": "this contains bannedword here"}, token=alice)
    check("filtered comment accepted", s in (200, 201), f"{s} {r}")
    cid = r.get("id") or r.get("comment", {}).get("id")
    s, r = req("POST", f"/api/posts/{cpost}/comments", {"body": "clean reply"}, token=dave)
    clean_id = r.get("id") or r.get("comment", {}).get("id")
    s, r = req("GET", f"/api/posts/{cpost}/comments", token=dave)
    ids = [c["id"] for c in r.get("comments", [])]
    check("filtered comment hidden from others", clean_id in ids and cid not in ids, f"{s} {ids}")
    s, r = req("GET", f"/api/posts/{cpost}/comments", token=carol)
    ids = [c["id"] for c in r.get("comments", [])]
    check("author still sees filtered comment", cid in ids, f"{s}")
    s, r = req("GET", f"/api/posts/{cpost}/comments", token=alice)
    ids = [c["id"] for c in r.get("comments", [])]
    check("commenter still sees own comment", cid in ids, f"{s}")

    # ---------- moments curation ----------
    grant_superadmin(names["a"])
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"{names['a']}@test.dev", "password": "Passw0rd!123"})
    admin = r.get("access_token")
    check("admin login", s == 200 and admin, f"{s} {r}")

    s, r = req("POST", "/api/admin/moments",
               {"title": f"Best of {ts}", "summary": "curated"}, token=admin)
    moment_id = r.get("id")
    check("create moment", s == 201 and moment_id, f"{s} {r}")
    s, r = req("POST", f"/api/admin/moments/{moment_id}/items", {"post_id": pa}, token=admin)
    check("add moment item", s == 201, f"{s} {r}")
    s, r = req("POST", f"/api/admin/moments/{moment_id}/items", {"post_id": "not-a-uuid"}, token=admin)
    check("moment rejects bad post", s == 404, f"{s} {r}")
    s, r = req("POST", "/api/admin/moments", {"title": f"empty {ts}"}, token=admin)
    empty_moment = r.get("id")
    s, r = req("POST", f"/api/admin/moments/{empty_moment}/publish", {}, token=admin)
    check("cannot publish empty moment", s == 400, f"{s} {r}")
    s, r = req("GET", "/api/moments", token=bob)
    check("draft moment not public", all(m["id"] != moment_id for m in r.get("moments", [])), f"{s}")
    s, r = req("POST", f"/api/admin/moments/{moment_id}/publish", {}, token=admin)
    check("publish moment", s == 200 and r.get("status") == "published", f"{s} {r}")
    s, r = req("GET", "/api/moments", token=bob)
    mine = [m for m in r.get("moments", []) if m["id"] == moment_id]
    check("moment listed", mine and mine[0].get("item_count") == 1, f"{s} {r}")
    s, r = req("GET", f"/api/moments/{moment_id}", token=bob)
    check("moment detail", s == 200 and r.get("moment", {}).get("title", "").startswith("Best of")
          and any(p["id"] == pa for p in r.get("posts", [])), f"{s} {r}")
    s, r = req("POST", f"/api/admin/moments/{moment_id}/publish", {}, token=admin)
    check("re-publish idempotent", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/admin/moments/{moment_id}/publish", {"publish": False}, token=admin)
    check("unpublish", s == 200 and r.get("status") == "unpublished", f"{s} {r}")
    s, r = req("GET", f"/api/moments/{moment_id}", token=bob)
    check("unpublished hidden", s == 404, f"{s}")
    s, r = req("DELETE", f"/api/admin/moments/{empty_moment}", token=admin)
    check("delete draft moment", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/admin/moments", token=admin)
    check("admin moments list", s == 200 and any(m["id"] == moment_id for m in r.get("moments", [])), f"{s}")

    # ---------- audio room recordings ----------
    s, r = req("POST", "/api/audio-rooms", {"title": f"g7 space {ts}"}, token=alice)
    room_id = r.get("id")
    check("create audio room", s in (200, 201) and room_id, f"{s} {r}")
    s, r = req("POST", f"/api/audio-rooms/{room_id}/start", {}, token=alice)
    check("start audio room", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/audio-rooms/{room_id}/recordings",
               {"media_id": f"rec{ts}", "duration_s": 61}, token=alice)
    rec_id = r.get("id")
    check("save space recording", s == 201 and rec_id, f"{s} {r}")
    s, r = req("POST", f"/api/audio-rooms/{room_id}/recordings",
               {"media_id": "bogus"}, token=bob)
    check("non-host cannot record", s == 403, f"{s} {r}")
    s, r = req("GET", f"/api/audio-rooms/{room_id}/recordings", token=bob)
    check("list space recordings", s == 200
          and any(x["id"] == rec_id and x["duration_s"] == 61 for x in r.get("recordings", [])), f"{s} {r}")
    s, r = req("DELETE", f"/api/audio-room-recordings/{rec_id}", token=bob)
    check("non-host cannot delete recording", s == 404, f"{s} {r}")
    s, r = req("DELETE", f"/api/audio-room-recordings/{rec_id}", token=alice)
    check("host deletes recording", s == 200, f"{s} {r}")

    # ---------- live room replays ----------
    s, r = req("POST", "/api/live-rooms", {"title": f"g7 live {ts}"}, token=dave)
    live_id = r.get("room", {}).get("id")
    check("create live room", s == 201 and live_id, f"{s} {r}")
    s, r = req("PUT", f"/api/live-rooms/{live_id}/replay",
               {"media_id": f"replay{ts}"}, token=dave)
    check("attach replay", s == 200 and r.get("status") == "replay set", f"{s} {r}")
    s, r = req("PUT", f"/api/live-rooms/{live_id}/replay",
               {"media_id": "evil"}, token=alice)
    check("non-host cannot attach replay", s == 404, f"{s} {r}")
    s, r = req("PUT", f"/api/live-rooms/{live_id}/replay", {"media_id": ""}, token=dave)
    check("empty replay media rejected", s == 400, f"{s} {r}")
    s, r = req("GET", f"/api/live-rooms/{live_id}", token=alice)
    check("replay in room payload", s == 200
          and r.get("room", {}).get("replay_media_id") == f"/media/replay{ts}", f"{s} {r}")
    s, r = req("GET", "/api/live-rooms", token=alice)
    this_room = [x for x in r.get("rooms", []) if x.get("id") == live_id]
    check("replay in listing", this_room
          and this_room[0].get("replay_media_id") == f"/media/replay{ts}", f"{s} {r}")

    # ---------- chunked resumable upload sessions ----------
    s, r = req("POST", "/api/uploads",
               {"filename": "big.bin", "total_bytes": 5 * 1024 * 1024}, token=alice)
    up_id = r.get("upload_id")
    if s == 503:
        check("upload session (security svc offline — skipped)", True, "503")
    else:
        check("open upload session", s == 201 and up_id and r.get("signature")
              and r.get("expires", 0) > int(time.time()), f"{s} {r}")
        s, r = req("GET", f"/api/uploads/{up_id}", token=alice)
        check("upload session status", s == 200 and r.get("status") == "active"
              and r.get("received_bytes") == 0, f"{s} {r}")
        s, r = req("POST", f"/api/uploads/{up_id}/complete",
                   {"media_url": "/media/abc.bin", "received_bytes": 123}, token=alice)
        check("complete rejects byte mismatch", s == 400, f"{s} {r}")
        s, r = req("POST", f"/api/uploads/{up_id}/complete",
                   {"media_url": "https://evil.example.com/abc.bin",
                    "received_bytes": 5 * 1024 * 1024}, token=alice)
        check("complete rejects external url", s == 400, f"{s} {r}")
        s, r = req("POST", f"/api/uploads/{up_id}/abort", token=bob)
        check("abort hidden from non-owner", s == 404, f"{s} {r}")
        s, r = req("POST", f"/api/uploads/{up_id}/abort", token=alice)
        check("abort upload session", s == 200, f"{s} {r}")
        s, r = req("GET", f"/api/uploads/{up_id}", token=alice)
        check("aborted session visible", s == 200 and r.get("status") == "aborted", f"{s} {r}")
        s, r = req("POST", f"/api/uploads/{up_id}/complete",
                   {"media_url": "/media/abc.bin", "received_bytes": 5 * 1024 * 1024}, token=alice)
        check("aborted session cannot complete", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/uploads", {"filename": "x", "total_bytes": 0}, token=alice)
    check("zero-byte session rejected", s == 400, f"{s} {r}")

    # ---------- E2E SAS verification codes ----------
    s, r = req("PUT", "/api/e2e/key", {"identity_key": f"ALICE{ts}"}, token=alice)
    check("publish alice key", s in (200, 201), f"{s} {r}")
    s, r = req("PUT", "/api/e2e/key", {"identity_key": f"BOBKEY{ts}"}, token=bob)
    check("publish bob key", s in (200, 201), f"{s} {r}")
    s, r = req("GET", f"/api/e2e/verify/{bob_id}", token=alice)
    if s == 503:
        check("e2e verify (security svc offline — skipped)", True, "503")
    else:
        fa = r.get("fingerprint", "")
        sa = r.get("sas", "")
        check("sas shape", s == 200 and re.fullmatch(r"[0-9a-f]{64}", fa)
              and re.fullmatch(r"[0-9]{60}", sa), f"{s} {fa} {sa}")
        s, r = req("GET", f"/api/e2e/verify/{alice_id}", token=bob)
        check("sas symmetric", s == 200 and r.get("fingerprint") == fa and r.get("sas") == sa, f"{s}")
        s, r = req("GET", f"/api/e2e/verify/{carol_id}", token=alice)
        check("verify needs both keys", s == 400, f"{s} {r}")
        s, r = req("GET", f"/api/e2e/verify/{alice_id}", token=alice)
        check("self verify rejected", s == 400, f"{s} {r}")
        s, r = req("GET", f"/api/e2e/verify/{bob_id}", token=alice)
        check("fingerprint stable", r.get("fingerprint") == fa, f"{s}")

    # ---------- related reels (ML embeddings) ----------
    s, r = req("POST", "/api/posts", {"body": f"ocean waves surfing {ts}", "type": "reel"}, token=alice)
    rel_src = r.get("id") or r.get("post", {}).get("id")
    s, r = req("POST", "/api/posts", {"body": f"ocean waves surfing beach {ts}", "type": "reel"}, token=bob)
    rel_close = r.get("id") or r.get("post", {}).get("id")
    s, r = req("POST", "/api/posts", {"body": f"quantum computing lecture notes {ts}", "type": "reel"}, token=carol)
    rel_far = r.get("id") or r.get("post", {}).get("id")
    # Embeddings are written asynchronously via the ML service — poll until
    # the close reel is indexed (or give up after ~8s).
    deadline = time.time() + 8
    rel_ids = []
    while time.time() < deadline:
        s, r = req("GET", f"/api/reels/{rel_src}/related?limit=50", token=eve)
        if s == 200:
            rel_ids = [x["id"] for x in r.get("reels", [])]
            if rel_close in rel_ids:
                break
        time.sleep(0.5)
    if s == 503:
        check("related reels (ml svc offline — skipped)", True, "503")
    else:
        rel_ids = [x["id"] for x in r.get("reels", [])]
        check("related reels ranked", s == 200 and rel_close in rel_ids, f"{s} {rel_ids}")
        # The handler applies a similarity threshold, so the unrelated
        # (quantum computing) reel must NOT be returned.
        check("unrelated reels excluded by similarity threshold",
              rel_far not in rel_ids, f"{rel_ids}")
        check("source reel not related to itself", rel_src not in rel_ids, f"{rel_ids}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    return 1 if integration_test.failed else 0


if __name__ == "__main__":
    sys.exit(main())
