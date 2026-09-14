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
3. **Fragmentation:** radio MTUs are much smaller than media files. Add fragmentation/reassembly, bounded memory, integrity checks, missing-fragment expiry and duplicate handling.
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

The current code-level mesh fixes and the remaining real-hardware/scale gates are recorded in `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`.


## Deep source-and-documentation audit — 2026-09-14

Cross-platform packet compatibility and native forwarding were corrected, but the 500-device claim still requires simulation and physical-device evidence. See `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`.
