# ChatApp implementation and dependency gap register

Assessment date: 2026-09-14. This register separates code that can be written and tested in the repository from capabilities that require devices, providers, operators, licensing or a user network.

## Direct source-scan findings

### P0 correctness

- The Go mesh and native mobile mesh use incompatible AEAD constructions and native envelopes previously omitted the nonce. Cross-platform offline delivery is therefore unproven and may fail closed at decryption.
- Native Android/iOS forwarding previously decremented TTL but did not enqueue the packet before flushing, so a phone acting as a relay could drop traffic.
- The iOS mesh source uses `@Volatile`, which is not a Swift language feature; an Xcode build is required after replacing it with a real synchronisation strategy.
- iOS AES-GCM must preserve the authentication tag; ciphertext-only storage is not a valid sealed message.
- The mobile projects have build configuration but no checked-in Gradle wrapper or generated Xcode project. CI cannot prove signed Android/iOS release builds from this repository alone.

### P1 reliability and scale

- Mesh routing has finite TTL, queue size and packet lifetime. It has no complete MTU fragmentation/reassembly, ACK/retry window, route repair, congestion control, relay fairness, multipath selection or 500-device hardware test.
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
