# ChatApp implementation and dependency gap register

## Current implementation-status notice (2026-09-15)

This specification is a requirements source, not proof that a feature is production-complete. The current source-backed status is maintained in `IMPLEMENTATION_STATUS.md` and the audit in `ChatApp_Deep_Code_and_Documentation_Audit.md`.

| Status | Meaning |
|---|---|
| **Implemented** | Corresponding source, route/schema/client surface, and repository validation exist. Runtime or production evidence may still be required. |
| **Partial** | A source implementation exists, but an explicitly documented requirement, client, provider, or reliability guarantee is incomplete. |
| **Outstanding** | The capability requires external devices, providers, operators, licensing, production infrastructure, or additional source work. |

The current checkout passes the parity and feature-registry checks. Go, C++, PostgreSQL-backed integration, native Android/iOS, and production deployment gates are environment-dependent and must not be inferred from static source inspection.


Assessment date: 2026-09-14. This register separates code that can be written and tested in the repository from capabilities that require devices, providers, operators, licensing or a user network.

## Direct source-scan findings

### P0 correctness
- **Resolved in source:** Go, Android, and iOS now use the AES-256-GCM envelope with an explicit 12-byte nonce; iOS preserves the authentication tag. The interoperability contract is covered by `services/mesh/interop_test.go` and native codec implementations.
- **Resolved in source:** Android and iOS relay forwarding decrements TTL, increments hops, re-enqueues, and flushes packets while local packet IDs are deduplicated.
- **Resolved in source:** iOS uses `NSLock` for mesh state synchronization; no Kotlin-only `@Volatile` declaration remains in the Swift mesh engine.
- **Still open as validation:** the mobile projects have build configuration but no checked-in Gradle wrapper or generated Xcode project. CI cannot prove signed Android/iOS release builds from this repository alone.

### P1 reliability and scale

- Mesh routing has finite TTL, queue size and packet lifetime. **MTU fragmentation/reassembly is now implemented** (see the 2026-09-15 pass at the end of this file): payloads above the 512-byte datagram ceiling are split into individually encrypted fragments and reassembled under bounded memory and time with a verified digest. Still absent: ACK/retry window, route repair, congestion control, multipath selection and the 500-device hardware test. Relay fairness is partially addressed by per-source token-bucket quotas.
- The current Internet call stack is separate from the mesh. SFU/TURN/WebRTC cannot make a live call when every IP path is unavailable.
- Radio discovery and permissions are platform-specific. Android Wi-Fi Direct/Bluetooth and iOS CoreBluetooth/local Wi-Fi need device state machines, background execution policy, reconnect handling and physical tests.
- Source coverage is broad but production proof is incomplete: native mobile builds, app-store signing, push wake, crash reporting, SLO dashboards, soak tests, disaster recovery and restore drills remain release gates.

### P1 product parity

- Facebook parity needs a real social graph, mature ranking, Pages/Groups management, ad measurement, creator distribution, safety operations and a populated network.
- TikTok parity needs camera/effects/sounds, duet/stitch/remix, rights, recommendation experiments, creator analytics/rewards, LIVE moderation and global video delivery.
- X parity needs high-availability public fan-out/search, Communities/Spaces quality, subscriptions and payouts, verified trust signals and anti-spam/bot operations.
- Telegram parity needs proven multi-device/cloud sync, large groups/channels, public bot API and mini-app SDK, import/export, privacy modes and client ecosystem.
- imo parity needs weak-network contact discovery, push/reconnect, call quality, privacy chat/screenshot controls and released mobile apps.

## Dependency map

| Dependency | Current role | What coding can solve | What must be supplied externally |
|---|---|---|---|
| PostgreSQL | durable users, messages, social and commerce data | migrations, indexes, transactions, retention and restore scripts | backups, replicas, storage, failover and operator drills |
| Redis | rate limits/cache/pub-sub support | key design, timeouts and fallback behaviour | HA topology, memory, persistence and monitoring |
| Go API/mesh/SFU | control plane, routing, media signalling | authz, protocol, route repair, metrics and tests | public bandwidth, TLS, load balancers, TURN reachability |
| Rust authn/security | password/JWT/TOTP and security boundary | test vectors, key rotation and hardening | secret storage, HSM/KMS policy, audit and incident response |
| C++ realtime/media/counters | fan-out, media and hot paths | backpressure, framing and benchmarks | CPU capacity, kernel/network tuning, capacity planning |
| Android/iOS/desktop | client surfaces and radio access | release automation, cross-platform fixtures, offline state machines | Android/iOS hardware, signing identities, app-store review, APNs/FCM |
| Python ML service | optional assistant, moderation, ASR/TTS/translation hooks | explicit provider adapters, limits and truthful unavailable states | model weights/GPU, model licence, inference capacity or provider API keys |
| WebRTC/TURN/SFU | Internet voice/video/live paths | ICE policy, reconnect, quality telemetry and load tests | reachable public IPs, UDP/TCP ports, TURN bandwidth and regional PoPs |
| OAuth/email/SMS/payments | account recovery, login and commerce | adapter validation, retries, idempotency and webhooks | provider accounts, quotas, verified domains/numbers, payment/KYC compliance |
| Blockchain/RPC | wallet/staking/settlement surfaces | validation, nonce handling, reconciliation and circuit breakers | chain RPC, gas, custody policy, audits and legal/compliance review |
| CDN/object storage/FFmpeg | media upload, transcode and delivery | resumable upload, lifecycle, cache keys and media validation | storage, egress, CDN capacity and codec/licensing rights |

## ML/provider gap

The ML image includes FastAPI/Pydantic but not a model package or model weights. `TRANSLATE_MODEL` alone does not make arbitrary-language translation available. Production deployment must select either an installed/licensed local model with its runtime and weights, or an authenticated external provider with rate limits, privacy policy, retries, cost controls and data residency decisions. The same rule applies to ASR, TTS, embeddings and safety moderation.

## Release gates before claiming competition

- Green Go, Rust, C++, Python, web, admin, Android and iOS builds in CI.
- Cross-platform mesh encryption/codec fixtures and real-device radio tests.
- 3/10/50/100/500-device offline experiments with p50/p95 latency, loss, throughput, battery, thermal and route-survival reports.
- Internet call/load tests through SFU/TURN with reconnect, NAT and network-change scenarios.
- Security review for key management, device revocation, group keys, relay abuse, spam, privacy and metadata leakage.
- Backup restore, regional failure, queue replay, webhook idempotency and incident-response drills.
- App-store release, crash/ANR monitoring, push delivery measurement, accessibility and localisation review.

## Correct conclusion

The repository has a substantial prototype/platform foundation, not a finished peer to five mature global networks. The highest-return work is correctness and proof — especially interoperable mesh, mobile builds, weak-network calls, operations and safety — rather than adding more unverified screens.


## Deep source-and-documentation audit — 2026-09-14

This register is expanded and reconciled by `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`, including the mesh dependency reduction and validation evidence.


## Deep source-and-documentation audit — 2026-09-14

The mesh crypto/forwarding gaps identified by this register were implemented and tested in the current pass; the full source audit and remaining dependency boundaries are in `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`.

Follow-up pass (same day): mesh routing was hardened further with first-party code — Ed25519-signed discovery beacons with per-device key pinning, deterministic route scoring (relay consent, link throughput, freshness), and neighbour route expiry. The P1 findings on route repair/congestion and the shared payload key remain open; see the audit's "Remaining device-identity hardening" and "Real-time offline voice/video" items.

Third pass (same day, standard library only) — the shared payload key gap is closed in the Go engine, plus two further hardening items:

- **Per-peer session keys (P1 resolved in Go and native):** X25519 key agreement with the public key advertised inside the signed beacon; unicast payloads use HKDF-derived AES-256 session keys; pre-shared-key fallback keeps legacy/native interop; third-party holders of the pre-shared key cannot decrypt session payloads (tested). The Android/iOS native engines now adopt the same signed-beacon KEM advertisement and HKDF derivation (`MeshIdentity.kt` / `MeshIdentity.swift`), with trust-on-first-use pinning, local key revocation and per-source replay protection; the Android logic is unit-tested on the JVM (20/20).
- **Replay protection (resolved):** per-sender monotonic sequence numbers with a 1024-slot sliding-window filter per source, checked after successful decryption so forged packets cannot flush the window.
- **Session rotation (resolved in Go):** epoch-bumped key regeneration with graceful pre-shared-key fallback between beacons. Revocation and privacy-preserving identifiers remain open.
- **Duplicate delivery (resolved):** destinations no longer re-deliver TTL-improved duplicate copies; exactly-once app delivery is regression-tested, and the related grid-flood deadlock is gone.
- **Call feasibility (resolved, honest mode):** `CallFeasible` classifies each call attempt as direct / multihop / unreachable so clients can degrade to low-bitrate audio or fall back to voice notes; per-source relay token buckets bound forwarding abuse; deterministic simulation covers chains, partitions, healing and a 100-device grid.

See audit §7 for evidence. Remaining open items are unchanged: native session-key adoption, physical-radio validation, 5k/50k-node sweeps, real-time media codec/congestion work, and the external infrastructure list below.

Follow-up pass (same day, session keys + scale): the shared payload key is replaced for unicast traffic by per-peer X25519 (stdlib `crypto/ecdh`) session keys with HKDF-SHA256 derivation and epoch rotation, advertised inside Ed25519-signed beacons; post-decrypt sliding-window replay protection and the duplicate-delivery fix landed in the same engine pass. Honest call feasibility (`direct`/`multihop`/`unreachable`), per-source relay token-bucket quotas, and a deterministic simulation harness (chain, partition-heal, 100-node grid; 500/5k/50k sweep CLI) are implemented and tested. The Android/iOS native engines now adopt the session-key envelope, signed beacons, local key revocation and replay protection (`MeshIdentity.kt` / `MeshIdentity.swift`). Remaining open: network-wide revocation distribution, physical-radio validation, and production operations (see the audit's narrowed device-identity item).

## Verification addendum — 2026-09-15

This revision was checked against the executable source tree on `main`, not against prior commit prose. The repository contains the documented API, web/admin clients, native client source, PostgreSQL migrations, Go/Rust/C++/Python services, mesh cryptography and the existing end-to-end test harness. The web and admin type checks and production builds, feature-registry validation, route parity validation, and patch whitespace validation pass in this environment.

A concrete security hardening pass is now implemented in `services/api/auth.go`: JWT parsing rejects oversized tokens, unsupported header algorithms, missing or non-positive expiry, and tokens issued more than five minutes in the future. Regression coverage is in `services/api/api_test.go`, and an executable Go fuzz target is in `services/api/auth_fuzz_test.go`. These checks reduce parser abuse and algorithm-confusion risk; they do not replace deployment key rotation, external security review, or device-level testing.

**Status remains explicit.** Implemented means present in source and covered by available tests. Partial means the application boundary or fallback exists but a production dependency, provider, hardware path, or release toolchain is not available here. Outstanding items remain Tor/onion IP-privacy transport, physical Bluetooth/Wi-Fi Direct validation, native Android/iOS release builds, configured AI providers, live SMTP/SMS delivery, production load testing, disaster recovery execution, and deployed tracing/alerting. No claim of completion is made for those environment-dependent gates.

## Layered mesh forwarding verification — 2026-09-15

A source-level gap identified during the renewed audit was closed in `services/mesh`: forwarded packets now support an authenticated layered envelope. The sender constructs per-hop AES-GCM layers using authenticated X25519 session keys, each relay peels only its own layer and learns only the next hop, and the destination-only layer reveals the final destination and payload. Tampering, missing session keys, malformed envelopes, and oversized packet inputs fail closed. `services/mesh/onion_test.go` verifies three-node peeling, final-destination confidentiality from the outer layer, plaintext recovery, and tamper rejection.

This closes the **source-completable layered-forwarding requirement**. It does not claim Tor/onion-network anonymity, source-metadata protection, physical Bluetooth/Wi-Fi Direct validation, or production radio behavior. Those remain separate outstanding requirements and still require additional protocol design, deployed infrastructure, or physical-device validation.

## Mesh key-revocation verification — 2026-09-15

The renewed source audit identified device-key revocation as a genuine mesh lifecycle gap. It is now implemented in `services/mesh/node.go` and `services/mesh/routing.go`. A node can revoke only the exact Ed25519 public key already pinned for a device id; revocation immediately removes the peer route and advertised KEM session, rejects future signed beacons, and blocks unsigned legacy-beacon downgrade. `services/mesh/revocation_test.go` verifies normal admission, key-matched revocation, route withdrawal, signed re-entry rejection, unsigned downgrade rejection, and mismatched-key rejection. Race-enabled tests pass.

The implementation is **local trust revocation**. Network-wide distribution of revocation decisions still requires a separately authenticated device-management channel, and physical radio validation remains environment-dependent. Those requirements remain outstanding rather than being falsely marked complete.

## Mesh envelope versioning and MTU fragmentation — 2026-09-15

Two further source-level mesh gaps were closed in this pass, both covered by executable tests in `services/mesh`.

**Versioned envelope.** `Packet` now carries a `Version` field stamped with `EnvelopeVersion` (1). `UnmarshalPacket` rejects an envelope newer than this build supports, so a future incompatible format fails closed instead of being misparsed; version 0 is accepted for pre-versioning peers so a rolling upgrade does not partition the mesh. `Packet.Validate` also enforces route, identifier and fragment-metadata coherence before routing. Covered by `TestEnvelopeVersionGate` and `TestPacketValidateRejectsMalformed`.

**MTU fragmentation and reassembly.** `fragment.go` splits a payload larger than `DefaultMaxPayload` (512 bytes) into fixed-size fragments, each encrypted with its own AEAD nonce and carried as an ordinary packet, so routing, TTL, hop counting, duplicate suppression, relay quotas and store-and-forward apply unchanged. The receiver reassembles under bounded memory (256 groups / 8 MiB) and time (5-minute expiry), ignores duplicate fragments, drops contradictory or out-of-range metadata, and verifies a sender-committed SHA-256 digest before delivery. A payload that fits one datagram keeps its original single-packet wire shape, preserving interoperability with pre-fragmentation peers. `Node.Send`/`Node.SendGroup` fragment automatically above the ceiling; `Node.OpenAssemblies` reports in-flight reassembly.

Covered by ten tests in `services/mesh/fragment_test.go`, including an out-of-order multi-fragment round trip through the production node API across a three-node chain asserting exactly-once application delivery, plus duplicate handling, digest-mismatch rejection, contradictory-metadata rejection, bounded-memory refusal and incomplete-group expiry.

**Latent data race fixed.** Race-enabled testing (which CI does not run) exposed a pre-existing race in `UDPTransport`: the receive goroutine read the inbound callback unlocked while `SetInbound` wrote it under the mutex. The reader now loads the callback under the lock and invokes it outside it. The full `services/mesh` suite passes under `go test -race`.

**Still open (narrowed 2026-09-15).** Congestion control, route repair, multipath *selection*, group sender-key rotation, network-wide distribution of revocation decisions, and the 3/10/50/100/500-device hardware experiments remain outstanding. Fragmentation makes a large payload transportable across a small-MTU link; it does not create bandwidth and is not evidence of 500-device capacity. Physical Bluetooth/Wi-Fi Direct validation still requires real devices. **ACK/retry with exponential backoff, user-visible delivery states (`queued`/`relaying`/`acked`/`expired`/`dead_letter`), priority traffic classes, and native Android/iOS adoption of the session-key envelope (signed beacons, per-peer session keys, local revocation, replay protection) are now implemented** — see the 2026-09-15 reliability pass at the end of this file.

---

## 2026-09-15 pass — mesh reliable delivery (ACK / retry / delivery states / priority classes)

A fresh reset of `main` re-audited every root specification file against the executable
source. The ACK/retry/delivery-state/priority-class cluster this file lists as outstanding
was confirmed genuinely absent (a `grep` for `ack`, `retry`, `dead.letter` and `priority`
across `services/mesh` returned no implementation) and is now implemented in first-party
Go, standard library only.

- **Acknowledgements and bounded retries** — `services/mesh/reliability.go` adds a
  sender-side transfer state machine with exponential backoff capped at 60 s and a
  5-attempt budget. Each attempt is a fresh packet with a fresh id and a fresh AEAD nonce,
  so a retry is never suppressed by intermediate-node duplicate suppression and no two
  transmissions of one transfer reuse a (key, nonce) pair. The transfer id travels in a new
  `xfer` envelope field.
- **Exactly-once application delivery** — the receiver collapses retried copies to a single
  delivery while acknowledging *every* copy, so a lost acknowledgement converges instead of
  stalling the sender.
- **Alternate-path retry** — a retransmission leads with a different path: the neighbour
  that failed to produce an acknowledgement is deferred to the end of the candidate list
  while the multi-path fan-out is preserved. This pass also found and fixed a defect that
  clobbered the recorded hop before the retry could consult it, silently disabling the
  deferral.
- **User-visible delivery states** — `queued` → `relaying` → `acked`, with terminal
  `expired` (payload lifetime exhausted) and `dead_letter` (retry budget exhausted) reported
  distinctly rather than conflated.
- **Priority traffic classes** — `services/mesh/priority.go` and `pfifo.go` add a forwarding
  buffer drained control → text → voice → media, FIFO inside a class, bounded by packet
  count *and* real bytes, evicting lowest-priority-first under pressure and never displacing
  control traffic. The previous count-bounded single-class queue could let bulk media crowd
  out call signalling and acknowledgements.
- **Tests** — `services/mesh/reliability_test.go` (18 new tests) covers the state machine,
  backoff, expiry versus dead-letter, exactly-once delivery, bounded retention, the byte
  bound, eviction ordering and backpressure refusal, plus an end-to-end acknowledgement round
  trip between two real nodes over sockets and an alternate-path retry assertion.
  `services/mesh` passes `go build`, `go vet`, `gofmt` and its full suite under `go test -race`,
  and 20 consecutive non-race runs (the suite had been failing intermittently — which is how
  the clobbering defect surfaced).

**Still absent, and not claimed:** congestion control, route repair, multipath selection
(the buffer fans out; it does not choose), group sender-key rotation, and group
acknowledgements — group payloads remain best-effort because a group ACK needs per-member
keys and per-member group key management. Radio handshakes still require real devices.

**Validation:** 21/21 Python E2E suites pass with zero failures against a live API on a fresh
PostgreSQL 15.19 with all 39 migrations (214 tables), with the Go SFU and all five strict
C++17 data planes running; route parity 153 files / 547 routes; feature registry 26 features
across 7 required clients.