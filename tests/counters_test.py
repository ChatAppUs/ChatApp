"""End-to-end checks for the C++ counters engine (CPP_CONVERSION_PLAN #5).

Covers: hashtag trending from the engine's 24h window, post view counts,
live-room viewer counts, the periodic flush into Postgres
(/internal/counters/flush), the fail-closed control-plane bearer, and the
SQL fallback contract (COUNTERS_URL unset -> identical API semantics).

Environment: API on :8080, psql reachable as postgres user, and (for the
engine-backed checks) the counters binary on :8600 with
COUNTERS_SECRET=test-counters, FLUSH_URL .../internal/counters/flush,
FLUSH_INTERVAL_MS<=2000. Without the engine the fallback checks still run.
"""
import integration_test
import json
import os
import subprocess
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req
from gaps6_test import register

ENGINE = os.environ.get("COUNTERS_URL", "http://localhost:8600")
ENGINE_SECRET = os.environ.get("COUNTERS_SECRET", "test-counters")


def engine_available():
    import urllib.request
    try:
        with urllib.request.urlopen(ENGINE + "/health", timeout=1) as r:
            return r.status == 200
    except Exception:
        return False


def engine_req(method, path, body=None):
    import urllib.request
    data = json.dumps(body).encode() if body is not None else None
    r = urllib.request.Request(ENGINE + path, data=data, method=method)
    r.add_header("Authorization", f"Bearer {ENGINE_SECRET}")
    if data:
        r.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(r, timeout=2) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except Exception:
            return e.code, {}


def psql(sql):
    subprocess.run(["sudo", "-u", "postgres", "psql", "-d", "chatapp", "-qc", sql],
                   check=False, capture_output=True)


def psql_val(sql):
    out = subprocess.run(["sudo", "-u", "postgres", "psql", "-t", "-d", "chatapp", "-c", sql],
                         check=False, capture_output=True, text=True)
    return out.stdout.strip()


def main():
    ts = int(time.time())
    alice = register(f"ct{ts}")

    engine = engine_available()
    if engine:
        # Control plane refuses forged bearers.
        import urllib.request
        try:
            r = urllib.request.Request(ENGINE + "/top/hashtags",
                                       headers={"Authorization": "Bearer forged"})
            urllib.request.urlopen(r, timeout=2)
            s = 200
        except urllib.error.HTTPError as e:
            s = e.code
        check("engine rejects forged bearer", s == 401, f"{s}")
    else:
        print("  SKIP engine checks (counters engine not reachable on :8600)")

    # ---------- hashtag trending ----------
    tag = f"ctr{ts}"
    s, r = req("POST", "/api/posts", {"body": f"hello #{tag} world", "visibility": "public"},
               token=alice)
    post_id = r.get("id")
    check("create post with hashtag", s in (200, 201) and post_id, f"{s} {r}")

    s, r = req("GET", "/api/hashtags/trending", token=alice)
    tags = {t.get("tag") for t in r.get("trending", [])}
    if engine:
        check("trending served with new tag", s == 200 and tag in tags, f"{s} {r}")
    else:
        check("trending fallback served with new tag", s == 200 and tag in tags, f"{s} {r}")

    if engine:
        before = int(psql_val(f"select use_count from hashtags where tag='{tag}'") or "0")
        # add one more use and wait for the flush cycle
        req("POST", "/api/posts", {"body": f"again #{tag}", "visibility": "public"}, token=alice)
        deadline = time.time() + 12
        after = before
        while time.time() < deadline:
            after = int(psql_val(f"select use_count from hashtags where tag='{tag}'") or "0")
            if after >= before + (1 if before else 2):
                break
            time.sleep(1)
        check("hashtag delta flushed to Postgres", after >= before + (1 if before else 2),
              f"before={before} after={after}")

    # ---------- post view counts ----------
    s, r = req("POST", f"/api/posts/{post_id}/view", {}, token=alice)
    check("record view", s == 200, f"{s} {r}")
    if engine:
        deadline = time.time() + 12
        views = 0
        while time.time() < deadline:
            views = int(psql_val(f"select view_count from posts where id='{post_id}'") or "0")
            if views >= 1:
                break
            time.sleep(1)
        check("view delta flushed to Postgres", views >= 1, f"views={views}")
    else:
        views = int(psql_val(f"select view_count from posts where id='{post_id}'") or "0")
        check("view counted (fallback)", views >= 1, f"views={views}")

    # ---------- live-room viewer counts ----------
    s, r = req("POST", "/api/live-rooms", {"title": f"counters {ts}"}, token=alice)
    room = (r.get("room") or {}).get("id")
    check("create live room", s in (200, 201) and room, f"{s} {r}")

    s, r = req("POST", f"/api/live-rooms/{room}/join", {}, token=alice)
    check("viewer join counted", s in (200, 201) and r.get("viewer_count", 0) >= 1,
          f"{s} {r}")

    if engine:
        time.sleep(1)
        s2, r2 = engine_req("GET", f"/viewers?room={room}")
        check("engine tracks room viewers", s2 == 200 and r2.get("viewers", 0) >= 1
              and r2.get("peak", 0) >= 1, f"{s2} {r2}")

    s, r = req("POST", f"/api/live-rooms/{room}/leave", {}, token=alice)
    check("viewer leave counted", s == 200 and r.get("viewer_count", 9) <= 1, f"{s} {r}")

    # ---------- integration with /internal/counters/flush directly ----------
    s, r = req("POST", "/internal/counters/flush", {"hashtags": [], "views": [], "peaks": []})
    check("flush endpoint requires internal secret", s == 401, f"{s}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    sys.exit(1 if integration_test.failed else 0)


if __name__ == "__main__":
    main()
