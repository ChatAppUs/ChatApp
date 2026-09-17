# ChatApp offline mesh analysis: 5 km, 500+ users and “infinite” range

Assessment date: 2026-09-14. This is a source-level engineering assessment, not a field-test result.

## Direct answer

With mobile internet and Wi-Fi internet unavailable, the current ChatApp code **does not yet guarantee** that a first user can send text, voice, audio or video to a last user 5+ km away through 500+ phones. The repository contains a real mesh direction — packet envelopes, TTL, deduplication, store-and-forward, Go routing and Android/iOS radio classes — but the mobile protocol and radio implementations are not yet an interoperable, tested 500-device network.

- **Text:** possible only when every relay is awake, opted in, mutually compatible, discoverable, within radio range and connected into one route. It is not proven end to end.
- **Voice message:** can become feasible as delayed encrypted store-and-forward data, after chunking, resumable transfer, integrity checks, acknowledgements, quotas and queue persistence. A voice note is not a live call.
- **Live audio call:** **not implemented over the offline mesh**. Existing call signalling and WebRTC/SFU/TURN need an IP path and tight latency; a store-and-forward queue cannot replace that.
- **Live video call:** **not implemented over the offline mesh** and is not realistic across 500 Bluetooth relays without a dedicated, measured media-routing system.
- **Infinity length:** impossible as a product guarantee. Every route has finite radio range, hop budget, TTL, queue lifetime, battery, bandwidth and topology. More devices can extend a connected path; they do not create an infinite or always-available network.

## What the source code currently does

### Positive foundations

- Go mesh packets carry source, destination, kind, TTL, hop count, encrypted payload and nonce.
- Go routing has duplicate suppression, a bounded store-and-forward queue and a multi-hop test.
- Android and iOS define transport-agnostic engines and physical-link classes for Wi-Fi/local UDP and Bluetooth/BLE.
- The app has separate call signalling and an Internet WebRTC/SFU/TURN stack.

### Blocking correctness issues found by direct inspection

1. **Go/native cryptography is different.** Go uses NaCl secretbox/XChaCha20 with a 24-byte nonce; Android and iOS use AES-GCM with a 12-byte nonce. The native JSON encoders previously omitted the nonce entirely. Native packets therefore cannot be decrypted across clients. A protocol version and one shared AEAD/key agreement are mandatory.
2. **iOS authentication data is incomplete.** The iOS AES-GCM path previously returned ciphertext without the authentication tag and reconstructed a box with an empty tag. It cannot reliably decrypt even a native packet.
3. **Native forwarding drops packets.** Android and iOS decrement TTL and flush without enqueueing the forwarded packet. A native intermediary can receive a packet but not reliably relay it.
4. **Native peer discovery is not a 500-device graph.** Android discovery does not automatically create a connected route to every discovered device. Wi-Fi Direct group ownership, Bluetooth RFCOMM connections, permissions and reconnects need a real state machine. iOS does not expose general Wi-Fi Direct; it uses local Wi-Fi and CoreBluetooth instead.
5. **Addressing is incomplete.** Local Wi-Fi, Wi-Fi Direct and Bluetooth use different address spaces and packet framing. The route table needs canonical device IDs, signed beacons, expiry, transport-specific endpoint records and route scoring. **Status (2026-09-14, Go engine):** signed beacons (Ed25519, TOFU key pinning), neighbour expiry (3 min) and route scoring (relay consent, link throughput, freshness) are implemented and tested in `services/mesh`; the native clients still need the same treatment plus transport-specific endpoint records.
6. ~~**No MTU protocol exists.**~~ **Resolved in source (2026-09-15, see the section at the end of this file).** Voice and media frames exceed Bluetooth/Wi-Fi datagram limits, so `services/mesh/fragment.go` now splits a payload above the 512-byte ceiling into individually encrypted fragments and reassembles it with bounded memory (256 groups / 8 MiB), a 5-minute expiry, duplicate handling and a verified SHA-256 digest. Sequence numbers already existed (anti-replay). ~~**Still absent:** retransmission windows~~ **Resolved in source (2026-09-17):** `services/mesh/fragreliable.go` adds the missing ACK/retry stream — a bounded selective-repeat window (8 fragments in flight) with per-fragment acknowledgements sealed under the session key, exponential backoff, a per-fragment attempt budget (dead letter) and a transfer lifetime, plus a done-acknowledgement that retires the sender even when per-fragment acks are lost. The receiver delivers exactly once, only after the digest verifies. Covered by `fragreliable_test.go` (lossy convergence, exactly-once, digest gate, done-ack retirement, small-payload delegation).
7. ~~**No congestion or fairness protocol exists.**~~ **Implemented in `services/mesh` (congestion.go, pfifo.go, priority.go, relaylimit.go):** byte-based radio backpressure, a priority-ordered forwarding buffer that protects control and text from media bulk, per-source relay quotas, and predictable expiry drops.
8. ~~**No route repair or delivery contract exists.**~~ **Implemented in source:** reliability.go / node_reliable.go provide ACKs, bounded retries, dead-letter and expiry states; routerepair.go repairs broken routes; delivery state is user-visible (`Transfer`/`LargeTransfer` views).
9. **No 500-device test exists.** The repository has unit/static tests but no hardware-in-the-loop test across 3, 10, 50, 100 and 500 devices, no battery/thermal test, and no measured latency, loss, throughput or route survival.

## Why 5 km is not a simple distance calculation

A 5 km line is not one radio link. It is a chain of overlapping links. The usable distance depends on phone model, Bluetooth mode, Wi-Fi Direct/hotspot restrictions, antenna orientation, bodies and buildings, interference, channel selection, operating-system background limits, battery-saving modes and whether each user consents to relay. A sparse gap of one missing relay breaks the route. A dense group may still collapse under contention.

A 500-person line also has a topology problem: a single chain has poor redundancy and every intermediate node becomes a bottleneck. A real design needs a mesh graph, route diversity, relay admission, bounded fan-out, controlled beaconing and a way to avoid broadcast storms.

## Offline live-call requirements not present

**Status update (2026-09-17):** the engine-side media plane now exists in `services/mesh/livecall.go` and is covered by `livecall_test.go`: call signalling rides the existing `CallSignal` path; media is framed as compact binary headers (sequence, timestamp, bitrate class) plus one codec frame; a jitter buffer reorders, adapts its playout delay and conceals late frames; XOR forward-error correction reconstructs a single lost frame per group; the bitrate class adapts to measured loss; media frames are sealed under a per-call key derived from the per-peer session key with epoch rotation; the route is re-checked every maintenance tick and a call whose route dies ends with an explicit fallback instead of pretending to continue. What remains open is exactly what source cannot supply: device Opus/AMR integration, and the hardware measurements below.

To support even one-to-one live audio across a disconnected mesh, ChatApp would need:

- Opus or another low-bitrate codec with packetisation that fits the selected MTU.
- Sequence numbers, timestamps, jitter buffers, loss concealment, forward-error correction and adaptive bitrate.
- A real-time route with a latency budget; store-and-forward cannot meet it.
- Congestion control and relay scheduling that protect call packets from chat/media traffic.
- NAT/radio path negotiation, key rotation and participant authentication without the cloud.
- Route repair and handoff when any relay leaves.
- Hardware measurements for latency, packet loss, jitter, battery and thermal load.

Video adds orders of magnitude more bandwidth and makes a 500-person multi-hop call a specialised research project. A practical product should ship offline text and voice notes first, then consider small local audio rooms only after measurement; it should not promise offline video across arbitrary phones.

## Safe product promise

The defensible promise is: **encrypted, store-and-forward text and voice notes over a tested, finite, user-consented local mesh when a connected path exists**. It is not: “works to infinity”, “works through 500 phones automatically”, or “supports live video without internet”.

## Required implementation/test plan

1. Freeze a versioned cross-platform envelope and AEAD fixture set.
2. Add Go, Android/JVM and iOS codec/crypto interoperability tests using the same known packets.
3. Implement route beacons, expiry, scoring, ACK/retry windows, fragmentation/reassembly and backpressure.
4. ~~**Implement automatic radio permission/discovery/reconnect state machines and explicit relay consent.**~~ **Implemented in source (2026-09-17):** relay consent is explicit (`relayOk` on both engines, honoured in candidate selection); Android and iOS engines prune neighbours after the Go engine's 3-minute beacon expiry and score forwarding candidates by relay consent and freshness; Android's Bluetooth link now consumes `ACTION_FOUND` results and reconnects discovered peers with per-peer exponential backoff (5 s → 60 s), and both platforms run a supervision loop that restarts links when radios or permissions come back, stops them when they go, and reports per-link health in the status surface.
5. ~~**Add hardware-in-the-loop tests at 3/10/50/100/500 devices**~~ — partially addressed: `TestSim500DeviceGridDelivery` exercises a 500-device grid in CI (corner-to-corner delivery, 137 ms), `cmd/meshsim` sweeps 500/5,000/50,000, and the 100-device lossy/repair suites run in CI. Physical p50/p95 latency, loss, throughput, battery, thermal and route-survival numbers still require device farms — this remains the honest boundary between simulated and measured.
6. Build an offline voice-note transfer protocol before any live-media work.
7. Keep Internet WebRTC/SFU/TURN and offline mesh media as separate transports with honest capability negotiation.
