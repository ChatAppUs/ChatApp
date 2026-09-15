# Implementation audit — 2026-09-15

This is an audit artifact, not a product specification. The root Markdown files remain the sole requirements source. This verification was performed against the executable source on `main`; `agent.md`, prior assistant claims, and prior commits were not used as evidence of implementation.

## Fresh source verification

The current `main` tree contains the documented multi-platform application, Go/Rust/C++/Python services, PostgreSQL migrations, mesh engine, native Android/iOS mesh implementations, web/admin clients, feature registry and validation scripts.

A source search for common unfinished markers (`TODO`, `FIXME`, `STUB`, `MOCK`, fake implementation markers, and placeholder implementation errors) returned no matching unfinished implementation in the indexed repository source. This does **not** substitute for compilation or runtime testing.

The Android mesh identity implementation currently contains the same hardened primitives documented by the Go engine: Ed25519 signed beacons, X25519 key agreement, HKDF-SHA256 per-peer keys, key rotation, trust-on-first-use pinning, revocation and sliding-window replay protection. Therefore the older documentation sentence claiming that native clients still use only the pre-shared-key path is stale and must not be treated as current evidence.

## Source-completable functionality confirmed present

- AES-256-GCM mesh packet encryption with explicit nonce/tag handling.
- Versioned packet envelope and strict packet validation.
- CSPRNG packet identifiers with fail-closed behavior if the OS CSPRNG is unavailable.
- Ed25519-signed discovery beacons and TOFU public-key pinning.
- X25519/HKDF-SHA256 per-peer session keys and epoch rotation.
- Local mesh key revocation and downgrade rejection.
- Per-source replay windows and exactly-once application delivery for reliable transfers.
- Authenticated layered forwarding.
- MTU fragmentation/reassembly with bounded memory/time and SHA-256 integrity verification.
- ACK/retry with bounded exponential backoff and explicit delivery states.
- Priority/byte-bounded forwarding queues and per-source relay quotas.
- Store-and-forward routing, neighbour expiry, route scoring and alternate-path retry.
- Android/iOS native mesh session-key implementation.
- Real backend/API/database/client implementations for the feature areas recorded as implemented in `IMPLEMENTATION_STATUS.md`.

## Defects fixed in the preceding source pass

1. Mesh packet IDs no longer fall back to predictable timestamps when the CSPRNG fails.
2. Mesh deduplication metadata is bounded together: evicting `seen` also evicts its corresponding `bestTTL` entry.
3. Regression tests cover sustained dedup churn and cryptographic packet-ID generation.

## Remaining source/product gaps

These remain deliberately **not claimed complete** because the repository source alone cannot establish the required behavior, or because the source requirement is genuinely incomplete:

- congestion control with measured link feedback;
- full route-repair state machine beyond bounded alternate-path retry;
- true multi-path path selection with path-level metrics;
- group sender-key lifecycle/rotation and cryptographically accountable group acknowledgements;
- authenticated network-wide distribution of mesh revocation decisions;
- Tor/onion IP-privacy transport and anonymity guarantees;
- physical Bluetooth/Wi-Fi Direct interoperability, permissions, reconnect and background-execution validation;
- Android/iOS release builds, signing and store distribution;
- 3/10/50/100/500-device physical mesh experiments with latency/loss/throughput/battery/thermal evidence;
- real-time offline mesh voice/video with codec adaptation, jitter/loss recovery and congestion-aware scheduling;
- configured external AI/SMTP/SMS/payment/KYC/blockchain/provider validation;
- production load, observability, backup/restore, regional failure and disaster-recovery execution;
- independent security review and penetration testing.

These are not replaced with simulations, fake providers, mock data, bypasses or hardcoded credentials. Where a real external dependency is required, the application must report truthful unavailable/configuration state rather than fabricate success.

## Validation boundary

This execution environment has GitHub source access and repository write access, but it does not provide the complete production runtime, Android/iOS release toolchains, physical radio hardware, provider credentials, or a persistent production-like deployment. Therefore this audit does not certify those external gates.

The repository's existing validation scripts and previously recorded passing checks remain useful evidence, but they are not a substitute for the environment-dependent gates above.

## Commits already on `main`

- `2f4f0e7fac87d024edda2b735f527ee971c1f2d9` — fail closed when mesh packet-ID CSPRNG is unavailable.
- `f7e9c4d94671bd3b41f4f0c1d275b3638b87fbb9` — bound mesh deduplication TTL metadata.
- `538fc70cf64338826d6ce38f459fbe9c96e83ee0` — regression tests.
- `abf2a0136bd39b67dd4d8c0dc540b07346cde066` — prior audit/status record.