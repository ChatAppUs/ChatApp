"""End-to-end checks for gap-pack-8: TikTok-style duet/stitch reel remixes
(migration 024 — posts.remix_mode).

Runs against a live API on :8080 with migrations 001-024 applied. No mocks.
"""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import integration_test
from integration_test import check, req
from gaps6_test import register


def main():
    ts = int(time.time())
    alice = register(f"g8d{ts}")
    bob = register(f"g8eR{ts}")

    # ---------- source reel ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": f"original reel {ts}",
                "media": [{"kind": "video", "url": "https://cdn.example.com/orig.mp4"}]},
               token=alice)
    reel_id = r.get("id")
    check("create source reel", s == 201 and reel_id, f"{s} {r}")

    # ---------- duet ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "my duet", "remix_of": reel_id, "remix_mode": "duet",
                "media": [{"kind": "video", "url": "https://cdn.example.com/duet.mp4"}]},
               token=bob)
    duet_id = r.get("id")
    check("create duet remix", s == 201 and duet_id, f"{s} {r}")

    s, r = req("GET", f"/api/posts/{duet_id}", token=alice)
    post = r.get("post") or {}
    check("duet serialized with mode + source",
          s == 200 and post.get("remix_mode") == "duet" and post.get("remix_of") == reel_id,
          f"{s} {post.get('remix_mode')} {post.get('remix_of')}")

    # ---------- stitch ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "my stitch", "remix_of": reel_id, "remix_mode": "stitch",
                "media": [{"kind": "video", "url": "https://cdn.example.com/stitch.mp4"}]},
               token=bob)
    stitch_id = r.get("id")
    check("create stitch remix", s == 201 and stitch_id, f"{s} {r}")

    s, r = req("GET", f"/api/posts/{stitch_id}", token=alice)
    post = r.get("post") or {}
    check("stitch serialized with mode",
          s == 200 and post.get("remix_mode") == "stitch" and post.get("remix_of") == reel_id,
          f"{s} {post.get('remix_mode')}")

    # ---------- plain remix stays mode-less ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "plain remix", "remix_of": reel_id,
                "media": [{"kind": "video", "url": "https://cdn.example.com/plain.mp4"}]},
               token=bob)
    plain_id = r.get("id")
    check("plain remix still works", s == 201 and plain_id, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{plain_id}", token=alice)
    post = r.get("post") or {}
    check("plain remix has no remix_mode",
          s == 200 and not post.get("remix_mode") and post.get("remix_of") == reel_id,
          f"{s} {post.get('remix_mode')}")

    # ---------- validation ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "x", "remix_mode": "duet"}, token=bob)
    check("remix_mode requires remix_of", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "x", "remix_of": reel_id, "remix_mode": "sidequest"},
               token=bob)
    check("invalid remix_mode rejected", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/posts",
               {"type": "post", "body": "x", "remix_of": reel_id, "remix_mode": "duet"},
               token=bob)
    check("duet of a non-reel rejected", s == 400, f"{s} {r}")

    # ---------- remixes list carries the layout ----------
    s, r = req("GET", f"/api/reels/{reel_id}/remixes", token=alice)
    rows = {m["id"]: m for m in r.get("remixes", [])}
    check("remixes list includes all three",
          s == 200 and duet_id in rows and stitch_id in rows and plain_id in rows, f"{s} {r}")
    check("remixes list carries remix_mode",
          rows.get(duet_id, {}).get("remix_mode") == "duet"
          and rows.get(stitch_id, {}).get("remix_mode") == "stitch"
          and not rows.get(plain_id, {}).get("remix_mode"),
          f"{rows.get(duet_id)} {rows.get(plain_id)}")

    # ---------- reels feed serializes the new fields ----------
    s, r = req("GET", "/api/reels", token=alice)
    feed = {p["id"]: p for p in r.get("reels", [])}
    check("reels feed carries remix fields",
          s == 200 and feed.get(duet_id, {}).get("remix_mode") == "duet"
          and feed.get(duet_id, {}).get("remix_of") == reel_id
          and feed.get(stitch_id, {}).get("remix_mode") == "stitch",
          f"{s} {feed.get(duet_id)}")

    # ---------- reel analytics counts all remix variants ----------
    s, r = req("GET", f"/api/reels/{reel_id}/analytics", token=alice)
    check("analytics counts duet+stitch+plain remixes",
          s == 200 and r.get("remixes") == 3, f"{s} {r}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    # ---------- remote remix_mode flow ----------
    ts = int(time.time())
    alice = register(f"g8dR{ts}")
    bob = register(f"g8eR{ts}")

    # ---------- source reel ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": f"original reel {ts}",
                "media": [{"kind": "video", "url": "https://cdn.example.com/orig.mp4"}]},
               token=alice)
    reel_id = r.get("id")
    check("create source reel", s == 201 and reel_id, f"{s} {r}")

    # ---------- duet ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "my duet", "remix_of": reel_id, "remix_mode": "duet",
                "media": [{"kind": "video", "url": "https://cdn.example.com/duet.mp4"}]},
               token=bob)
    duet_id = r.get("id")
    check("create duet remix", s == 201 and duet_id, f"{s} {r}")

    s, r = req("GET", f"/api/posts/{duet_id}", token=alice)
    post = r.get("post") or {}
    check("duet serialized with mode + source",
          s == 200 and post.get("remix_mode") == "duet" and post.get("remix_of") == reel_id,
          f"{s} {post.get('remix_mode')} {post.get('remix_of')}")

    # ---------- stitch ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "my stitch", "remix_of": reel_id, "remix_mode": "stitch",
                "media": [{"kind": "video", "url": "https://cdn.example.com/stitch.mp4"}]},
               token=bob)
    stitch_id = r.get("id")
    check("create stitch remix", s == 201 and stitch_id, f"{s} {r}")

    s, r = req("GET", f"/api/posts/{stitch_id}", token=alice)
    post = r.get("post") or {}
    check("stitch serialized with mode",
          s == 200 and post.get("remix_mode") == "stitch" and post.get("remix_of") == reel_id,
          f"{s} {post.get('remix_mode')}")

    # ---------- plain remix stays mode-less ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "plain remix", "remix_of": reel_id,
                "media": [{"kind": "video", "url": "https://cdn.example.com/plain.mp4"}]},
               token=bob)
    plain_id = r.get("id")
    check("plain remix still works", s == 201 and plain_id, f"{s} {r}")
    s, r = req("GET", f"/api/posts/{plain_id}", token=alice)
    post = r.get("post") or {}
    check("plain remix has no remix_mode",
          s == 200 and not post.get("remix_mode") and post.get("remix_of") == reel_id,
          f"{s} {post.get('remix_mode')}")

    # ---------- validation ----------
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "x", "remix_mode": "duet"}, token=bob)
    check("remix_mode requires remix_of", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/posts",
               {"type": "reel", "body": "x", "remix_of": reel_id, "remix_mode": "sidequest"},
               token=bob)
    check("invalid remix_mode rejected", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/posts",
               {"type": "post", "body": "x", "remix_of": reel_id, "remix_mode": "duet"},
               token=bob)
    check("duet of a non-reel rejected", s == 400, f"{s} {r}")

    # ---------- remixes list carries the layout ----------
    s, r = req("GET", f"/api/reels/{reel_id}/remixes", token=alice)
    rows = {m["id"]: m for m in r.get("remixes", [])}
    check("remixes list includes all three",
          s == 200 and duet_id in rows and stitch_id in rows and plain_id in rows, f"{s} {r}")
    check("remixes list carries remix_mode",
          rows.get(duet_id, {}).get("remix_mode") == "duet"
          and rows.get(stitch_id, {}).get("remix_mode") == "stitch"
          and not rows.get(plain_id, {}).get("remix_mode"),
          f"{rows.get(duet_id)} {rows.get(plain_id)}")

    # ---------- reels feed serializes the new fields ----------
    s, r = req("GET", "/api/reels", token=alice)
    feed = {p["id"]: p for p in r.get("reels", [])}
    check("reels feed carries remix fields",
          s == 200 and feed.get(duet_id, {}).get("remix_mode") == "duet"
          and feed.get(duet_id, {}).get("remix_of") == reel_id
          and feed.get(stitch_id, {}).get("remix_mode") == "stitch",
          f"{s} {feed.get(duet_id)}")

    # ---------- reel analytics counts all remix variants ----------
    s, r = req("GET", f"/api/reels/{reel_id}/analytics", token=alice)
    check("analytics counts duet+stitch+plain remixes",
          s == 200 and r.get("remixes") == 3, f"{s} {r}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    import sys as _s
    _s.exit(1 if integration_test.failed else 0)


if __name__ == "__main__":
    main()
