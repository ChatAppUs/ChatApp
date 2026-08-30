# Self-Built Infrastructure — No Third-Party Kits

ChatApp deliberately owns its critical infrastructure instead of renting it.
This document covers the components that replace what competitors typically
outsource (Twilio for OTP, hosted WebRTC kits for calls, hardcoded asset lists
for wallets, and mixed admin/user surfaces).

## 1. Phone OTP engine (`services/api/otp.go`)

Replaces Twilio Verify entirely.

- 6-digit codes from `crypto/rand`; stored as salted SHA-256
  (`phone_verifications.salt`, migration 009), never in plaintext.
- 10-minute expiry, max 5 attempts, 60-second resend cooldown
  (`429 otp_throttled`), one-shot consumption.
- Delivery: real-time in-app push to the user's already-linked devices plus an
  SMS/email gateway hook for carriers. In development the code is surfaced as
  `dev_code`; in production it is never returned in the API response.
- Endpoints: `POST /api/auth/phone/send-code`, `POST /api/auth/phone/check-code`
  (rate-limited 5/min per IP at the edge).

## 2. SFU + STUN/TURN (`services/sfu`)

Replaces hosted WebRTC kits for everything beyond 1:1 mesh calls.

- Pion-based SFU: one `RTCPeerConnection` per participant, track forwarding
  between peers, polite-peer renegotiation when tracks are added/removed.
- Room modes: `meeting` (everyone publishes) and `live` (one broadcaster,
  everyone else receive-only subscribers).
- Embedded STUN/TURN server (UDP 3478) with RFC draft-uberti REST credentials:
  `username = expiry:uid`, `password = base64(hmac-sha1(TURN_SECRET, username))`.
- The API service mints short-lived HMAC-SHA256 tickets
  (`base64url(room|user|role|expiry).hex(hmac)`) after verifying conversation
  membership; the SFU validates tickets before any media setup. Forged or
  expired tickets are rejected with 401.
- API orchestration: `POST /api/calls/rooms`, `POST /api/calls/rooms/{id}/join`,
  `GET /api/live` (discovery). The API talks to the SFU over an internal
  bearer-authenticated control plane (`/internal/rooms`, `/internal/live`).
- Web client: `SfuCall` in `apps/web/src/lib/webrtc.ts`; pages
  `/meeting/[conversationId]` and `/live/[roomId]`. 1:1 calls still use the
  peer-to-peer `MeshCall` through our own STUN/TURN.
- Verified end-to-end by `services/sfu/main_test.go` (real SDP offer/answer
  through the signaling WebSocket, no mocks).

## 3. Admin plane separation

The user platform can never reach admin functionality.

- Separate login: `POST /api/admin/login` issues short-lived tokens with
  JWT claim `scope: "admin"` (no refresh token, audited as `admin.login`).
- `requireAdmin(roles...)` rejects any token whose scope is not `admin`
  (401 before role lookup); `requireAuth` rejects admin-scoped tokens on all
  user routes. Cross-plane tokens are dead on arrival.
- The admin console is a standalone Next.js app (`apps/admin`, port 3100) with
  its own token storage keys; the user app (`apps/web`) contains no admin
  routes, links, or code.
- RBAC roles: `superadmin`, `moderator`, `finance` via `admin_roles`.

## 4. Admin-managed platform tokens (wallet)

The built-in multichain wallet no longer ships a hardcoded asset list.

- Migration 009 creates `platform_tokens` (symbol, name, chain, contract
  address, decimals, native flag, enabled flag) seeded with 14 rows
  (BTC/ETH/USDT/USDC/BNB/SOL/XRP/ADA/DOGE/TRX + ERC-20/BEP-20/TRC-20 variants).
- Admin endpoints (`superadmin`/`finance` only):
  `GET/POST /api/admin/wallet/tokens`,
  `POST /api/admin/wallet/tokens/{id}/status`,
  `DELETE /api/admin/wallet/tokens/{id}` (409 while users hold accounts).
- `GET /api/wallet/assets` is served live from enabled `platform_tokens` rows;
  disabling a token hides it from all user wallets immediately.
- Every admin token mutation is written to the audit log.

## 5. Realtime relay (`services/realtime`, C++)

Replaces managed WebSocket/pub-sub infrastructure (Pusher/Ably-class).

- epoll-based RFC6455 fanout edge with a hand-rolled frame codec: one node
  holds 10k+ concurrent sockets with microsecond fanout, no GC pauses.
- Clients connect `GET /ws?token=<access JWT>`; the relay verifies the JWT
  itself (HS256, exp checked) and never trusts the network.
- The Go API stays the control plane: it persists, then publishes fanout
  events to the relay's loopback-only `/publish` port
  (`Authorization: Bearer CLUSTER_SECRET`). If no relay is configured the Go
  hub serves WebSocket directly — same wire contract either way.
- Wired into docker-compose (`REALTIME_RELAY_URL=http://realtime:8301`).

## 6. Transcode worker (`services/transcode`, C++)

Replaces hosted transcoding (Mux/Cloudflare Stream-class).

- ffmpeg-driven HLS ABR ladder (240p→1080p) + thumbnail per upload; jobs live
  in Postgres (`transcode_jobs`) and are claimed `FOR UPDATE SKIP LOCKED` via
  the API's internal control plane (`/internal/transcode/claim|complete`,
  CLUSTER_SECRET bearer).
- Finished jobs rewrite `post_media.url` to the HLS master playlist, so
  reel/story players switch to adaptive playback without a client release.
- Shares the media volume with `services/media` in docker-compose.
