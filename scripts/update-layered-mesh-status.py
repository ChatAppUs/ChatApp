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
marker = "## Layered mesh forwarding verification — 2026-09-15\n"
body = """## Layered mesh forwarding verification — 2026-09-15

A source-level gap identified during the renewed audit was closed in `services/mesh`: forwarded packets now support an authenticated layered envelope. The sender constructs per-hop AES-GCM layers using authenticated X25519 session keys, each relay peels only its own layer and learns only the next hop, and the destination-only layer reveals the final destination and payload. Tampering, missing session keys, malformed envelopes, and oversized packet inputs fail closed. `services/mesh/onion_test.go` verifies three-node peeling, final-destination confidentiality from the outer layer, plaintext recovery, and tamper rejection.

This closes the **source-completable layered-forwarding requirement**. It does not claim Tor/onion-network anonymity, source-metadata protection, physical Bluetooth/Wi-Fi Direct validation, or production radio behavior. Those remain separate outstanding requirements and still require additional protocol design, deployed infrastructure, or physical-device validation.

"""
for name in files:
    path = root / name
    text = path.read_text()
    if marker not in text:
        path.write_text(text.rstrip() + "\n\n" + body)
print(f"updated {len(files)} authoritative markdown files")
