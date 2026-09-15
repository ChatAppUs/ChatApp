# ChatApp deep code-and-documentation audit

## Current implementation-status notice (2026-09-15)

This specification is a requirements source, not proof that a feature is production-complete. The current source-backed status is maintained in `IMPLEMENTATION_STATUS.md` and the audit in `ChatApp_Deep_Code_and_Documentation_Audit.md`.

| Status | Meaning |
|---|---|
| **Implemented** | Corresponding source, route/schema/client surface, and repository validation exist. Runtime or production evidence may still be required. |
| **Partial** | A source implementation exists, but an explicitly documented requirement, client, provider, or reliability guarantee is incomplete. |
| **Outstanding** | The capability requires external devices, providers, operators, licensing, production infrastructure, or additional source work. |

The current checkout passes the parity and feature-registry checks. Go, C++, PostgreSQL-backed integration, native Android/iOS, and production deployment gates are environment-dependent and must not be inferred from static source inspection.


**Audit date:** 2026-09-14 UTC
**Repository:** `ChatAppUs/ChatApp`  
**Scope:** every root Markdown file, the tracked application/service source, build manifests, migrations, and executable validation scripts. Documentation was treated as a requirements record, not as proof that a feature works. The source tree and validation results are the authority.

## Executive verdict

ChatApp is a broad, unusually ambitious social and communications platform. It is not yet a production-scale substitute for Facebook, TikTok, X/Twitter, Telegram or imo. It can compete today as a feature-rich product prototype and as a differentiated privacy/offline-messaging project; it cannot honestly compete on their strongest axes until the remaining correctness, native-device, recommendation, trust-and-safety, operations and scale gates are closed.

| Competitor | Can ChatApp compete today? | Reason |
|---|---:|---|
| Facebook | **No** | ChatApp has profiles, feed, stories, groups, pages, events, marketplace, live, ads and analytics in source, but not Facebook's mature identity graph, ranking, business tooling, moderation operation, creator ecosystem or global reliability. |
| TikTok | **No** | ChatApp has FYP/reels/live/creator/commerce surfaces, but not TikTok-grade capture, editing, effects, music rights, ranking, LIVE tooling, safety automation or creator analytics. |
| X/Twitter | **No** | ChatApp has public posts, threads, topics, trends and Pulse, but not X-grade search/trends, quote/repost culture, Spaces, long-form publishing, API ecosystem or mature creator subscriptions/rewards. |
| Telegram | **No** | ChatApp has messaging, groups, channels, bots, calls and a mesh foundation, but not Telegram's proven cloud sync, massive group/channel delivery, bot/mini-app platform, topics, stories, multi-device reliability or global operations. |
| imo | **Not yet** | imo is the closest communications comparison. ChatApp is broader, but imo is ahead in low-bandwidth calling, contact onboarding, mobile reliability, group calling and deployed weak-network behaviour. |

These are capability comparisons, not claims that a source-level route or UI is equivalent to a mature production system.

## Root Markdown files reviewed one by one

| File | What it specifies | Audit result |
|---|---|---|
| `README.md` | Product overview, development commands and validation claims | Broad surface inventory is supported by the route/registry checks. "Production complete" claims must remain qualified by the gates below. |
| `Anonymous.md` | Anonymous identity, privacy, calls, mesh and social requirements | The privacy goals are substantially larger than the current native and operations proof. Bounded TTL and finite queues are the correct implementation model; infinite range is not physically implementable. |
| `ChatApp_Complete_Features_and_Architecture_Master_Plan.md` | Feature registry, architecture and priorities | The registry is useful for coverage, but a registered route is not a user-tested feature. Native release and production-scale evidence remain separate gates. |
| `ChatApp_Complete_Master_Documentation.md` | Detailed feature trees, APIs, clients and operational design | The source has many corresponding handlers and screens. Several "complete" sections describe target architecture rather than demonstrated production behaviour. |
| `Identity-Authentication-and-Account-Security.md` | Authentication, account recovery, deletion and security invariants | The Go API tests and security migrations cover many invariants. Independent review, mobile security testing, key rotation and deployed incident controls are still required. |
| `IMPLEMENTATION_STATUS.md` | Historical status and validation record | It contains useful audit history. The current pass supersedes older claims where current source, toolchain or runtime evidence differs. |
| `COMPETITIVE_GAP_ANALYSIS.md` | Executive competitor and mesh gap analysis | Consistent with this audit: broad feature coverage is not production parity. |
| `ChatApp_Competitive_Comparison.md` | Capability-by-capability Facebook/TikTok/X/Telegram/imo comparison | Correct conclusion: no competitor can be matched today; P0 correctness and P1 product depth are the right order. |
| `ChatApp_Competitor_Comparison.md` | Short competitor decision and gates | Correct as an executive summary; this file should not be read as a release certification. |
| `ChatApp_Implementation_and_Dependency_Gap_Register.md` | Direct source findings and external dependency map | Correctly separates code-fillable gaps from dependencies that software alone cannot remove. |
| `ChatApp_Offline_Mesh_Analysis.md` | Five-kilometre, 500-device and live-media feasibility | Correctly rejects an unbounded/infinite-distance promise and identifies live-call limitations. |
| `ChatApp_Offline_Mesh_500_Device_Analysis.md` | Capacity model and acceptance criteria | Correctly requires simulation and physical-device evidence before advertising the mesh at that scale. |

## Code findings fixed in this pass

### 1. Cross-platform mesh cryptography was not interoperable

The Go implementation used NaCl secretbox with a 24-byte nonce while Android and iOS used AES-GCM with a 12-byte nonce. Native packet decoders also discarded the nonce, and iOS discarded the GCM authentication tag. That meant a packet produced by one client could not reliably be opened by another client.

The mesh wire contract is now canonical AES-256-GCM:

- Go uses the standard-library `crypto/aes` and `crypto/cipher` packages.
- Android and iOS carry the 12-byte nonce in the JSON envelope.
- iOS carries `ciphertext || tag` and reconstructs the CryptoKit sealed box correctly.
- `group_id` is preserved for group packets.
- An interoperability test covers encryption/decryption, nonce encoding, group identity and envelope round-tripping.

This is a protocol change. Existing deployed packets from the old format need an explicit migration/versioning plan before a rolling production deployment.

### 2. Native forwarding dropped packets

On Android and iOS, receiving a packet for another destination decremented TTL and incremented hops but did not enqueue the forwarded packet. Forwarding now re-enters the store-and-forward queue. Locally originated packet IDs are added to the deduplication cache, preventing simple relay loops.

### 3. The mesh module no longer needs an external crypto module

After moving to the Go standard library, `services/mesh/go.mod` no longer requires `golang.org/x/crypto` or its transitive `x/sys` dependency. This reduces supply-chain surface and makes the small mesh module easier to build offline.

### 4. Group metadata is retained on native packets

Android and iOS now encode/decode the optional `group_id` field rather than silently turning group traffic into an unscoped packet.

### 5. Relay admission and nil-transport safety

Beacon processing now honours the advertised `relay` versus `member` role, so a relay can forward without hidden test-only policy mutation. Node startup, transport selection and queue flushing now fail closed when a transport is absent instead of dereferencing a nil transport.

### 6. Signed device identity, route scoring and route expiry (this pass)

Three more routing findings from this audit are now implemented in the Go engine, using only the standard library:

- **Signed beacons.** Every Go mesh node holds an Ed25519 signing key (`signing.go`) and signs its discovery beacons. Receiving nodes verify the signature and pin the first public key seen per device id (trust-on-first-use); a later beacon claiming a known device id with a different key is rejected (`ErrKeyMismatch`), which blocks impersonation of already-known peers without any central authority. Signed and legacy unsigned beacons coexist on the wire.
- **Route quality scoring.** `Neighbor.Score` ranks candidates by relay consent, then link throughput (`wifi_direct` > `local_wifi` > `bluetooth`, per the Anonymous.md §5.3 order), then beacon freshness; `flush` orders neighbours by score and `RouteTable.BestRelay` exposes the best relay for a destination. The packet's final destination is always tried even without relay consent.
- **Route expiry.** `RouteTable.Expire` drops neighbours not seen within three minutes, so traffic stops flowing to devices that left range or powered down; the beacon loop prunes every cycle.

New tests cover score ordering, expiry, best-relay selection, signed-beacon round trips, tamper rejection and key-pinning mismatch rejection. All mesh tests, vet and formatting pass under Go 1.25.1.

### 7. Per-peer session keys, replay protection and duplicate delivery (this pass)

Using only the Go standard library (`crypto/ecdh`, `crypto/hkdf`, `crypto/aes`):

- **Per-peer session keys.** Each node holds an X25519 key-agreement pair whose public half is advertised inside the signed beacon (authenticated by the Ed25519 signature, so a man-in-the-middle cannot substitute it). Unicast payloads are encrypted with an AES-256 key derived via HKDF-SHA256 from the X25519 shared secret, replacing the pre-shared payload key for the hardened path. Senders fall back to the pre-shared key for peers without an advertised key (legacy/native interop), and receivers try session first, then pre-shared, so both paths coexist on the wire. A third party holding only the pre-shared key cannot open session-key payloads (tested).
- **Key rotation.** `RotateSessions()` regenerates the X25519 pair and bumps an epoch advertised in beacons; peers re-derive on the next beacon and traffic degrades gracefully to the pre-shared key in between (no lost packets).
- **Replay protection.** Senders stamp packets with a monotonic per-sender sequence number; receivers keep an IPsec-style 1024-slot sliding bitmap window per source and reject duplicates and sequences below the window base — closing the hole where a captured packet re-injected after dedup-cache expiry would be accepted. The check runs after successful decryption so forged packets cannot flush the window.
- **Duplicate local delivery fixed.** A destination reachable by paths of different lengths had its handler invoked once per TTL-improved copy; delivery now happens exactly once per packet id (regression-tested on a two-path topology; this also removes the grid-flood deadlock).

New tests cover session-key symmetry, rotation invalidation, authenticated exchange (pre-shared key cannot decrypt), replay filtering and inbound replay rejection. The full mesh suite, vet and formatting pass under Go 1.27.1; `services/api` and `services/sfu` Go tests also pass.

Native adoption note: the Android/iOS mesh engines now adopt the same hardened path — `MeshIdentity.kt` (Android) and `MeshIdentity.swift` (iOS) implement Ed25519-signed beacons advertising an X25519 key-agreement key, per-peer AES-256 session keys derived via ECDH + HKDF-SHA256 (`chatapp-mesh-session-v2`), trust-on-first-use key pinning, local key revocation, and per-source sliding-window replay protection. The native engines fall back to the pre-shared key for peers that have not advertised a KEM key, matching the Go engine's `sessionKeyFor`/`decryptPayload` behaviour, so legacy and native traffic coexist on the wire. The Android logic is unit-tested on the JVM (20/20 checks: session-key symmetry, rotation invalidation, signed-beacon verify/pinning, tamper and forgery rejection, revocation, replay filtering, and an end-to-end session-key-sealed delivery where the pre-shared key cannot open the payload).

Also in this pass, using the same engine: honest call feasibility (`CallFeasible` reports `direct`/`multihop`/`unreachable` from live route state so clients degrade to voice notes instead of pretending the mesh sustains a live call), per-source relay token-bucket quotas (burst 200, refill 50/s) for abuse prevention, and a deterministic simulation harness (`SimBus`, chain/partition-heal/100-node-grid tests plus a `cmd/meshsim` 500/5,000/50,000-node sweep CLI) that drives the production node code over explicit adjacencies.

## What is still not implemented or not proven

These are not hidden by the code fixes above:

1. **Real-time offline voice/video.** The mesh can carry control packets and delayed payloads, but a 500-device intermittent radio chain is not a real-time media network. Live voice/video needs codec adaptation, congestion control, jitter buffering, loss recovery, relay scheduling, call membership churn handling and a hard fallback to voice notes or delayed messaging.
2. **Automatic physical-radio mesh formation.** Android Wi-Fi Direct/Bluetooth and iOS local-Wi-Fi/BLE code still require physical-device testing, permission handling, background execution, OS power-policy validation and automatic peer/route formation tests. iOS does not expose Android-style generic Wi-Fi Direct APIs.
3. **Remaining device-identity hardening.** Beacons are Ed25519-signed with trust-on-first-use key pinning, which removes beacon spoofing for known peers. Per-peer session keys, replay protection, rotation and local key revocation are now implemented in the Go engine and adopted in the Android/iOS native engines (see §7 below); still open for a hostile multi-hop network: privacy-preserving stable identifiers and network-wide distribution of revocation decisions (local revocation is implemented; distributing it requires a separately authenticated device-management channel).
4. **Scale evidence.** The finite hop budget, queue size, radio bandwidth, battery, OS limits and topology determine capacity. "500 devices" and "infinite distance" are not source-code features. Deterministic simulation now covers chains, partitions, healing and a 100-device grid delivery (§7); the 5,000- and 50,000-node sweeps and physical field measurements remain.
5. **Provider-backed ML.** The API supports a configured translation/ML provider and a bounded local phrasebook fallback. Arbitrary-language, high-quality translation still requires a configured model/provider and compute; the fallback is not a substitute for TikTok/Facebook-grade ML.
6. **Recommendation and creator depth.** FYP/ranking, search, trends, effects, music licensing, creator analytics, subscriptions, rewards, commerce fraud controls and ad optimisation are not equivalent to mature competitor systems merely because route names exist.
7. **Trust and safety operations.** Automated moderation, human review, appeals, child safety, spam/fraud/coordination detection, legal-request workflows, transparency reports and regional policy operations remain deployment work.
8. **Production operations.** Android/iOS release builds and signing, physical-device tests, APNs/FCM delivery, multi-region failover, backup/restore drills, disaster recovery, SLOs, observability, penetration testing and incident response remain release gates.

## Dependencies: what coding can replace and what it cannot

### Reduced by code in this pass

- Mesh cryptography dependency: Go standard library AES-GCM replaced the mesh module's `x/crypto` dependency.
- Native envelope mismatch: nonce, tag and group metadata are now represented consistently.
- Basic native forwarding correctness: queue re-entry and source deduplication are now implemented.
- Go relay admission now honours beacon role, and nil transports fail closed during startup, selection and flushing.
- Signed device identity: standard-library Ed25519-signed beacons with per-device key pinning replace blind trust in advertised device ids.
- Route selection: deterministic scoring (relay consent, link throughput, freshness) plus neighbour expiry replaced first-match forwarding.

### Can be reduced with more first-party code

- Implement per-peer session key agreement, key rotation/revocation and a versioned packet envelope on top of the now-signed beacon identity.
- Add a simulator for 500/5,000/50,000 nodes with partitions, churn, queue pressure and packet loss.
- Add codec-aware voice-note transfer with resumable chunks, checksums and delayed delivery.
- Add local search/ranking baselines, moderation rules, audit logs and an operator review queue.
- Add self-hosted object storage adapters, local metrics, structured audit events and deterministic backup verification.

### Cannot be eliminated by application coding alone

- PostgreSQL/durable storage, network reachability, TURN/STUN and public DNS/TLS.
- APNs/FCM delivery and app-store signing/distribution.
- Email/SMS/OTP delivery and carrier reachability.
- Payment rails, tax, KYC, payouts and regional legal compliance.
- Moderation/translation model weights and the compute needed to run them at scale.
- CDN/edge capacity, multi-region hardware, incident response and independent audits.

The correct goal is to isolate these behind replaceable interfaces, provide local/self-hosted implementations where practical, and never claim that an adapter has removed the external dependency.

## Validation performed for this pass

Passed in the checkout:

- Go 1.25.1 tests and `go vet` for `services/api`, `services/mesh` and `services/sfu`.
- Web and admin Next.js production builds.
- Web TypeScript checking.
- Python ML bytecode compilation.
- Feature-registry validation and cross-platform route parity.
- Extension JavaScript syntax checks and backup-script syntax checks.
- Database migration ordering/non-empty validation.
- `git diff --check`.

Not runnable in this Linux checkout:

- Xcode/iOS physical-device build and radio tests.
- Android SDK/Gradle physical-device build and radio tests.
- Rust service tests where the Rust toolchain is not installed.
- PostgreSQL-backed integration suites requiring a live database/service stack.

## Release decision

Do not market ChatApp as a Facebook/TikTok/X/Telegram/imo replacement yet. Ship the current code as a broad beta only after the documented P0 protocol migration and native build checks pass. The next honest milestone is **cross-platform mesh interoperability on two Android devices, two iPhones and one Go node**, followed by a simulated 500-node test and a measured low-bandwidth voice-note fallback—not an infinite-distance live-video promise.

## References for competitor capability claims

[^1]: https://en-gb.facebook.com/business/help/1034727950288693  
[^2]: https://support.tiktok.com/en/using-tiktok/creating-videos/creator-tools-on-tiktok  
[^3]: https://help.x.com/en/using-x/subscriptions-creator  
[^4]: https://telegram.org/blog/calls-and-bots  
[^5]: https://imo.im/en/faq/What-are-the-features-of-the-chat-function-in-the-imo-app

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