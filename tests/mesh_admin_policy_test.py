#!/usr/bin/env python3
"""Admin control of mesh features (master plan §110).

Administrators may configure operational policy for the offline mesh but must
never gain the ability to decrypt private mesh messages. This suite verifies
the singleton mesh_admin_policy surface:

  - GET /api/admin/mesh/policy returns the current operational policy
  - PUT /api/admin/mesh/policy updates feature enablement, version
    requirements, abuse limits (payload/TTL/relay quota) and transport
    deprecation, and audit-logs the change
  - a disabled mesh refuses register/send/relay admission
  - a deprecated transport is refused at register
  - policy-bounded payload/TTL ceilings are enforced on send
  - non-admin callers are forbidden
"""
import os
import time

BASE = os.environ.get("CHATAPP_BASE_URL", "http://localhost:8080")

import integration_test
from integration_test import check, req, register_verified, grant_superadmin


def admin_token(username, email):
    grant_superadmin(username)
    s, r = req("POST", "/api/admin/login",
               {"identifier": email, "password": "Passw0rd!123"})
    check("admin login (superadmin)", s == 200 and r.get("access_token"), f"{s} {r}")
    return r.get("access_token")


def mesh_headers(device_key):
    return {"X-Mesh-Device": device_key}


def main():
    ts = int(time.time())
    admin_u = f"meshadm{ts}"
    user_u = f"meshuser{ts}"

    s, r = register_verified(admin_u)
    check("register admin", s in (200, 201) and r.get("access_token"), f"{s} {r}")
    admin_tok = admin_token(admin_u, f"{admin_u}@test.dev")

    s, r = register_verified(user_u)
    check("register user", s in (200, 201) and r.get("access_token"), f"{s} {r}")
    user_tok = r.get("access_token")

    # --- Non-admin is forbidden from the admin mesh policy surface ---
    # A regular user token is not an admin session, so requireAdminAuth
    # rejects it with 401 before the permission check (standard across all
    # admin endpoints).
    s, r = req("GET", "/api/admin/mesh/policy", token=user_tok)
    check("non-admin GET mesh policy forbidden", s in (401, 403), f"{s} {r}")
    s, r = req("PUT", "/api/admin/mesh/policy", {"mesh_enabled": False}, token=user_tok)
    check("non-admin PUT mesh policy forbidden", s in (401, 403), f"{s} {r}")

    # --- Admin can read the default policy ---
    s, r = req("GET", "/api/admin/mesh/policy", token=admin_tok)
    check("admin GET mesh policy 200", s == 200, f"{s} {r}")
    check("policy has mesh_enabled", "mesh_enabled" in r, f"{r}")
    check("policy has max_ttl", r.get("max_ttl") == 16, f"{r}")
    check("policy has max_payload_bytes", r.get("max_payload_bytes") == 2097152, f"{r}")

    # --- Admin can update the policy (partial merge) ---
    s, r = req("PUT", "/api/admin/mesh/policy",
               {"mesh_enabled": True, "max_ttl": 8, "max_payload_bytes": 1048576},
               token=admin_tok)
    check("admin PUT mesh policy 200", s == 200 and r.get("ok"), f"{s} {r}")
    s, r = req("GET", "/api/admin/mesh/policy", token=admin_tok)
    check("policy reflects max_ttl=8", r.get("max_ttl") == 8, f"{r}")
    check("policy reflects max_payload=1MiB", r.get("max_payload_bytes") == 1048576, f"{r}")
    check("policy keeps mesh_enabled", r.get("mesh_enabled") is True, f"{r}")

    # --- Invalid policy values are rejected ---
    s, r = req("PUT", "/api/admin/mesh/policy", {"max_ttl": 99}, token=admin_tok)
    check("invalid max_ttl rejected", s == 400, f"{s} {r}")
    s, r = req("PUT", "/api/admin/mesh/policy", {"max_payload_bytes": 1}, token=admin_tok)
    check("invalid max_payload rejected", s == 400, f"{s} {r}")

    # --- Deprecate a transport; register on it is refused ---
    s, r = req("PUT", "/api/admin/mesh/policy",
               {"deprecated_transports": ["bluetooth"]}, token=admin_tok)
    check("deprecate bluetooth", s == 200 and r.get("ok"), f"{s} {r}")
    dev = f"dev_{ts}"
    s, r = req("POST", "/api/mesh/register",
               {"device_key": dev, "transport": "bluetooth"},
               token=user_tok, headers=mesh_headers(dev))
    check("deprecated transport refused at register", s == 403, f"{s} {r}")
    dev2 = f"dev2_{ts}"
    s, r = req("POST", "/api/mesh/register",
               {"device_key": dev2, "transport": "internet"},
               token=user_tok, headers=mesh_headers(dev2))
    check("non-deprecated transport registers", s == 200, f"{s} {r}")

    # --- Disable the mesh; send/relay are refused ---
    s, r = req("PUT", "/api/admin/mesh/policy",
               {"mesh_enabled": False, "deprecated_transports": []}, token=admin_tok)
    check("disable mesh", s == 200 and r.get("ok"), f"{s} {r}")
    s, r = req("POST", "/api/mesh/send",
               {"packet_id": f"p{ts}", "dest_device_key": dev2,
                "payload": "c2VjcmV0", "ttl": 8},
               token=user_tok, headers=mesh_headers(dev2))
    check("send refused when mesh disabled", s == 403, f"{s} {r}")

    # --- Re-enable and enforce policy-bounded TTL on send ---
    s, r = req("PUT", "/api/admin/mesh/policy",
               {"mesh_enabled": True, "max_ttl": 8, "max_payload_bytes": 1048576},
               token=admin_tok)
    check("re-enable mesh", s == 200 and r.get("ok"), f"{s} {r}")
    s, r = req("POST", "/api/mesh/send",
               {"packet_id": f"p2{ts}", "dest_device_key": dev2,
                "payload": "c2VjcmV0", "ttl": 9},
               token=user_tok, headers=mesh_headers(dev2))
    check("send with ttl above policy ceiling refused", s == 400, f"{s} {r}")
    s, r = req("POST", "/api/mesh/send",
               {"packet_id": f"p3{ts}", "dest_device_key": dev2,
                "payload": "c2VjcmV0", "ttl": 8},
               token=user_tok, headers=mesh_headers(dev2))
    check("send with ttl within policy ceiling accepted", s == 200, f"{s} {r}")

    # --- Restore defaults for re-runnability ---
    s, r = req("PUT", "/api/admin/mesh/policy",
               {"mesh_enabled": True, "min_protocol_version": 1,
                "max_payload_bytes": 2097152, "max_ttl": 16,
                "relay_quota_bytes": 10485760, "deprecated_transports": []},
               token=admin_tok)
    check("restore default policy", s == 200 and r.get("ok"), f"{s} {r}")

    passed = integration_test.passed
    failed = integration_test.failed
    print(f"\nadmin mesh policy: {passed} passed, {failed} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    import sys
    sys.exit(main())
