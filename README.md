# ChatApp

A vertically-integrated social platform combining social feed, stories, reels,
real-time messaging, audio/video calls, meetings and live broadcasting, a
multi-chain crypto wallet with P2P payments, creator monetization, advertising,
and a full admin panel — web (Next.js PWA), desktop (Tauri), native mobile
(Android Kotlin/Compose, iOS SwiftUI), and browser-extension clients against a
polyglot microservice backend.

## Architecture

```
clients/
  apps/web        Next.js 14 + TypeScript PWA (8 locales, RTL support,
                  light/dark theme)
  apps/admin      Next.js 14 — standalone admin console (separate login plane,
                  never reachable from the user app, light/dark theme)
  apps/desktop    Tauri 2 shell (Rust) hosting the web app (Windows/macOS/Linux)
  apps/android    Native Android (Kotlin + Jetpack Compose, light/dark theme)
  apps/ios        Native iOS (SwiftUI, light/dark theme)
  apps/extension  Chrome/Firefox MV3 extension — popup quick-nav, unread
                  badge, full-app tab view, light/dark theme

services/
  api             Go 1.23 — core API: auth (incl. self-built OTP engine, TOTP
                  2FA, passkeys, OAuth), social graph, feed, chat control plane,
                  calls/live orchestration, wallet ledger, ads, KYC, admin
                  RBAC, push notifications, transcode job control plane
  sfu             Go 1.23 (Pion) — self-built SFU for meetings/group calls/live
                  broadcast + embedded STUN/TURN server
  security        Rust — request signing, media upload grants, HMAC tokens
  media           C++17 — media upload & streaming edge (HTTP range requests)
  realtime        C++17 — epoll WebSocket fanout edge (10k+ connections,
                  JWT-verified /ws, HMAC-guarded /publish control port)
  transcode       C++17 — ffmpeg HLS ABR ladder + thumbnail worker
                  (SKIP LOCKED job claiming via the API control plane)
  ml              Python — feed ranking, content moderation, ASR hooks

infra/
  db              PostgreSQL migrations 001–023 (pgcrypto, citext)
  docker-compose  Postgres, MongoDB, Redis, MinIO + all services
```

## Features

- **Accounts**: register/login by email, username, or phone (245 countries with
  dial codes and flags), argon2id password hashing, access+refresh token sessions,
  full password-reset flow, phone verification codes, **Google sign-in**
  (ID-token verified against Google's live JWKS), **passkeys/WebAuthn**
  (fingerprint, face, or device-passcode login — pure-stdlib CBOR/COSE
  verification, ES256/RS256/EdDSA, replay-resistant sign counters), and
  **Telegram-style QR login** (scan from a signed-in device to approve a new
  one, one-shot tokens).
- **Social**: posts, comments with @mentions (with notifications), likes,
  follows, user search, profiles, stories (24h expiry), reels with view
  tracking, ML-ranked feed with moderation hook.
- **Messaging**: realtime chat (1:1 and groups) over the C++ epoll WebSocket
  fanout edge, conversation deduping, member isolation, message persistence.
- **Calls**: WebRTC audio/video calls, group meetings and live broadcasting on
  the self-built SFU (`services/sfu`, Pion) with embedded STUN/TURN and HMAC
  time-limited credentials — no external WebRTC kit.
- **Wallet & finance plane**: multi-chain accounts (BTC, ETH, USDT, USDC, BNB,
  SOL, TRX, MATIC, LTC, DOGE), double-entry ledger, atomic P2P transfers gated
  on KYC verification, full transaction history. On top of that: deterministic
  per-user **deposit addresses** (HKDF-derived from `WALLET_MASTER_SEED`;
  bech32/base58check/keccak per chain) with QR + copy in every client;
  **signed withdrawals** — every request is risk-scored, HMAC-signed and, below
  `WITHDRAW_AUTO_THRESHOLD`, auto-approved in under a second (larger or risky
  requests queue for superadmin sign-off; rejection auto-refunds); escrowed
  **P2P marketplace** with 881 local payment methods across all 238 countries
  and dispute resolution; instant **convert** engine on admin-managed rates;
  per-token deposit/withdraw/P2P/convert switches; and superadmin-defined
  dynamic admin roles (create/delete role, grant/revoke) — all audit-logged.
- **KYC**: document submission, admin review queue, enforced before any
  transfer.
- **Ads**: campaign creation with country/locale targeting, budgets, creatives,
  funding from wallet balance, admin review workflow, targeted serving with
  impression/click event tracking.
- **Admin**: RBAC (superadmin/moderator/support/finance), platform stats, user
  search/suspend, report resolution, KYC review, ad review, audit log. The
  first registered account on a fresh deployment is bootstrapped as superadmin.
- **Cluster engine**: multi-node global expansion — node registry with
  heartbeats and dead-node reaping, least-loaded region routing for client
  bootstrap (`GET /api/cluster/route`), rendezvous (HRW) consistent-hash
  sharding for sticky conversation/media placement, weight + spare-capacity
  aware balancing, and superadmin fleet management (list/drain/remove nodes).
  Single-node deployments behave exactly as before.
- **Social parity batch 2**: repost toggle/unrepost with quote posts and
  embedded quoted previews, post editing, X-style threads, pinned messages
  with realtime fanout, message forwarding with attribution (E2EE messages
  excluded), Telegram-style Saved Messages self-chat, story engagement
  (deduped view tracking, author-only viewer list, emoji reactions, story
  replies as DMs), and a composer audience selector (public/followers/only me)
  enforced in every feed query.
- **Chat parity batch 3**: scheduled messages with a multi-node-safe delivery
  worker (SKIP LOCKED claiming), voice messages (MediaRecorder → media
  service → inline audio player), and per-conversation message drafts.
- **Comments & discovery parity**: comment likes with author notification,
  nested reply threads in the UI, share-post-to-DM with conversation picker,
  in-conversation message search, group creation UI, and a notifications
  center (`/notifications`) with mark-all-read.
- **Privacy & group parity**: disappearing messages per conversation
  (1m/1h/24h/7d timers, live WS removal, 30s sweeper), member list with
  roles, and add/remove member management in the chat UI.
- **TOTP 2FA**: authenticator-app setup with QR provisioning URI, login
  challenge (`totp_required` → retry with code), disable flow.
- **Creator & discovery parity (batch 4)**: watch-time ranked FYP
  (completion %, rewatches, not-interested), reels creation tools (text
  overlays, ASR captions, speed ramp), full group features (invite links,
  join requests, pinned messages), Facebook-style Pages (create/follow/posts),
  creator monetization (subscription tiers, tips, revenue dashboard),
  profile song with autoplay.
- **Platform parity (batch 5)**: Telegram-style Bot API (long-poll
  getUpdates + webhooks, sendMessage), push notifications (Web Push/VAPID
  self-built, FCM/APNs gateways optional), phone contact discovery (hashed),
  payment cards (tokenized storage, never PANs), URL unfurling.
- **Gap pack 5**: drop-in call rooms with persistent shareable links (SFU
  tickets, web `/room/[slug]`, create/share on Android/iOS), self-built KYC
  auto-verification pipeline in the ML service (doc metrics, liveness, face
  match, admin ML evidence), ads platform (campaigns, creatives, review,
  wallet-funded budgets, 55% creator rev-share from the platform treasury),
  optional Redis scaling cache on the FYP path (fail-open), AR video filters on
  web calls/meetings/rooms, pay-in-chat UI on web/Android/iOS.
- **Video pipeline**: C++ ffmpeg transcode worker producing HLS ABR ladders
  (240p→1080p) + thumbnails for reels/stories, claimed SKIP LOCKED from the
  API job control plane.
- **Clients**: web PWA, admin console, Tauri desktop shell, native Android
  (Compose), native iOS (SwiftUI), MV3 browser extension — every client has a
  persisted light/dark theme switch and talks to the same backend.
- **i18n**: English, Español, Français, Deutsch, Português, العربية (RTL),
  हिन्दी, 中文.

## Quick start (Docker)

```bash
cp .env.example .env   # set real secrets
docker compose up --build
# web: http://localhost:3000  api: http://localhost:8080
```

## Local development

```bash
# Database
psql -d chatapp -f infra/db/001_schema.sql

# Core API (Go)
cd services/api && DATABASE_URL=postgres://... go run .

# Security service (Rust)
cd services/security && cargo run --release

# Media edge (C++)
cd services/media && g++ -O2 -std=c++17 -o media main.cpp && ./media

# Realtime relay (C++) — WS fanout edge
cd services/realtime && g++ -O3 -std=c++17 -o realtime main.cpp && ./realtime

# Transcode worker (C++) — needs ffmpeg installed
cd services/transcode && g++ -O3 -std=c++17 -o transcode main.cpp && ./transcode

# ML service (Python)
cd services/ml && pip install -r requirements.txt && python main.py

# Web app (Next.js)
cd apps/web && npm install && npm run dev

# Admin console (Next.js, port 3100)
cd apps/admin && npm install && npm run dev

# Desktop (Tauri shell over the web app)
cd apps/desktop && npm install && npm run tauri dev

# Android / iOS — native projects
#   apps/android: open in Android Studio (or ./gradlew assembleDebug)
#   apps/ios:     xcodegen && open ChatApp.xcodeproj

# Browser extension (MV3): chrome://extensions → Developer mode →
# "Load unpacked" → apps/extension
```

## Testing

- Go: `cd services/api && go build ./... && go vet ./... && go test ./...`
  (plus `cd services/sfu && go test ./...`)
- Rust: `cd services/security && cargo test` (SHA-256/HMAC RFC test vectors)
- C++: `services/media`, `services/realtime`, `services/transcode` build with
  `-Wall -Wextra` clean
- Integration: `tests/integration_test.py` — 153 end-to-end checks, plus
  `tests/features_test.py` — 72 checks (watch signals, transcode jobs, groups,
  pages, monetization, bots, push, contacts, 2FA), plus
  `tests/finance_test.py` — 44 checks (deposit address derivation per chain,
  withdrawal auto-approval <1s + superadmin review, escrow/P2P lifecycle,
  disputes, convert rates, token feature switches, dynamic roles), plus
  `tests/gaps_test.py` … `tests/gaps7_test.py` — 92+ checks incl. quiz polls,
  bot API expansion, chunked uploads, search operators, word filters,
  E2E key + SAS verification, moments, audio-room recordings, related-reels
  embeddings — all passing against real PostgreSQL + Go API + Rust security +
  C++ media/realtime + SFU, no mocks.

## Security notes

- Passwords hashed with argon2id (PHC string format); sessions are random
  256-bit tokens, refresh tokens stored as SHA-256 hashes.
- Per-IP token-bucket rate limits on auth endpoints (login 15/min, register
  10/min, password reset 5/min, SMS 5/min, passkey/OAuth 60/min).
- Every authenticated request re-validates user status (suspension takes
  effect immediately).
- Wallet transfers run in a single DB transaction with balance checks; KYC
  enforced server-side. Withdrawals additionally require an HMAC signature
  over the full request (destination address included), are debit-held in the
  ledger from the moment they're requested, and no admin role can release
  funds outside the signed pipeline.
- Inter-service calls are HMAC-signed (Rust security service); media uploads
  require a 5-minute signed grant (`POST /api/media/upload-token`) verified by
  the C++ edge; downloads use unguessable 128-bit CSPRNG IDs.
- CORS + WebSocket Origin allowlist via `ALLOWED_ORIGINS` (CSWSH protection);
  production refuses to boot with a weak `JWT_SECRET`/`SIGNING_SECRET`.
- Security headers set on all web responses; the Tauri desktop shell runs the
  web app under a restrictive CSP with a disabled-node WebView.

## Production checklist

- The phone OTP engine (`services/api/otp.go`) is fully self-built: crypto/rand
  codes, salted storage, attempt limits, resend throttling, in-app delivery to
  linked devices with an SMS/email hook left for carrier gateways. No third-party
  verification service.
- Group calls, meetings and live broadcasting run on our own SFU
  (`services/sfu`, Pion-based) with embedded STUN/TURN and HMAC time-limited
  credentials — no external WebRTC kit.
- Connect on-chain deposits/withdrawals through a custody provider SDK
  (Fireblocks/BitGo) writing into the existing ledger.
- Put Postgres behind TLS, enable Redis session cache, and run the media edge
  behind a CDN.
- Admin operations live in a completely separate app (`apps/admin`, port 3100)
  authenticating via `/api/admin/login` with admin-scoped tokens; user tokens
  are rejected there and admin tokens are rejected everywhere else.