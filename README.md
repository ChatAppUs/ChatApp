# ChatApp

A vertically-integrated social platform combining social feed, stories, reels,
real-time messaging, audio/video calls, a multi-chain crypto wallet with P2P
payments, advertising, and a full admin panel — web (Next.js PWA), desktop
(Electron), and mobile (Capacitor) clients against a polyglot microservice
backend.

## Architecture

```
clients/
  apps/web        Next.js 14 + TypeScript PWA (8 locales, RTL support)
  apps/admin      Next.js 14 — standalone admin console (separate login plane,
                  never reachable from the user app)
  apps/desktop    Electron wrapper (Windows/macOS/Linux)
  apps/mobile     Capacitor wrapper (Android/iOS)

services/
  api             Go 1.23 — core API: auth (incl. self-built OTP engine), social
                  graph, feed, chat (WebSocket), WebRTC signaling, wallet ledger,
                  ads, KYC, admin RBAC, call/live orchestration
  sfu             Go 1.23 (Pion) — self-built SFU for meetings/group calls/live
                  broadcast + embedded STUN/TURN server
  security        Rust — request signing, signed media URLs, HMAC tokens
  media           C++17 — media upload & streaming edge (HTTP range requests)
  ml              Python — feed ranking + content moderation hooks

infra/
  db              PostgreSQL schema (24 tables, pgcrypto, citext)
  docker-compose  Postgres, MongoDB, Redis, MinIO + all services
```

## Features

- **Accounts**: register/login by email, username, or phone (245 countries with
  dial codes and flags), bcrypt password hashing, access+refresh token sessions,
  full password-reset flow, phone verification codes, **Google sign-in**
  (ID-token verified against Google's live JWKS), **passkeys/WebAuthn**
  (fingerprint, face, or device-passcode login — pure-stdlib CBOR/COSE
  verification, ES256/RS256/EdDSA, replay-resistant sign counters), and
  **Telegram-style QR login** (scan from a signed-in device to approve a new
  one, one-shot tokens).
- **Social**: posts, comments with @mentions (with notifications), likes,
  follows, user search, profiles, stories (24h expiry), reels with view
  tracking, ML-ranked feed with moderation hook.
- **Messaging**: realtime WebSocket chat (1:1 and groups), conversation
  deduping, member isolation, message persistence.
- **Calls**: WebRTC mesh audio/video calls and group meetings, signaling relayed
  over the ChatApp WebSocket; STUN-based, SFU-ready contract.
- **Wallet**: multi-chain accounts (BTC, ETH, USDT, USDC, BNB, SOL, TRX, MATIC,
  LTC, DOGE), double-entry ledger, atomic P2P transfers gated on KYC
  verification, full transaction history.
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

# ML service (Python)
cd services/ml && pip install -r requirements.txt && python main.py

# Web app (Next.js)
cd apps/web && npm install && npm run dev

# Desktop
cd apps/desktop && npm install && npm start

# Mobile
cd apps/mobile && npm install && npx cap add android && npx cap sync
```

## Testing

- Go: `go build ./... && go vet ./...`
- Rust: `cargo test` (SHA-256/HMAC RFC test vectors)
- Integration: full user-journey suite (43 API assertions + 6 WebSocket
  assertions) covering auth, reset, phone codes, posts, likes, comments,
  mentions, follows, stories, reels, chat, calls signaling, KYC, transfers,
  ads, moderation, and suspension — all passing against real PostgreSQL.

## Security notes

- Passwords hashed with bcrypt; sessions are random 256-bit tokens, refresh
  tokens stored as SHA-256 hashes.
- Every authenticated request re-validates user status (suspension takes
  effect immediately).
- Wallet transfers run in a single DB transaction with balance checks; KYC
  enforced server-side.
- Inter-service calls are HMAC-signed (Rust security service); media downloads
  use expiring signed URLs.
- Security headers set on all web responses; Electron runs sandboxed with
  context isolation.

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