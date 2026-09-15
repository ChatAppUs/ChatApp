from pathlib import Path

root = Path(__file__).resolve().parent.parent
files = [
    "Anonymous.md",
    "COMPETITIVE_GAP_ANALYSIS.md",
    "ChatApp_Competitive_Comparison.md",
    "ChatApp_Competitor_Comparison.md",
    "ChatApp_Complete_Features_and_Architecture_Master_Plan.md",
    "ChatApp_Complete_Master_Documentation.md",
    "ChatApp_Deep_Code_and_Documentation_Audit.md",
    "ChatApp_Implementation_and_Dependency_Gap_Register.md",
    "ChatApp_Offline_Mesh_500_Device_Analysis.md",
    "ChatApp_Offline_Mesh_Analysis.md",
    "IMPLEMENTATION_STATUS.md",
    "Identity-Authentication-and-Account-Security.md",
    "README.md",
]
marker = "## Mesh key-revocation verification — 2026-09-15\n"
body = """## Mesh key-revocation verification — 2026-09-15

The renewed source audit identified device-key revocation as a genuine mesh lifecycle gap. It is now implemented in `services/mesh/node.go` and `services/mesh/routing.go`. A node can revoke only the exact Ed25519 public key already pinned for a device id; revocation immediately removes the peer route and advertised KEM session, rejects future signed beacons, and blocks unsigned legacy-beacon downgrade. `services/mesh/revocation_test.go` verifies normal admission, key-matched revocation, route withdrawal, signed re-entry rejection, unsigned downgrade rejection, and mismatched-key rejection. Race-enabled tests pass.

The implementation is **local trust revocation**. Network-wide distribution of revocation decisions still requires a separately authenticated device-management channel, and physical radio validation remains environment-dependent. Those requirements remain outstanding rather than being falsely marked complete.

"""
for name in files:
    path = root / name
    text = path.read_text()
    if marker not in text:
        path.write_text(text.rstrip() + "\n\n" + body)
print(f"updated {len(files)} authoritative markdown files")
