# C++ Conversion Plan — Ultra-Low-Latency Hot Paths

Date: 2026-08-29; updated 2026-08-30. Purpose: identify which code files/paths
must run in C++ for super-fast, ultra-low-latency service at global scale, and
which stay in Go/Rust/Python.

## Principle

C++ earns its cost on paths where **per-request microseconds and GC pauses
matter**: media bytes, realtime fanout to hundreds of thousands of sockets,
media packet forwarding. Business logic does not belong in C++ — its
maintenance cost would slow the whole platform down.

C++ footprint (all shipped, `-O3 -std=c++17`, zero dependencies, clean
`-Wall -Wextra` builds):
- `services/media/main.cpp` — the media edge: raw-socket HTTP, sendfile-style
  streaming with Range support, 2 GiB uploads, signed-upload enforcement.
- `services/realtime/main.cpp` — epoll WebSocket fanout edge: RFC6455
  server with manual frame codec, JWT-verified `/ws` (10k+ connections),
  HMAC-guarded `/publish` control port fed by the Go API (`relay.go`).
- `services/transcode/main.cpp` — ffmpeg HLS ladder worker: polls the API's
  internal claim endpoint (SKIP LOCKED jobs), renders 240p→1080p renditions +
  thumbnail into the media volume, reports the ladder back.
- `services/counters/main.cpp` — trending/counters engine: hot in-memory hash
  tables for hashtag views, post view deltas, and live-room viewer peaks,
  drained by the Go `/internal/counters/flush` loop.

## Conversion status

| # | Source (today) | Target | Status |
|---|----------------|--------|--------|
| 1 | WebSocket hub + fanout inside `services/api/handlers_chat.go` (`Hub`, `wsClient`, `sendTo`) | `services/realtime/` C++ relay | ✅ **Shipped** — Go stays the control plane (persists, then publishes to the relay over `REALTIME_RELAY_URL`); C++ owns the socket edge. The Go hub remains as the no-relay fallback for single-process dev. |
| 2 | Presence/typing fanout (same file, `handleWS` broadcast paths) | Same `services/realtime` relay | ✅ **Shipped** — same publish path; presence/typing never queue behind persistence. |
| 3 | `services/sfu/` (shipped in Go/Pion: signaling + embedded STUN/TURN + HMAC room tickets) | Port the RTP/RTCP forwarding hot loop to C++ | 🚧 Roadmap (P1) — the Go/Pion SFU closes the meetings/group-call/live gap functionally. When a single room's packet rate saturates a core, the forwarding plane (RTP relay, NACK/PLI handling, simulcast layer selection) ports to a C++ io_uring forwarder while Go keeps signaling — same split as the media edge. |
| 4 | — | `services/transcode/` C++ worker (ffmpeg HLS ladder) | ✅ **Shipped** — closes the #1 media gap: HLS/ABR ladder, thumbnails; jobs queue in Postgres with SKIP LOCKED claiming via `/internal/transcode/claim` + `/complete`. |
| 5 | — | Trending/counter engine standalone `services/counters` | ✅ **Shipped** — hot in-memory counters (hashtag trending, post view deltas, live-room viewer peaks) flushed on a timer to Postgres; `POST /counters {hashtag,post_id?} /live-view` + `GET /trending` + `GET /flush` (Go API drains via `/internal/counters/flush` + FLUSH_SECRET). Rehash-only reallocation, zero-alloc overflow tags, nanosecond timers — same hot-path discipline as the epoll relay. The API routes trending through it when `COUNTERS_URL` is set; SQL upsert fallback keeps behavior identical when not. |

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

**Done: the hub/fanout core became `services/realtime`, and the transcoder is
`services/transcode` — both shipped, wired into docker-compose, and covered by
integration/feature suites.** Remaining C++ work: the SFU RTP forwarding plane
(P1, scale-triggered) and the counter engine (P2). Everything else is already
in the right language: C++ at the byte/packet/socket edge, Rust at the trust
boundary, Go for orchestration, Python for ML.
