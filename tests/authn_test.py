"""End-to-end checks for the Rust authn service (RUST_CONVERSION_PLAN P0).

Covers the delegation chain the Go API relies on today: password
hash/verify, JWT mint/verify (register+login+requireAuth), TOTP
generate/verify, the OTP engine (phone send/check), CSPRNG token minting,
and the fail-closed control-plane bearer, plus fallback semantics (AUTHN
unset = same observable behavior).

Environment: API on :8080, and (for the delegated checks) the authn binary
on :8400 with AUTHN_SECRET=test-authn-secret + JWT_SECRET matching the API's
JWT_SECRET.
"""
import json
import os
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
import integration_test
from integration_test import check, req
from gaps6_test import register

AUTHN = os.environ.get("AUTHN_URL", "http://localhost:8400")
AUTHN_SECRET = os.environ.get("AUTHN_SECRET", "test-authn-secret")


def engine_available(path="/health"):
    import urllib.request
    try:
        with urllib.request.urlopen(AUTHN + path, timeout=1) as r:
            return r.status == 200
    except Exception:
        return False


def authn_req(path, body):
    import urllib.request
    r = urllib.request.Request(AUTHN + path, data=json.dumps(body).encode(), method="POST")
    r.add_header("Authorization", f"Bearer {AUTHN_SECRET}")
    r.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(r, timeout=2) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except Exception:
            return e.code, {}


def main():
    ts = int(time.time())
    engine = engine_available()
    if engine:
        # Control plane refuses forged bearers.
        import urllib.request
        try:
            r = urllib.request.Request(AUTHN + "/jwt/verify",
                                       data=b'{"token":"x"}',
                                       headers={"Authorization": "Bearer forged"})
            urllib.request.urlopen(r, timeout=2)
            s = 200
        except urllib.error.HTTPError as e:
            s = e.code
        check("authn rejects forged bearer", s == 401, f"{s}")
    else:
        print("  SKIP engine checks (authn service not reachable on :8400)")

    # -------- full register/login over the delegated surface --------
    name = f"au{ts}"
    token = register(name)  # register -> /password/hash, token -> /jwt/mint
    check("register via authn", bool(token), "")

    s, r = req("POST", "/api/auth/login", {"identifier": f"{name}@test.dev",
                                           "password": "Passw0rd!123"})
    check("login via authn", s == 200 and r.get("access_token"), f"{s} {r}")

    s, r = req("POST", "/api/auth/login", {"identifier": f"{name}@test.dev",
                                           "password": "wrong"})
    check("bad password rejected", s == 401, f"{s} {r}")

    s, r = req("GET", "/api/me", token=token)
    check("requireAuth round-trip", s == 200 and r.get("username") == name, f"{s} {r}")

    s, r = req("GET", "/api/me", token=token + "x")
    check("forged token rejected", s == 401, f"{s}")

    # -------- TOTP round-trip through the engine (then disable) --------
    s, r = req("POST", "/api/auth/2fa/setup", {}, token=token)
    secret = r.get("secret")
    check("TOTP setup issues secret", s == 200 and bool(secret), f"{s} {r}")

    if secret:
        import hashlib, hmac, struct

        def totp_code_now(secret):
            import base64
            key = base64.b32decode(secret.upper())
            counter = int(time.time()) // 30
            mac = hmac.new(key, struct.pack(">Q", counter), hashlib.sha1).digest()
            off = mac[-1] & 0x0f
            code = ((mac[off] & 0x7f) << 24 | mac[off + 1] << 16 |
                    mac[off + 2] << 8 | mac[off + 3]) % 1000000
            return f"{code:06d}"

        code = totp_code_now(secret)
        s, r = req("POST", "/api/auth/2fa/enable", {"code": code}, token=token)
        check("TOTP verify via engine", s == 200, f"{s} {r}")
        s, r = req("POST", "/api/auth/2fa/disable", {"code": totp_code_now(secret)}, token=token)
        check("TOTP disable", s == 200, f"{s} {r}")

    # -------- OTP engine: phone send/check over delegated RNG --------
    phone = f"+1555{ts % 10000000:07d}"
    s, r = req("POST", "/api/auth/phone/send-code", {"phone": phone})
    check("OTP send works", s in (200, 201), f"{s} {r}")
    code = r.get("dev_code")
    if code:
        s, r = req("POST", "/api/auth/phone/check-code",
                   {"phone": phone, "code": code})
        check("OTP check works", s == 200 and r.get("status") == "verified", f"{s} {r}")

    if engine:
        # direct engine-level check for the HMAC primitive + random minting
        s, r = authn_req("/random", {"bytes": 16})
        check("random token minted", s == 200 and len(r.get("token", "")) == 32, f"{s} {r}")
        s, r = authn_req("/hmac", {"key": "k", "message": "m"})
        check("hmac primitive", s == 200 and len(r.get("signature", "")) == 64, f"{s} {r}")

    print(f"\n{integration_test.passed} passed, {integration_test.failed} failed")
    sys.exit(1 if integration_test.failed else 0)


if __name__ == "__main__":
    main()
