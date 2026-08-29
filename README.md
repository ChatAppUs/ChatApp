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
  apps/desktop    Electron wrapper (Windows/macOS/Linux)
  apps/mobile     Capacitor wrapper (Android/iOS)

services/
  api             Go 1.23 — core API: auth, social graph, feed, chat (WebSocket),
                  WebRTC signaling, wallet ledger, ads, KYC, admin RBAC
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
  full password-reset flow, phone verification codes.
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
  search/suspend, report resolution, KYC review, ad review, audit log.
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

- Wire a real SMS provider (Twilio Verify) and email provider (SES) via the
  provider interfaces in `services/api/providers.go`.
- Connect on-chain deposits/withdrawals through a custody provider SDK
  (Fireblocks/BitGo) writing into the existing ledger.
- Add TURN servers for WebRTC NAT traversal; front large meetings with an SFU
  (LiveKit) — the signaling contract is already compatible.
- Put Postgres behind TLS, enable Redis session cache, and run the media edge
  behind a CDN.