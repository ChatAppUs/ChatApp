# Implementation audit — 2026-09-15

This is an audit artifact, not a new product specification. The root specification files remain the source of truth. The audit was performed against the executable source on `main`; `agent.md`, prior assistant claims, and prior commits were not used as evidence of implementation.

## Findings in this pass

### Implemented / fixed in this pass

1. **Packet-ID cryptographic fallback removed.** `services/mesh/packet.go` previously fell back from the operating-system CSPRNG to a timestamp-derived packet ID. Packet IDs participate in deduplication, transfer correlation and fragment grouping, so a predictable fallback weakened those invariants. The implementation now fails closed when the CSPRNG is unavailable instead of generating predictable identifiers.
2. **Mesh deduplication metadata leak fixed.** `RouteTable` bounded the `seen` packet cache but not `bestTTL`. Long-lived relays could therefore accumulate one `bestTTL` entry per packet indefinitely. The pruning path now removes the corresponding `bestTTL` entry whenever the oldest `seen` entry is evicted.
3. **Regression coverage added.** `services/mesh/routing_memory_test.go` verifies both deduplication maps remain bounded under sustained packet churn and that fresh packet IDs are accepted.

## Requirements audit status

The source-completable mesh requirements already present on `main` include AES-256-GCM envelopes, X25519/HKDF per-peer sessions, Ed25519 signed discovery, local key revocation, replay windows, authenticated layered forwarding, MTU fragmentation/reassembly, ACK/retry, delivery states, priority queues, store-and-forward, relay quotas, route scoring and native Android/iOS session-key adoption.

The current root specifications still identify these as **not proven complete or still outstanding**:

- physical Bluetooth/Wi-Fi Direct radio validation;
- Android/iOS release compilation and signing in this environment;
- Tor/onion IP-privacy transport and network-wide anonymity guarantees;
- network-wide distribution of mesh revocation decisions;
- congestion control, route repair and true multipath selection;
- group sender-key lifecycle/rotation and group acknowledgements;
- 3/10/50/100/500-device physical mesh experiments;
- real-time offline mesh voice/video with codec adaptation, jitter buffering and loss recovery;
- configured external AI, SMTP/SMS, payment/KYC, blockchain/RPC and other provider validation;
- production load, observability, backup/restore and disaster-recovery execution.

These are not marked complete merely because related routes or abstractions exist. Some require real hardware, providers or deployed infrastructure; source-only work must not be represented as physical/runtime proof.

## Validation boundary

This environment has GitHub source access and repository write access, but it does not provide the repository's complete production runtime, Android/iOS toolchains, physical radio hardware, provider credentials, or a persistent deployment suitable for claiming production certification. Consequently this audit does not claim those external gates are complete.

## Commits

- `2f4f0e7fac87d024edda2b735f527ee971c1f2d9` — fail closed when mesh packet-ID CSPRNG is unavailable.
- `f7e9c4d94671bd3b41f4f0c1d275b3638b87fbb9` — bound mesh deduplication TTL metadata.
- `538fc70cf64338826d6ce38f459fbe9c96e83ee0` — add regression tests and this audit artifact.
