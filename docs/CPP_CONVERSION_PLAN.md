# C++ Conversion Plan — Ultra-Low-Latency Hot Paths

Date: 2026-08-29. Purpose: identify which code files/paths must run in C++
for super-fast, ultra-low-latency service at global scale, and which stay
in Go/Rust/Python.

## Principle

C++ earns its cost on paths where **per-request microseconds and GC pauses
matter**: media bytes, realtime fanout to hundreds of thousands of sockets,
media packet forwarding. Business logic does not belong in C++ — its
maintenance cost would slow the whole platform down.

Current C++ footprint: `services/media/main.cpp` (317 LOC) — the media edge:
raw-socket HTTP, sendfile-style streaming with Range support, 2 GiB uploads,
signed-upload enforcement, zero dependencies.

## Conversions to C++: 2 existing paths + 3 new services

| # | Source (today) | Target | Why C++ | Priority |
|---|----------------|--------|---------|----------|
| 1 | WebSocket hub + fanout inside `services/api/handlers_chat.go` (`Hub`, `wsClient`, `sendTo`, ~120 LOC of that file) | New `services/realtime/` C++ relay (epoll/io_uring, one thread per core, shared-nothing) | Go's per-connection goroutines + GC stop-the-world pauses cap a node at low tens of thousands of sockets with tail-latency spikes. A C++ edge relay sustains 100k+ concurrent connections per node with microsecond fanout — this is the WhatsApp/Telegram-scale messaging path. The Go API stays the control plane: it persists the message, then publishes a fanout event to the relay over an internal socket. | P0 |
| 2 | Presence/typing fanout (same file, `handleWS` broadcast paths) | Same `services/realtime` relay | Presence and typing are the highest-frequency, lowest-value messages in the system — they must never queue behind real messages or pay GC. | P0 |
| 3 | `services/sfu/` (shipped in Go/Pion: signaling + embedded STUN/TURN + HMAC room tickets) | Port the RTP/RTCP forwarding hot loop to C++ | The Go/Pion SFU closes the meetings/group-call/live gap functionally. When a single room's packet rate saturates a core, the forwarding plane (RTP relay, NACK/PLI handling, simulcast layer selection) ports to a C++ io_uring forwarder while Go keeps signaling — same split as the media edge. | P1 |
| 4 | — (does not exist yet) | `services/transcode/` C++ worker (ffmpeg/x264/x265/SVT-AV1) | Closes the #1 media gap: HLS/ABR ladder generation, thumbnails, audio normalization for reels/stories. CPU-bound codec work is C++ territory; queue from Postgres with SKIP LOCKED like the scheduler. | P1 |
| 5 | — (does not exist yet) | Trending/counter engine inside `services/realtime` or standalone | Hashtag trending, reel view counts and live viewer counts are hot in-memory counters flushed periodically to Postgres; a C++ (or Redis) counter tier removes that write pressure from the API. | P2 |

## What must NOT be converted

- **REST API / business logic (Go)** — request latency there is dominated by
  Postgres round-trips (~0.1–1 ms), not language overhead; Go's p99 is
  already excellent and iteration speed matters more.
- **Auth/crypto/money (Rust per RUST_CONVERSION_PLAN.md)** — safety beats
  the last microsecond; HMAC/JWT/argon2 in Rust are within noise of C++.
- **ML ranking/moderation (Python)** — model tooling ecosystem; the scoring
  path is batch, not per-request. If `/rank` ever becomes a bottleneck, the
  scoring function alone ports to C++ behind the same HTTP contract.
- **Media edge (already C++)** — done; keep it dependency-free.

## Bottom line

**Convert 1 existing file's worth of code (the hub/fanout core of
`handlers_chat.go`) into a new C++ realtime relay, and build/port 3 C++
services (SFU forwarding plane, transcoder, counters).** Everything else is already in the
right language: C++ at the byte/packet/socket edge, Rust at the trust
boundary, Go for orchestration, Python for ML.
