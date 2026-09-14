#!/usr/bin/env python3
"""Feature flags, controlled experiments, and the QoE / call-quality
telemetry plane.

Master plan §74 (feature flags with percentage/region/platform rollouts),
§75 (controlled experiments with per-variant outcome metrics: satisfaction
proxy via reports, retention via completion, watch time), §72 (video QoE:
startup, buffering, failures, resolution/bitrate/completion) and §73 (call
quality: packet loss, jitter, RTT, bitrate, frame rate, resolution).

Covers the stable-bucket variant assignment contract: the same user always
lands in the same variant for a given flag.
"""
import json
import os
import subprocess
import time
import urllib.request
import urllib.error

BASE = os.environ.get("CHATAPP_BASE_URL", "http://localhost:8080")
DBURL = os.environ.get("DATABASE_URL",
                       "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable")

import integration_test
from integration_test import check, req, register_verified, grant_superadmin


def db(sql):
    subprocess.run(["psql", DBURL, "-c", sql], check=True,
                   capture_output=True)


def admin_token(username, email):
    grant_superadmin(username)
    s, r = req("POST", "/api/admin/login",
               {"identifier": email, "password": "Passw0rd!123"})
    check("admin login (superadmin)", s == 200 and r.get("access_token"), f"{s} {r}")
    return r.get("access_token")


def main():
    ts = int(time.time())
    alice = f"flagA{ts}"
    bob = f"flagB{ts}"
    carol = f"flagC{ts}"

    s, r = register_verified(alice)
    check("register alice", s in (200, 201) and r.get("access_token"), f"{s} {r}")
    alice_tok = r.get("access_token")
    s, r = register_verified(bob)
    check("register bob", s in (200, 201) and r.get("access_token"), f"{s} {r}")
    bob_tok = r.get("access_token")
    s, r = register_verified(carol)
    check("register carol", s in (200, 201) and r.get("access_token"), f"{s} {r}")
    carol_tok = r.get("access_token")

    admin_tok = admin_token(alice, f"{alice}@test.dev")

    # The suite is re-runnable: upsert semantics mean earlier runs may have
    # left flags/experiments behind, so the user plane is only asserted to be
    # reachable and well-formed here (the empty case is covered on a fresh DB).
    s, r = req("GET", "/api/me/flags", token=alice_tok)
    check("my flags list", s == 200 and isinstance(r.get("flags"), list), f"{s} {r}")
    s, r = req("GET", "/api/me/experiments", token=alice_tok)
    check("my experiments list", s == 200 and isinstance(r.get("experiments"), list), f"{s} {r}")

    # --- admin plane: create a 100% flag ---
    s, r = req("POST", "/api/admin/flags",
               {"key": "new_fyp", "description": "FYP v2 ranker",
                "rollout_pct": 100}, admin_tok)
    check("admin create flag 100%", s in (200, 201), f"{s} {r}")

    s, r = req("GET", "/api/me/flags", token=alice_tok)
    fyp = next((f for f in r.get("flags", []) if f.get("key") == "new_fyp"), None)
    check("flag evaluated on for alice", fyp is not None and fyp.get("on") is True, f"{s} {r}")

    # --- partial rollout: deterministic buckets ---
    s, r = req("POST", "/api/admin/flags",
               {"key": "video_editor_v2", "description": "editor v2",
                "rollout_pct": 50}, admin_tok)
    check("admin create flag 50%", s in (200, 201), f"{s} {r}")

    variants = set()
    for tok in (alice_tok, bob_tok, carol_tok):
        s, r = req("GET", "/api/me/flags", token=tok)
        f = next((f for f in r.get("flags", []) if f.get("key") == "video_editor_v2"), {})
        variants.add(f.get("on"))
    check("50% rollout evaluated for all users", None not in variants and len(variants) >= 1,
          f"variants={variants}")

    # --- region + platform gating ---
    s, r = req("POST", "/api/admin/flags",
               {"key": "ai_dubbing", "description": "dubbing beta",
                "rollout_pct": 100, "regions": ["BD"], "platforms": ["web"]}, admin_tok)
    check("admin create gated flag", s in (200, 201), f"{s} {r}")

    s, r = req("GET", "/api/me/flags?region=us&platform=web", token=alice_tok)
    f = next((f for f in r.get("flags", []) if f.get("key") == "ai_dubbing"), {})
    check("region gate blocks us", f.get("on") is False, f"{s} {r}")

    s, r = req("GET", "/api/me/flags?region=bd&platform=web", token=alice_tok)
    f = next((f for f in r.get("flags", []) if f.get("key") == "ai_dubbing"), {})
    check("region+platform gate passes bd/web", f.get("on") is True, f"{s} {r}")

    s, r = req("GET", "/api/me/flags?region=bd&platform=android", token=alice_tok)
    f = next((f for f in r.get("flags", []) if f.get("key") == "ai_dubbing"), {})
    check("platform gate blocks android", f.get("on") is False, f"{s} {r}")

    # --- disabled flag is off for everyone ---
    s, r = req("PUT", "/api/admin/flags/new_fyp", {"enabled": False}, admin_tok)
    check("admin disable flag", s in (200, 201), f"{s} {r}")
    s, r = req("GET", "/api/me/flags", token=alice_tok)
    f = next((f for f in r.get("flags", []) if f.get("key") == "new_fyp"), {})
    check("disabled flag off", f.get("on") is False, f"{s} {r}")
    s, r = req("PUT", "/api/admin/flags/new_fyp", {"enabled": True}, admin_tok)
    check("admin re-enable flag", s in (200, 201), f"{s} {r}")

    # --- admin list + delete ---
    s, r = req("GET", "/api/admin/flags", token=admin_tok)
    check("admin list flags", s == 200 and len(r.get("flags", [])) >= 3, f"{s} {r}")

    s, r = req("POST", "/api/admin/flags",
               {"key": "temp_flag", "description": "temp"}, admin_tok)
    check("admin create temp flag", s in (200, 201), f"{s} {r}")
    s, r = req("DELETE", "/api/admin/flags/temp_flag", token=admin_tok)
    check("admin delete flag", s in (200, 204), f"{s} {r}")
    s, r = req("GET", "/api/admin/flags", token=admin_tok)
    check("deleted flag gone",
          all(f.get("key") != "temp_flag" for f in r.get("flags", [])), f"{s} {r}")

    # --- validation: rollout bounds ---
    s, r = req("POST", "/api/admin/flags",
               {"key": "bad_flag", "rollout_pct": 101}, admin_tok)
    check("rollout_pct > 100 rejected", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/admin/flags", {"key": ""}, admin_tok)
    check("empty key rejected", s == 400, f"{s} {r}")

    # --- experiments ---
    s, r = req("POST", "/api/admin/experiments",
               {"key": "fyp_rank_v2", "flag_key": "new_fyp",
                "description": "FYP ranking experiment"}, admin_tok)
    check("admin create experiment", s in (200, 201), f"{s} {r}")

    # Drop the flag to a 50%% rollout so the results query has both buckets.
    s, r = req("PUT", "/api/admin/flags/new_fyp", {"rollout_pct": 50}, admin_tok)
    check("admin set experiment flag to 50%%", s == 200, f"{s} {r}")

    s, r = req("GET", "/api/me/experiments", token=alice_tok)
    e = next((e for e in r.get("experiments", []) if e.get("key") == "fyp_rank_v2"), {})
    check("experiment variant resolved for alice",
          e.get("variant") in ("on", "off"), f"{s} {r}")

    # variant assignment must be stable across calls
    s2, r2 = req("GET", "/api/me/experiments", token=alice_tok)
    e2 = next((e for e in r2.get("experiments", []) if e.get("key") == "fyp_rank_v2"), {})
    check("variant stable across reads", e.get("variant") == e2.get("variant"),
          f"{e} vs {e2}")

    s, r = req("GET", "/api/admin/experiments", token=admin_tok)
    check("admin list experiments", s == 200 and len(r.get("experiments", [])) >= 1, f"{s} {r}")

    # --- experiment results (§75 metrics) ---
    # seed watch events for alice so the "on" variant has outcome data
    alice_id = subprocess.run(
        ["psql", DBURL, "-tAc", f"SELECT id FROM users WHERE username='{alice}'"],
        capture_output=True, text=True).stdout.strip()
    post_id = subprocess.run(
        ["psql", DBURL, "-tAc",
         "INSERT INTO posts (author_id, type, body, visibility) "
         f"VALUES ('{alice_id}', 'reel', 'exp reel', 'public') RETURNING id"],
        capture_output=True, text=True).stdout.strip().split()[0]
    db(f"INSERT INTO reel_watch_events (user_id, post_id, watched_ms, duration_ms, completed) "
       f"VALUES ('{alice_id}', '{post_id}', 30000, 30000, true)")
    db(f"INSERT INTO reports (reporter_id, target_type, target_id, reason) "
       f"VALUES ('{alice_id}', 'post', '{post_id}', 'experiment metric probe')")

    s, r = req("GET", "/api/admin/experiments/fyp_rank_v2/results", token=admin_tok)
    check("experiment results returned", s == 200 and r.get("variants"), f"{s} {r}")
    variants = r.get("variants", [])
    check("results have on+off buckets (50%% rollout splits the population)",
          any(v.get("variant") == "on" for v in variants)
          and any(v.get("variant") == "off" for v in variants), f"{variants}")
    on = next((v for v in variants if v.get("variant") == "on"), {})
    check("on-bucket outcome fields present",
          "users" in on and "completion_rate" in on and "avg_watch_ms" in on
          and "reports" in on, f"{on}")

    # --- QoE telemetry (§72) ---
    s, r = req("POST", "/api/telemetry/qoe",
               {"video_id": post_id, "startup_ms": 850, "buffer_ms": 240,
                "buffering_events": 2, "playback_failed": False,
                "resolution": "1080p", "bitrate_kbps": 4500,
                "completed": True, "watch_ms": 30000}, alice_tok)
    check("qoe ingest accepted (202 Accepted)", s in (200, 201, 202), f"{s} {r}")

    s, r = req("POST", "/api/telemetry/qoe", {"video_id": post_id}, token=None)
    check("qoe ingest requires auth", s == 401, f"{s} {r}")

    s, r = req("GET", "/api/admin/qoe/summary", token=admin_tok)
    rows = r.get("rows", [])
    check("qoe summary returned", s == 200 and len(rows) >= 1, f"{s} {r}")
    if rows:
        b = rows[0]
        check("qoe percentiles present",
              "p50_startup_ms" in b and "p95_startup_ms" in b
              and "p50_buffer_ms" in b, f"{b}")
        check("qoe failure+completion rates present",
              "failure_pct" in b and "completion_pct" in b, f"{b}")

    s, r = req("GET", "/api/admin/qoe/summary", token=alice_tok)
    check("qoe summary admin-only", s == 401 or s == 403, f"{s} {r}")

    # --- call quality telemetry (§73) ---
    s, r = req("POST", "/api/telemetry/call-quality",
               {"room_id": "room-1", "packet_loss_pct": 1.2, "jitter_ms": 18.5,
                "rtt_ms": 96, "bitrate_kbps": 2400, "frame_rate": 30,
                "resolution": "1280x720"}, alice_tok)
    check("call-quality ingest accepted (202 Accepted)", s in (200, 201, 202), f"{s} {r}")

    s, r = req("GET", "/api/admin/call-quality/summary", token=admin_tok)
    rows = r.get("rows", [])
    check("call-quality summary returned", s == 200 and len(rows) >= 1, f"{s} {r}")
    if rows:
        b = rows[0]
        check("call-quality aggregates present",
              "p50_packet_loss_pct" in b and "p50_jitter_ms" in b
              and "p50_rtt_ms" in b, f"{b}")

    print()
    print(f"{integration_test.passed} passed, {integration_test.failed} failed")
    return 0 if integration_test.failed == 0 else 1


if __name__ == "__main__":
    import sys
    sys.exit(main())
