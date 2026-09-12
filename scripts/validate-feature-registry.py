#!/usr/bin/env python3
"""Validate the master-plan feature registry used by CI."""
import json
import sys
from pathlib import Path

REQUIRED = {"web", "android", "ios", "desktop", "extension", "backend", "database"}
STATUSES = {"IMPLEMENTED", "TESTED", "SUPPORTED", "DEPRECATED", "PARTIAL"}


def main() -> int:
    path = Path(sys.argv[1] if len(sys.argv) > 1 else "feature-registry.json")
    data = json.loads(path.read_text())
    clients = set(data.get("required_clients", []))
    if clients != REQUIRED:
        raise SystemExit(f"required_clients mismatch: {sorted(clients)}")
    features = data.get("features")
    if not isinstance(features, list) or not features:
        raise SystemExit("features must be a non-empty list")
    seen = set()
    for feature in features:
        missing = ({"id", "domain", "priority", "status"} | REQUIRED) - set(feature)
        if missing:
            raise SystemExit(f"{feature.get('id', '<unknown>')} missing {sorted(missing)}")
        if feature["id"] in seen:
            raise SystemExit(f"duplicate feature id: {feature['id']}")
        seen.add(feature["id"])
        if feature["priority"] not in {"P0", "P1", "P2"}:
            raise SystemExit(f"invalid priority: {feature['id']}")
        if feature["status"] not in STATUSES:
            raise SystemExit(f"invalid status: {feature['id']}")
        if any(type(feature[client]) is not bool for client in REQUIRED):
            raise SystemExit(f"client values must be boolean: {feature['id']}")
    print(f"feature registry: OK ({len(features)} features, {len(REQUIRED)} required clients)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
