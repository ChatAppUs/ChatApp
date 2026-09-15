#!/usr/bin/env python3
"""End-to-end checks for guest-scoped, authenticated mesh queue operations."""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from integration_test import check, req


def mesh_headers(device_key):
    return {"X-Mesh-Device": device_key}


def main():
    ts = int(time.time())
    s, r = req("POST", "/api/auth/guest", {})
    check("guest session 200", s == 200, f"{s} {r}")
    check("guest returns access token", bool(r.get("access_token")), f"{r}")
    check("guest flag set", r.get("guest") is True, f"{r}")
    guest_token = r.get("access_token")

    s, r = req("GET", "/api/feed?limit=5", token=guest_token)
    check("guest can browse feed", s == 200, f"{s} {r}")

    dev_a = f"dev_a_{ts}"
    dev_b = f"dev_b_{ts}"
    dev_c = f"dev_c_{ts}"
    for device, transport in ((dev_a, "internet"), (dev_b, "wifi_direct"), (dev_c, "bluetooth")):
        s, r = req("POST", "/api/mesh/register",
                   {"device_key": device, "transport": transport},
                   token=guest_token, headers=mesh_headers(device))
        check(f"register {device}", s == 200 and r.get("device_id"), f"{s} {r}")

    ciphertext = "c2VjcmV0LWNpcGhlcnRleHQ="
    pid = f"pkt_{ts}"
    s, r = req("POST", "/api/mesh/send",
               {"packet_id": pid, "dest_device_key": dev_b,
                "payload": ciphertext, "ttl": 8},
               token=guest_token, headers=mesh_headers(dev_a))
    check("enqueue packet", s == 200 and r.get("status") == "queued", f"{s} {r}")

    s, r = req("POST", "/api/mesh/send",
               {"packet_id": pid, "dest_device_key": dev_b,
                "payload": ciphertext, "ttl": 8},
               token=guest_token, headers=mesh_headers(dev_a))
    check("duplicate packet dedup", s == 200 and r.get("status") == "duplicate", f"{s} {r}")

    s, r = req("GET", "/api/mesh/poll", token=guest_token, headers=mesh_headers(dev_b))
    check("poll returns packets", s == 200 and len(r.get("packets", [])) == 1, f"{s} {r}")

    s, r = req("PUT", "/api/mesh/relay-policy",
               {"device_key": dev_a, "relay_enabled": True, "relay_mode": "active"},
               token=guest_token, headers=mesh_headers(dev_a))
    check("set relay policy", s == 200 and r.get("status") == "policy_updated", f"{s} {r}")

    relay_id = f"relay_{ts}"
    s, r = req("POST", "/api/mesh/send",
               {"packet_id": relay_id, "dest_device_key": dev_c,
                "payload": ciphertext, "ttl": 8},
               token=guest_token, headers=mesh_headers(dev_a))
    check("enqueue relay packet", s == 200 and r.get("status") == "queued", f"{s} {r}")

    s, r = req("POST", "/api/mesh/relay",
               {"packet_id": relay_id, "next_device_key": dev_b},
               token=guest_token, headers=mesh_headers(dev_a))
    check("relay one hop", s == 200 and r.get("status") == "relayed", f"{s} {r}")

    s, r = req("GET", "/api/mesh/poll", token=guest_token, headers=mesh_headers(dev_b))
    check("relay destination receives packet", s == 200 and len(r.get("packets", [])) == 1, f"{s} {r}")

    s, r = req("GET", "/api/mesh/status", token=guest_token, headers=mesh_headers(dev_a))
    check("mesh status 200", s == 200 and "mesh" in r, f"{s} {r}")


if __name__ == "__main__":
    main()
