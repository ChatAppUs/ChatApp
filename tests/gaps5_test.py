#!/usr/bin/env python3
"""End-to-end checks for gap-pack-5: persistent drop-in call rooms, the own
KYC auto-verification pipeline (ML scoring + auto-verify/manual-review
routing), and true ad revenue-share accounting (treasury double-entry +
creator payout on attributed impressions).

Runs against a live API on :8080 with migration 020 applied and the ML
service on :8200. No mocks.
"""
import http.server
import io
import os
import sys
import threading
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req, grant_superadmin
from finance_test import db, fund
from gaps2_test import register

TREASURY = "00000000-0000-0000-0000-000000000000"


def make_image(w, h, seed):
    """A textured, sharp PNG (passes the ML doc-quality gates)."""
    from PIL import Image

    img = Image.new("L", (w, h))
    px = img.load()
    state = seed
    for y in range(h):
        for x in range(w):
            state = (state * 1103515245 + 12345) & 0x7FFFFFFF
            px[x, y] = (state >> 16) % 256
    buf = io.BytesIO()
    img.save(buf, "PNG")
    return buf.getvalue()


def serve_files(files, port):
    """Serve {path: bytes} over HTTP on 127.0.0.1 so the ML service can
    fetch document/selfie images exactly as it would in production."""
    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            data = files.get(self.path)
            if data is None:
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header("Content-Type", "image/png")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *a):
            pass

    srv = http.server.HTTPServer(("127.0.0.1", port), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def main():
    ts = int(time.time())
    alice = register(f"g5a{ts}")
    bob = register(f"g5b{ts}")
    carol = register(f"g5c{ts}")

    s, r = req("GET", "/api/me", token=alice)
    alice_id = r.get("id") or r.get("user", {}).get("id")
    s, r = req("GET", "/api/me", token=bob)
    bob_id = r.get("id") or r.get("user", {}).get("id")
    check("user ids", bool(alice_id and bob_id), f"{alice_id} {bob_id}")

    # ============ Drop-in rooms (Messenger Rooms parity) ============
    s, r = req("POST", "/api/rooms", {"title": "Standup"}, token=alice)
    slug = r.get("slug")
    check("create drop-in room", s == 201 and slug and r.get("link") == f"/room/{slug}", f"{s} {r}")

    s, r = req("GET", f"/api/rooms/{slug}", token=bob)
    check("room preview", s == 200 and r.get("title") == "Standup" and r.get("ended") is False, f"{s} {r}")

    s, r = req("POST", f"/api/rooms/{slug}/join", {}, token=bob)
    check("non-host joins via link", s == 200 and r.get("ticket") and r.get("room_id") == f"dropin-{slug}"
          and any("stun:" in str(u.get("urls")) for u in r.get("ice_servers", [])), f"{s} {r}")

    s, r = req("POST", f"/api/rooms/{slug}/join", {}, token=carol)
    check("third party joins via link", s == 200 and r.get("ticket"), f"{s} {r}")

    s, r = req("POST", "/api/rooms/NO_SUCH_SLUG/join", {}, token=bob)
    check("unknown slug 404", s == 404, f"{s} {r}")

    s, r = req("GET", "/api/me/rooms", token=alice)
    check("host lists own rooms", s == 200 and any(x.get("slug") == slug for x in r.get("rooms", [])), f"{s} {r}")
    s, r = req("GET", "/api/me/rooms", token=bob)
    check("non-host list empty", s == 200 and not r.get("rooms"), f"{s} {r}")

    s, r = req("POST", f"/api/rooms/{slug}/end", {}, token=bob)
    check("non-host cannot end", s == 403, f"{s} {r}")
    s, r = req("POST", f"/api/rooms/{slug}/end", {}, token=alice)
    check("host ends room", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/rooms/{slug}/join", {}, token=bob)
    check("join ended room 410", s == 410, f"{s} {r}")
    s, r = req("GET", f"/api/rooms/{slug}", token=bob)
    check("preview shows ended", s == 200 and r.get("ended") is True, f"{s} {r}")

    # ============ Own KYC auto-verification pipeline ============
    port = 8791
    files = {
        "/doc.png": make_image(640, 400, ts),
        "/selfie.png": make_image(480, 480, ts + 7),
    }
    serve_files(files, port)
    base = f"http://127.0.0.1:{port}"

    # High-quality submission: ML score clears the threshold -> auto-verified.
    s, r = req("POST", "/api/kyc/submit", {
        "full_name": "Alice Gapfive", "country": "US", "doc_type": "passport",
        "doc_number": "X12345678", "doc_image_url": f"{base}/doc.png",
        "selfie_url": f"{base}/selfie.png"}, token=alice)
    check("kyc submit with documents", s == 201, f"{s} {r}")
    check("kyc auto-verified by ML pipeline", r.get("status") == "verified"
          and r.get("auto_score", 0) >= 0.75, f"{s} {r}")
    s, r = req("GET", "/api/me", token=alice)
    me = r.get("user", r)
    check("kyc_status flipped to verified", me.get("kyc_status") == "verified", f"{s} {me.get('kyc_status')}")

    # Low-information submission: stays pending for a human reviewer.
    s, r = req("POST", "/api/kyc/submit", {
        "full_name": "Bob Gapfive", "country": "US", "doc_type": "passport",
        "doc_number": "!!!"}, token=bob)
    check("kyc submit without documents", s == 201, f"{s} {r}")
    check("kyc stays pending below threshold", r.get("status") == "pending", f"{s} {r}")
    sub_id = r.get("id")

    # Admin review path still works on auto-pending submissions.
    admin_name = f"g5admin{ts}"
    register(admin_name)
    grant_superadmin(admin_name)
    s, r = req("POST", "/api/admin/login",
               {"identifier": f"{admin_name}@test.dev", "password": "Passw0rd!123"})
    check("admin login (superadmin)", s == 200 and r.get("access_token"), f"{s} {r}")
    admin_tok = r.get("access_token")
    s, r = req("GET", "/api/admin/kyc", token=admin_tok)
    check("admin sees pending submission", s == 200
          and any(x.get("id") == sub_id for x in r.get("submissions", [])), f"{s} {r}")
    s, r = req("POST", f"/api/admin/kyc/{sub_id}/review", {"decision": "verified"}, token=admin_tok)
    check("admin approves pending kyc", s == 200, f"{s} {r}")
    s, r = req("GET", "/api/me", token=bob)
    me = r.get("user", r)
    check("bob verified after review", me.get("kyc_status") == "verified", f"{s} {me.get('kyc_status')}")

    # ============ True ad revenue-share accounting ============
    # Creator publishes a reel (the ad placement surface).
    s, r = req("POST", "/api/posts", {"body": "g5 reel", "type": "reel", "visibility": "public"}, token=carol)
    post_id = r.get("id")
    check("creator publishes reel", s == 201 and post_id, f"{s} {r}")

    # Advertiser: campaign -> creative -> submit -> admin approve -> fund.
    s, r = req("POST", "/api/ads/campaigns",
               {"name": "g5 camp", "daily_budget": "10", "total_budget": "10",
                "target_countries": ["US"]}, token=alice)
    camp_id = r.get("id")
    check("create campaign", s == 201 and camp_id, f"{s} {r}")
    s, r = req("POST", f"/api/ads/campaigns/{camp_id}/creatives",
               {"title": "g5 ad", "body": "buy", "cta_url": "https://example.com"}, token=alice)
    check("add creative", s == 201, f"{s} {r}")
    s, r = req("POST", f"/api/ads/campaigns/{camp_id}/submit", {}, token=alice)
    check("submit campaign", s == 200, f"{s} {r}")
    s, r = req("POST", f"/api/admin/ads/{camp_id}/review", {"decision": "active"}, token=admin_tok)
    check("admin activates campaign", s == 200, f"{s} {r}")

    fund(f"g5a{ts}", "USD", "internal", "10")
    s, r = req("POST", f"/api/ads/campaigns/{camp_id}/fund", {"amount": "5"}, token=alice)
    check("fund campaign", s == 200, f"{s} {r}")

    def q1(sql):
        import subprocess
        dburl = os.environ.get(
            "DATABASE_URL", "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")
        out = subprocess.run(["psql", dburl, "-tA", "-c", sql], check=True, capture_output=True, text=True)
        return out.stdout.strip()

    treasury_credit = q1("SELECT COALESCE(SUM(amount),0) FROM ledger_entries le "
                         "JOIN wallet_accounts wa ON wa.id=le.account_id "
                         f"WHERE wa.user_id='{TREASURY}' AND le.kind='ad_credit' "
                         f"AND le.memo='campaign {camp_id}'")
    check("treasury credited on funding (double-entry)", float(treasury_credit or 0) == 5.0, treasury_credit)

    # Un-attributed serve: impression + spend, no creator share.
    s, r = req("GET", "/api/ads/serve?country=US", token=bob)
    check("serve ad unattributed", s == 200 and (r.get("ad") or {}).get("creative_id"), f"{s} {r}")
    shares = q1(f"SELECT COUNT(*) FROM creator_earnings WHERE source='ad_share' AND post_id='{post_id}'")
    check("no share without placement", shares == "0", shares)

    # Attributed serve: creator earns 55% of the impression cost from treasury.
    s, r = req("GET", f"/api/ads/serve?country=US&placement_post_id={post_id}", token=bob)
    check("serve ad with placement", s == 200 and (r.get("ad") or {}).get("creative_id"), f"{s} {r}")
    earn = q1(f"SELECT COALESCE(SUM(amount),0) FROM creator_earnings "
              f"WHERE source='ad_share' AND post_id='{post_id}'")
    check("creator ad-share earning recorded", float(earn or 0) > 0, earn)
    recv = q1("SELECT COALESCE(SUM(amount),0) FROM ledger_entries le "
              "JOIN wallet_accounts wa ON wa.id=le.account_id "
              f"JOIN users u ON u.id=wa.user_id WHERE u.username='g5c{ts}' AND le.kind='ad_share_recv'"
              f" AND le.memo='placement {post_id}'")
    check("creator wallet credited", float(recv or 0) > 0, recv)
    sent = q1("SELECT COALESCE(SUM(amount),0) FROM ledger_entries le "
              "JOIN wallet_accounts wa ON wa.id=le.account_id "
              f"WHERE wa.user_id='{TREASURY}' AND le.kind='ad_share_send'"
              f" AND le.memo='placement {post_id}'")
    check("treasury debited same amount", sent and abs(float(sent)) == float(recv), f"{sent} vs {recv}")

    # ============ FYP cache behavior (no Redis configured: still correct) ============
    s1, r1 = req("GET", "/api/fyp", token=bob)
    s2, r2 = req("GET", "/api/fyp", token=bob)
    check("fyp consistent without redis", s1 == 200 and s2 == 200
          and r1.get("posts") == r2.get("posts"), f"{s1} {s2}")

    print("\nDONE")


if __name__ == "__main__":
    main()
