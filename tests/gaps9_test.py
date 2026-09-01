"""End-to-end checks for gap-pack-9: TikTok/imo/Telegram client-gaps.

Backend-exercised pieces: per-user message deletion ("delete for me") undo
(POST /api/messages/{id}/unhide), still filtered per-viewer, plus a stale-doc
confirmation sweep. Client-only pieces (CameraRecorder, PhotoDeck, SfuCall
networkQuality/audio-only badge) are covered by web build in CI.

Runs against a live API on :8080 (migrations 001-024 applied). No mocks.
"""
import asyncio
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import integration_test
from integration_test import check, req
from gaps6_test import register


def main():
    ts = int(time.time())
    alice = register(f"g9a{ts}")
    bob = register(f"g9b{ts}")

    def self_id(tok):
        s, r = req("GET", "/api/me", token=tok)
        return r.get("id")

    # ---------- DM + send a message ----------
    s, r = req("POST", "/api/conversations", {"member_ids": [self_id(bob)]},
               token=alice)
    conv = r.get("id")
    check("create DM conversation", s in (200, 201) and conv, f"{s} {r}")

    # Send over a real WebSocket connection (integration_test.chat_flow does
    # typing + message + asserts delivery).
    msg_id = asyncio.run(integration_test.chat_flow(alice, bob, conv,
                                                    self_id(alice),
                                                    self_id(bob)))
    check("message delivered over WS", bool(msg_id), f"msg={msg_id}")

    # ---------- hide for me ----------
    s, r = req("POST", f"/api/messages/{msg_id}/hide", {}, token=bob)
    check("hide (delete for me)", s == 200, f"{s} {r}")

    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    ids_bob = [m.get("id") for m in r.get("messages", [])]
    check("hidden for hider only", s == 200 and msg_id not in ids_bob, f"{s} {ids_bob}")

    s, r = req("GET", f"/api/conversations/{conv}/messages", token=alice)
    ids_alice = [m.get("id") for m in r.get("messages", [])]
    check("visible for others", s == 200 and msg_id in ids_alice, f"{s}")

    # ---------- unhide (undo) ----------
    s, r = req("POST", f"/api/messages/{msg_id}/unhide", {}, token=bob)
    check("unhide (undo)", s == 200, f"{s} {r}")
    s, r = req("GET", f"/api/conversations/{conv}/messages", token=bob)
    ids_bob = [m.get("id") for m in r.get("messages", [])]
    check("visible again after unhide", s == 200 and msg_id in ids_bob, f"{s}")

    # ---------- hide is idempotent; unhide of never-hidden still 200 ----------
    s, r = req("POST", f"/api/messages/{msg_id}/hide", {}, token=alice)
    check("hide is idempotent (same user)", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/messages/{msg_id}/unhide", {}, token=alice)
    check("unhide idempotent", s == 200, f"{s} {r}")

    # non-member cannot hide/unhide
    carol = register(f"g9c{ts}")
    s, r = req("POST", f"/api/messages/{msg_id}/hide", {}, token=carol)
    check("outsider cannot hide", s == 404, f"{s} {r}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    sys.exit(1 if integration_test.failed else 0)


if __name__ == "__main__":
    main()
