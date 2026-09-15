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

marker = "## Verification addendum — 2026-09-15\n"
body = """## Verification addendum — 2026-09-15

This revision was checked against the executable source tree on `main`, not against prior commit prose. The repository contains the documented API, web/admin clients, native client source, PostgreSQL migrations, Go/Rust/C++/Python services, mesh cryptography and the existing end-to-end test harness. The web and admin type checks and production builds, feature-registry validation, route parity validation, and patch whitespace validation pass in this environment.

A concrete security hardening pass is now implemented in `services/api/auth.go`: JWT parsing rejects oversized tokens, unsupported header algorithms, missing or non-positive expiry, and tokens issued more than five minutes in the future. Regression coverage is in `services/api/api_test.go`, and an executable Go fuzz target is in `services/api/auth_fuzz_test.go`. These checks reduce parser abuse and algorithm-confusion risk; they do not replace deployment key rotation, external security review, or device-level testing.

**Status remains explicit.** Implemented means present in source and covered by available tests. Partial means the application boundary or fallback exists but a production dependency, provider, hardware path, or release toolchain is not available here. Outstanding items remain Tor/onion IP-privacy transport, physical Bluetooth/Wi-Fi Direct validation, native Android/iOS release builds, configured AI providers, live SMTP/SMS delivery, production load testing, disaster recovery execution, and deployed tracing/alerting. No claim of completion is made for those environment-dependent gates.

"""

for name in files:
    path = root / name
    text = path.read_text()
    if marker not in text:
        path.write_text(text.rstrip() + "\n\n" + body)
print(f"updated {len(files)} authoritative markdown files")
