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
  authn           Rust — trust-critical authentication core: argon2id password
                  hashing, HS256 JWT mint/verify, RFC 6238 TOTP, OTP engine,
                  CSPRNG token minting, HMAC-SHA256
  security        Rust — request signing, media upload grants, HMAC tokens
  media           C++17 — media upload & streaming edge (HTTP range requests)
  realtime        C++17 — epoll WebSocket fanout edge (10k+ connections,
                  JWT-verified /ws, HMAC-guarded /publish control port)
  transcode       C++17 — ffmpeg HLS ABR ladder + thumbnail worker
                  (SKIP LOCKED job claiming via the API control plane)
  counters        C++17 — real-time counters engine (hashtag trends, view
                  counts, live-room viewers) with periodic flush to Postgres
  sfu-forwarder   C++17 — TURN relay (UDP 3479) with self-contained SHA-1/
                  HMAC-SHA1 STUN message handling
  ml              Python — feed ranking, content moderation, KYC auto-verify,
                  ASR hooks

infra/
  db              PostgreSQL migrations 001–024 (pgcrypto, citext)
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
cp .env.example .env   # set real secrets (see "Secrets & production setup")
docker compose up --build
# web: http://localhost:3000  api: http://localhost:8080  admin: http://localhost:3100
```

## System requirements

**Minimum (single-node dev):** 4 vCPU, 8 GB RAM, 20 GB disk, Linux x86_64
(Ubuntu 22.04+/Debian 12+ recommended) or macOS 13+ for client builds.

**Recommended (production node):** 8+ vCPU, 16 GB+ RAM, NVMe SSD, and one
node per stateful service (Postgres, Redis, MinIO) behind a load balancer.

**Toolchains (build from source):**

| Component | Requirement |
|-----------|-------------|
| Go services (`api`, `sfu`) | Go 1.23+ |
| Rust services (`authn`, `security`) | Rust 1.75+ (`rustup`) |
| C++ services (`media`, `realtime`, `transcode`, `counters`, `sfu-forwarder`) | g++ 11+ or clang 14+ with C++17 |
| ML service | Python 3.10+, `pip` |
| Web/admin/desktop | Node.js 18+ / npm 9+ |
| Android | Android Studio Hedgehog+, JDK 17, Android SDK 34 |
| iOS | Xcode 15+, `xcodegen` (`brew install xcodegen`) |
| Datastores | PostgreSQL 15+ (pgcrypto, citext), Redis 6+, MongoDB 6+, MinIO/S3 |
| Media | `ffmpeg` 5+ (transcode worker + live ingest) |

## Installation

### 1. Datastores

```bash
# PostgreSQL
sudo apt-get install postgresql
sudo -u postgres psql -c "CREATE USER chatapp PASSWORD 'chatapp' SUPERUSER;"
sudo -u postgres createdb -O chatapp chatapp

# Apply migrations in order (001..024)
for f in infra/db/0*.sql infra/db/1*.sql; do
  sudo -u postgres psql -d chatapp -f "$f"
done

# Redis + MongoDB + MinIO (or use docker compose for just these)
sudo apt-get install redis-server mongodb-org
```

### 2. Backend services

```bash
# Core API (Go)
cd services/api && go build -o chatapp-api .
DATABASE_URL=postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable \
  ./chatapp-api   # :8080

# Rust authn core
cd services/authn && cargo build --release && ./target/release/chatapp-authn   # :8400

# Rust security service
cd services/security && cargo build --release && ./target/release/chatapp-security  # :8090

# C++ edges
cd services/media     && g++ -O3 -std=c++17 -Wall -Wextra -pthread main.cpp -o media-edge
cd services/realtime  && g++ -O3 -std=c++17 -Wall -Wextra -pthread main.cpp -o realtime-relay
cd services/transcode && g++ -O3 -std=c++17 -Wall -Wextra -pthread main.cpp -o transcode-edge
cd services/counters  && g++ -O3 -std=c++17 -Wall -Wextra -pthread main.cpp -o counters-edge
cd services/sfu-forwarder && g++ -O3 -std=c++17 -Wall -Wextra -pthread main.cpp -o sfu-forwarder

# SFU (Go/Pion)
cd services/sfu && go build -o sfu . && ./sfu   # :8095 + udp/3478

# ML service (Python)
cd services/ml && pip install -r requirements.txt && \
  python3 -m uvicorn main:app --port 8200
```

### 3. Web & admin frontends

```bash
cd apps/web   && npm install && npm run build && npm start   # :3000
cd apps/admin && npm install && npm run build && npm start   # :3100
```

## Building the apps

Every client consumes the same backend; set the API base URL per client.

### Web (Next.js PWA)

```bash
cd apps/web
npm install
NEXT_PUBLIC_API_URL=https://api.example.com npm run build
npm start                 # or `npm run dev` for development
```

### Desktop (Tauri 2 shell over the web app)

```bash
cd apps/desktop
npm install
npm run tauri dev         # development
npm run tauri build       # produces installers (msi/dmg/AppImage/deb)
```

### Android (Kotlin + Jetpack Compose)

```bash
cd apps/android
./gradlew assembleDebug           # debug APK
./gradlew assembleRelease         # release APK (sign in Gradle config)
# or open the folder in Android Studio and Run
```
Point `BuildConfig.API_BASE_URL` / `MEDIA_BASE_URL` at your deployment.

### iOS (SwiftUI)

```bash
cd apps/ios
xcodegen                        # generates ChatApp.xcodeproj from project.yml
open ChatApp.xcodeproj          # build & run in Xcode 15+
```
Set `CHATAPP_API_BASE` / `CHATAPP_MEDIA_BASE` in the scheme environment or
`Info.plist`.

### Browser extension (Chrome/Firefox MV3)

No build step — it is plain MV3:

1. Open `chrome://extensions` (or `about:debugging#/runtime/this-firefox`).
2. Enable **Developer mode**.
3. **Load unpacked** → select `apps/extension`.
4. Open the extension **Options** page and paste a user access token; the
   popup badge reads `/api/me` + `/api/notifications`, and the full-page view
   iframes the web app.

## Secrets & production setup

All secrets are read from environment variables (see `.env.example`). In
production **every one of these must be set to a strong, unique value** —
the services refuse to boot with weak/empty critical secrets when
`APP_ENV=production`.

Generate strong secrets:

```bash
openssl rand -hex 32   # 64-char hex — use for JWT_SECRET, SIGNING_SECRET,
                       # WALLET_MASTER_SEED, CLUSTER_SECRET, AUTHN_SECRET,
                       # SFU_SECRET, TURN_SECRET, COUNTERS_SECRET, FLUSH_SECRET
```

| Secret | Used by | Purpose |
|--------|---------|---------|
| `JWT_SECRET` | Go API | HS256 signing of access/refresh tokens. **Required ≥32 bytes in production.** |
| `SIGNING_SECRET` | Rust security svc | HMAC request signing + media upload grants. **Required ≥32 chars in production.** |
| `AUTHN_SECRET` | Rust authn svc | Bearer auth between the API and the authn core. |
| `WALLET_MASTER_SEED` | Go API | HKDF derivation of deterministic deposit addresses + the withdrawal HMAC signing key. **Required in production.** Never store a real private key. |
| `CLUSTER_SECRET` | API ↔ realtime ↔ transcode | Internal bearer for the control plane (`/internal/*`, `/publish`). |
| `SFU_SECRET` | API ↔ SFU | HMAC key for room tickets. |
| `TURN_SECRET` | API ↔ SFU/TURN | HMAC key for short-lived TURN credentials. |
| `COUNTERS_SECRET` | API ↔ counters engine | Control-plane bearer for the counters service. |
| `FLUSH_SECRET` | counters → API | Bearer the counters engine uses on `/internal/counters/flush` (must equal `CLUSTER_SECRET`). |
| `DATABASE_URL` | Go API | Postgres DSN (use TLS in production). |
| `REDIS_URL` | Go API | Optional scaling cache (FYP). Empty = caching off (fail-open). |
| `MONGO_URL` | Go API | MongoDB for non-relational stores. |
| `S3_*` | media | Object storage endpoint/credentials for media. |
| `ALLOWED_ORIGINS` | Go API | Comma-separated CORS + WebSocket Origin allowlist. Empty = dev wildcard. **Set explicitly in production.** |
| `GOOGLE_CLIENT_ID` / `NEXT_PUBLIC_GOOGLE_CLIENT_ID` | API + web | Google sign-in (ID-token verified against Google's JWKS). |
| `WEBAUTHN_RP_ID` / `WEBAUTHN_ORIGINS` | API | Passkey relying-party — must match the public origin. |
| `VAPID_*` / `FCM_SERVER_KEY` / `APNS_*` | API | Optional push gateways (in-app + WS notifications work without them). |
| `SMTP_*` | API | Optional carrier-gateway hook for OTP code delivery (the OTP engine itself is self-built). |
| `SUMSUB_*` | API | Optional KYC provider (self-built ML auto-verify runs without it). |

**Production hardening checklist:**

- Set `APP_ENV=production` and provide every required secret above.
- Terminate TLS at a load balancer; put Postgres/Redis/Mongo/MinIO on a
  private network with TLS enabled.
- Set `ALLOWED_ORIGINS` to your exact app origins (no wildcard).
- Run the admin console (`apps/admin`, port 3100) on a separate, access-
  restricted domain — it uses admin-scoped tokens rejected everywhere else.
- Wire on-chain deposits/withdrawals through a custody provider SDK
  (Fireblocks/BitGo) writing into the existing double-entry ledger.
- Put the media edge behind a CDN; keep download IDs unguessable (they are
  128-bit CSPRNG by default).

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