# Offline mesh analysis: 5+ km, 500+ devices and unbounded distance

Assessment date: 2026-09-14. This is a systems analysis of the source code and physics, not a claim that a field trial has passed.

## Short answer

### Five kilometres and 500 devices

**Text and delayed voice notes are a plausible engineering target, but they are not currently proven by this repository.** The devices must form a connected, sufficiently dense radio graph; every handset must have the required radio permission and remain awake enough to relay; and the application must implement a reliable packet protocol. A line of 500 random devices is not automatically a route. Bluetooth/Wi-Fi range, buildings, terrain, interference, antenna orientation, operating-system background limits, battery and user consent determine whether a path exists.

**Live audio/video calls across a no-Internet mesh are not implemented or realistically equivalent to Internet calls.** The current call path uses WebRTC/SFU/TURN. The mesh packet engine carries message-like encrypted packets and call signalling concepts, but it does not provide a measured low-latency audio/video media plane over a multi-hop 500-device radio chain.

### Infinite distance

**No finite radio network can guarantee communication to infinite distance.** More devices can extend a connected route only while there is a continuous path, enough spectrum, power, storage, forwarding consent, route capacity and a bounded hop budget. ChatApp source uses finite TTL/hop limits, queue limits and packet age limits. “Infinity” would require an unbounded population, time, energy and storage and is not a meaningful availability guarantee.

## What the source currently provides

- Go mesh routing with encrypted packet envelopes, duplicate suppression, TTL/hops and store-and-forward queueing.
- Native Android and iOS mesh engines with the same broad packet model and radio adapters for local Wi-Fi/Wi-Fi Direct/Bluetooth or CoreBluetooth, subject to platform permissions and device APIs.
- A finite default hop budget, bounded queue and packet lifetime; these are safety controls, not infinite routing.
- Internet calls through the Go SFU and TURN/WebRTC path; this is a different plane from the offline mesh.

The source scan found protocol and release risks that must be closed before claiming cross-platform offline delivery: the Go and native clients used different AEAD constructions; native packet codecs omitted the nonce; iOS did not preserve the AES-GCM authentication tag; native forwarding paths needed to enqueue decremented packets; iOS source used a non-Swift `@Volatile` annotation; and no Android/iOS hardware CI or 500-device test exists. These are P0/P1 release blockers, not documentation details.

## Required packet architecture

1. **Versioned envelope:** version, packet id, source/destination or group id, kind, TTL, hop count, creation/expiry, fragment id/count, payload length, nonce and authentication tag.
2. **Interoperable cryptography:** a documented AEAD supported by Go, Android and iOS; authenticated key exchange; per-device identity keys; group sender-key rotation; device revocation; replay protection; no shared global key.
3. ~~**Fragmentation:**~~ **Implemented in source (2026-09-15, see the section at the end of this file).** Radio MTUs are much smaller than media files, so `services/mesh/fragment.go` now provides fragmentation and reassembly with bounded memory, SHA-256 integrity verification, missing-fragment expiry and duplicate handling. This makes a large payload transportable across a small-MTU link; it does not increase radio bandwidth and is not evidence of 500-device capacity, which still requires the physical experiments listed below.
4. **Reliability:** ACKs, bounded retries, duplicate IDs, delivery receipts, route repair, backoff, congestion control, priority classes and a dead-letter/expiry state.
5. **Routing:** neighbour expiry, link quality, route metrics, loop avoidance, relay consent, fairness, per-user quotas, multipath where useful, and protection from flooding/Sybil attacks.
6. **Media policy:** text first; compressed voice notes second; live voice only after latency/jitter/loss measurements; live video is last and likely needs local edge/SFU nodes rather than simple phone forwarding.
7. **Power/background:** explicit foreground relay mode, battery/thermal limits, charging-only relay option, OS background modes, user-visible consent and graceful departure.
8. **Discovery/security:** authenticated beacons, privacy-preserving rotating identifiers, no plaintext user identity or location leakage, pairing/invite flow and abuse reporting.

## Capacity reality

A five-kilometre line of devices has a serial bottleneck: traffic from the first region converges on the next relay and then the next. Adding users increases contention rather than multiplying capacity. A single route can become unusable from collisions, interference, queue growth or one missing relay. A real plan needs measured values for:

- effective throughput per radio and per hop;
- packet-loss and retry rate;
- one-way delay, jitter and route repair time;
- usable MTU and fragmentation overhead;
- simultaneous senders and group fan-out;
- battery and thermal drain while relaying;
- device density and gap probability;
- relay churn, sleep and OS termination;
- privacy leakage from metadata and discovery beacons.

For a 500-device trial, collect p50/p95/p99 results at 3, 10, 50, 100 and 500 devices, across indoor/outdoor, dense/sparse, moving/static, battery/charging and Android/iOS mixes. A successful unit test or route count cannot substitute for this trial.

## Feasibility matrix

| Capability | No Internet, connected mesh | Current status | Honest target |
|---|---:|---|---|
| Short text | Yes in principle | Protocol/interop and device tests incomplete | Ship after cross-platform fixtures and field trial |
| Small encrypted file/voice note | Possibly | Fragmentation, retry and capacity proof incomplete | Store-and-forward with strict size/expiry limits |
| Group text | Possibly | Group-key/routing/fan-out proof incomplete | Bounded groups and delivery receipts |
| One-to-one live voice | Very difficult | No offline media plane | Research local relay/edge media after text succeeds |
| Group live voice | Very difficult | No offline media plane or congestion control | Not a launch promise |
| Live video | Usually impractical on phone multi-hop | Not implemented offline | Internet/SFU or local edge nodes |
| Five-kilometre reach | Only with connected relays | No hardware evidence | Measure topology, not distance alone |
| Infinite reach | No | Physically and algorithmically impossible | Replace with finite hop/expiry/SLO guarantees |

## Dependency and deployment requirements

Coding alone cannot supply Bluetooth/Wi-Fi hardware, radio spectrum, battery, OS background privileges, signed mobile distribution, public safety support, model providers, TURN/CDN bandwidth or field operators. The repository needs hardware-in-the-loop labs, test phones, app signing, push credentials, telemetry, a relay abuse policy, support and an emergency rollback plan.

## Acceptance criteria before advertising offline mesh

- Go, Android and iOS known-answer encryption/codec fixtures pass in CI.
- Two-device, three-hop and route-repair tests pass with packet loss and duplicate injection.
- Real Android/iOS devices exchange text, fragments and voice notes while the Internet is disabled.
- A controlled 500-device experiment reports measured delivery, delay, battery, heat, loss and route survival.
- Offline live calls are either implemented and measured against declared limits or explicitly unavailable in the UI.
- Documentation says “finite store-and-forward mesh” rather than “infinite communication”.


## Deep source-and-documentation audit — 2026-09-14

The current code-level mesh fixes (signed beacons, route scoring, route expiry) and the remaining real-hardware/scale gates are recorded in `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`.


## Deep source-and-documentation audit — 2026-09-14

Cross-platform packet compatibility and native forwarding were corrected, but the 500-device claim still requires simulation and physical-device evidence. See `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`.

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

**Still open, unchanged.** ACK/retry windows, congestion control, route repair, multipath selection, group sender-key rotation, native Android/iOS adoption of the session-key envelope, and the 3/10/50/100/500-device hardware experiments remain outstanding. Fragmentation makes a large payload transportable across a small-MTU link; it does not create bandwidth and is not evidence of 500-device capacity. Physical Bluetooth/Wi-Fi Direct validation still requires real devices.
