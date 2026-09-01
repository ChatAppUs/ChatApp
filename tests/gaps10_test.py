"""End-to-end checks for gap-pack-10 (2026-09-01): Telegram Mini Apps launcher
public endpoint (GET /api/miniapps) FYP author-affinity boost regression guard,
plus bot mini-app flex through the existing handleSetMiniApp endpoint.

Runs against a live API on :8080 (migrations up to 024 applied; Mini Apps re-use
the mini_apps table from 013_bots.sql — no new migration this pack).
"""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req
from gaps6_test import register


def main():
    ts = int(time.time())
    alice = register(f"g10a{ts}")
    bob = register(f"g10b{ts}")

    # ---------- create a bot and set its mini app ----------
    s, r = req("POST", "/api/bots", {"username": f"g{ts}bot", "description": "gap10"}, token=alice)
    check("create bot", s == 201 and r.get("id"), f"{s}")
    bot_id = r.get("id")
    token = r.get("token")

    s, r = req("POST", f"/api/bots/{bot_id}/mini-app",
               {"title": "Shop", "url": "https://example.com/app", "icon_url": "https://example.com/icon.png"},
               token=alice)
    check("owner set mini app", s == 200, f"{s} {r}")

    s, r = req("POST", f"/api/bots/{bot_id}/mini-app",
               {"title": "Bad", "url": "http://insecure.example.com", "icon_url": ""},
               token=alice)
    check("reject non-https url", s == 400, f"{s} {r}")

    s, r = req("POST", f"/api/bots/{bot_id}/mini-app",
               {"title": "Shop", "url": "https://example.com/v2", "icon_url": "https://example.com/icon2.png"},
               token=alice)
    check("idempotent upsert on (bot,title)", s == 200, f"{s} {r}")

    # ---------- public directory ----------
    s, r = req("GET", "/api/miniapps", token=bob)
    check("public miniapps list", s == 200 and any(a.get("url") == "https://example.com/v2"
                                                    for a in r.get("apps", [])), f"{s} {r.get('apps')!r}")

    # ---------- FYP regression (query must still parse + return a feed) ----------
    s, r = req("GET", "/api/fyp?limit=10", token=alice)
    check("fyp 200 after affinity boost", s == 200 and "posts" in r, f"{s} {r}")


if __name__ == "__main__":
    main()
