# ChatApp — Complete Master Documentation

> **Single source of truth for the ChatApp monorepo.** This file combines the
> project's **README.md** (human-facing overview) with the complete **AI Agent
> Master Implementation Specification** (the 129-section production spec used
> by autonomous coding agents to build, audit, repair, extend, and maintain ChatApp).

## Implementation status — audited 2026-09-12

The repository implementation is tracked by `feature-registry.json`, validated by `scripts/validate-feature-registry.py`, and enforced in `.github/workflows/validate.yml`. The registry covers the documented P0/P1/P2 platform surfaces across Web, Android, iOS, Desktop, Extension, Backend, and Database. Repository parity, web/admin production builds, ML compilation, extension syntax, migration ordering, API readiness wiring, and backup-script syntax are validated in this checkout. Provider-backed, real-device, load, disaster-recovery, and production deployment tests require their configured environments and are not claimed as executed here.

| Status area | Repository evidence | Current result |
|---|---|---|
| Feature registry and cross-platform parity | `feature-registry.json`, `scripts/validate-feature-registry.py`, `tests/parity_check.py` | Implemented and statically validated |
| CI and production safeguards | `.github/workflows/validate.yml`, `/health`, `/ready`, `docker-compose.yml`, `scripts/backup-restore.sh` | Implemented and syntax-validated |
| Client and service feature surface | `apps/`, `services/`, `infra/db/` | Implemented source coverage; runtime environments required for final certification |
| Creator analytics integration | `services/api/handlers_gap9.go`, `infra/db/025_gap_pack9.sql`, web `creator/page.tsx`, Android `MonetizeScreen.kt`, iOS `FeatureClient.swift`/`FeatureViews.swift` | Implemented: Web, Android, and iOS Creator Studio surfaces consume daily reach, impressions, watch time, follower growth, and top-sound insights |
| Professional analytics dashboard | `services/api/handlers_gap4.go`, `services/api/main.go`, `apps/web/src/app/analytics/page.tsx`, `apps/web/src/components/Nav.tsx` | Implemented: authenticated web dashboard consumes account posts, audience, engagement, seven-day shares, and earnings metrics |

## What is inside

| Part | Source file | Contents |
|---|---|---|
| **Part I** | `README.md` | Project overview: two-way access (full member / fully anonymous guest), architecture (Go / Rust / C++ / Python services), all clients (Web, Admin, Android, iOS, Desktop, Extension), full feature inventory (social, messaging, calls, creator economy, multichain wallet, bots, admin/trust & safety), development &amp; test |
| **Part II** | `ChatApp_Complete_Master_Features_Formatted.md` | Master instruction set for AI coding agents: operating contract, non-negotiable rules, repository discovery, implementation order, 129 numbered sections covering identity, auth, authorization, private/E2E messaging, groups/channels, realtime, offline multi-hop mesh, voice/video calls, media, social, stories/reels/live, communities, creator economy, multichain crypto wallet, conversion, P2P, staking, crypto cards, LuckyDraw, admin/moderation/audit, database/ledger/API/security rules, testing, definitions of done, feature trees, gap analysis, and platform architecture summary |

---

## Quick links

- [Part I — Project Overview (README.md)](#part-i--project-overview-readmemd)
- [Part II — AI Agent Master Implementation Specification](#part-ii--ai-agent-master-implementation-specification)
- [Full feature inventory](#feature-inventory)
- [Development & test](#development--test)
- [AI agent operating contract](#1-agent-operating-contract)
- [Feature-by-feature execution list](#75-required-feature-by-feature-execution-list)
- [Final platform architecture summary](#129-final-platform-architecture-summary)

---

# Part I — Project Overview (README.md)

---

# ChatApp — Complete Social, Messaging, Calls & Crypto Platform

ChatApp is a **fully-featured monorepo** social platform: realtime chat, audio/video calls,
stories, reels, groups, pages, events, live rooms, creator monetization, and a complete
multi-chain **crypto wallet** (deposits, P2P marketplace, staking, virtual crypto cards,
convert, withdrawals, and crypto payouts for earnings** — shipped on **all clients**:
Web, Admin, Android, iOS, Desktop (Tauri)and Browser Extension.

Every back-end service is wired to every frontend(and every frontend to every backend** —
**100/100 feature parity**. Light/dark theme works on **every page of every app**.

## Access — Full App with or without an Account

| Mode | Authentication | Works immediately | Sessions manager | Notes |
|---|---|---|---|---|
| **Full member** | Register / login (username, email, phone, Google, passkey, QR, 2FA) | ✔ full features | ✔ multi-account switcher, sessions manager, revoke | Own posts, following, wallet, chats, calls, creator tools, admin plane |
| **Browser / guest (TorChat/Simplex/Briar/Session-style)** | None — no login, no password, no email, no phone, no IP-bound identity | ✔ full anonymous access — 1:1 chat, group chat, audio/video calls, group calls+ read-only browsing | ✔ device-local `chatapp.guest` ephemeral anonymous session (`localStorage`; no server-side account row | Chat **anonymously + call + group chat + group call** like TorChat/Simplex/Briar/Session — zero traceability: messages/calls are E2E,, relayed without storing who talked to whom;; feed,, FYP,, reels,, stories,, groups,, pages,, trending,, search,, public profiles stay browsable;; logout clears the device session |
| **Continue as guest** | One tap from login/register page — `localStorage` session boot | ✔ full anonymous feature surface | ✔ device-local | Anonymous chat, calls, group chat, group calls + full browsing until the user registers/logs in; account creation in place promotes the guest session to full member |

Users can use the app **two ways** — fully anonymous, or with a registered member account:

- **Anonymous guest mode (TorChat/Simplex/Briar/Session-style)** — one tap from the login/register
  page (`Continue without account`) boots a **device-local ephemeral session**
  (`chatapp.guest` in `localStorage`; no server-side account row created). **No one can
  track this**: guests chat **1:1**, join **group chats**, make **audio/video calls** and
  **group calls** completely anonymously — no username, no password, no email, no phone,
  no IP-bound identity stored; messages/calls are **end-to-end encrypted** and relayed without
  storing who talked to whom.. The **whole public feature surface** stays reachable too —
  feed, FYP, reels, stories, groups, pages, channels, events, trending, search,
  public profiles, marketplace listings, price tickers..
- **Registered member** — login/register (username, email, phone, Google, passkey, QR, 2FA),
  unlocks the **full write experience** — posting, comments, reactions, DMs, calls, wallet,
  P2P, staking, cards, convert, creator tools, admin plane — with a normal sessions
  manager (multi-account switcher, device list, remote revoke)..
### How access works (tree)

```
                         ┌─ Login / Register (full member)
                         │    ├── JWT access + refresh session (multi-account switcher)
                         │    ├── Full write surface: posts, chats, calls, wallet,
                         │    │   P2P, staking, cards, convert, creator tools, admin
                         │    └── Sessions manager: device list, remote revoke
 A user on ChatApp ────┤
                         │                        ┌─ One tap "Continue without account"
                         └─ Guest browser session ─┤
                                                  ├── Device-local ephemeral anonymous session
                                                  │   (`chatapp.guest` = localStorage; no account row)
                                                  ├── Anonymous 1:1 chat + group chat — E2E,
                                                  │   relayed without storing who talked to whom
                                                  ├── Anonymous audio/video calls + group calls
                                                  ├── Read-only surface: feed,, FYP,, reels,, stories,, groups,
                                                  │   pages,, chat preview,, calls lobby,, trending,, search,
                                                  │   public profiles,, prices,, listings
                                                  ├── Survives tab close;; cleared on logout
                                                  └── Register/login later → promotes guest session
                                                      to the full member session (no re-entry,, no lost context)
```

---

## Architecture (high-speed, ultra-low-latency, world-scale)

| Layer | Service | Tech | Role |
|---|---|---|---|
| API | `services/api` | **Go** (pgx, Redisand high-load control plane | Auth, posts, chat, calls signaling, wallet, P2P, staking, cards, convert, ads, bots, admin plane, rate limits, KYC, fanout publish|
| Security | `services/security` | **Rust** | Custody: deposit-address key derivation, withdrawal co-signature, upload-grant signing, JWT/TOTP verification, E2E fingerprints |
| AuthN | `services/authn` | **Rust** | P0 crypto surface: argon2id password hash/verify, HS256 JWT mint/verify, RFC 6238 TOTP, 6-digit OTP engine, CSPRNG, HMAC |
| Realtime | `services/realtime` | **C++** (epoll) | Ultra-low-latency WebSocket fanout data plane (:8300 WS / :8301 control) |
| Media | `services/media` | **C++** | Ultra-low-latency upload + streaming edge (:8100), signed grants, chunked resumable uploads |
| Transcode | `services/transcode` | **C++** (ffmpeg | HLS adaptive-bitrate ladder + thumbnails, reels/stories VOD, compositor (duet/stitch/trim/mix), RTMP live ingest |
| Counters | `services/counters` | **C++** | In-memory hot counters: hashtag trending, post views, live viewer counts (:8600), flushed to Postgres |
| TURN | `services/sfu-forwarder` | **C++** | Self-contained TURN relay (:3479/:8099) for NAT traversal |
| SFU | `services/sfu` | **Go** (Pion | Group calls, meetings, live broadcasting + embedded STUN/TURN (:8095) |
| ML | `services/ml` | **Python/FastAPI** | Reels ranking, KYC auto-verify (score + checks), media moderation, embeddings, captions |
| DB | Postgres 17 | SQL | Primary store (25 migrations, double-entry ledger, 177 tables) |
| Cache | Redis 7 | — | FYP feed cache (15 s TTL), price cache, rate limiting, sessions |

No SQLite anywhere. All value moves through a **double-entry ledger** with idempotent
transactions — balances = `SUM(ledger_entries.amount)` per wallet account.

---

## Clients (one feature set, every platform)

| Client | Location | Notes |
|---|---|---|
| Web app | `apps/web` (Next.js 14) | 52 route pages (social, chat, calls, wallet, P2P, staking, cards, convert, creator..., guest browse mode, full admin of own content). Guest "Continue without account" button on login/register; `chatapp.guest` ephemeral browser session for no-login browsing. Theme (`chatapp.theme`) on every page via root layout script|
| Admin console | `apps/admin` (Next.js,:3100) | Separate admin login plane (admin-scoped JWT); tokens, withdrawals, KYC, merchant, cards, transfers, ads, staking, prices, roles, reports, sanctions, groups, moderation, organizations, moments, verification requests |
| Android | `apps/android` (Kotlin/Compose) | Same features via tabbed navigation + deep screens (Wallet, P2P, Staking, Cards, Monetize, Bots, Groups, Pages, Privacy...). Native QR scan (ML Kit) + camera recorder. Theme: `Session.darkTheme` → `ChatAppTheme` (root of every Activity)|
| iOS | `apps/ios` (SwiftUI) | Same feature set (Feed, FYP, Chat, Calls, Wallet, Staking, Cards, Monetize...). Native QR scan (AVFoundation) + camera. Theme: `@AppStorage("chatapp.theme")` → `.preferredColorScheme` |
| Desktop | `apps/desktop` (Tauri 2/Rust) | Thin OS shell loading the full web app (PWA); `CHATAPP_URL` sets the server in production. Inherits web theme |
| Extension | `apps/extension` (MV3) | Chrome/Firefox. Popup quick-nav + session status; full-page view iframes the web app. Theme: chrome.storage `theme` applied before first paint |

---

## Feature inventory

### Social
- Feed, reels (duet/stitch/remix/photo-mode), stories (+stickers/music/background), moments,
  albums, posts (+feeling/location/tags/schedule/sensitive/content-warning), comments
  (sort, hide/unhide, Q&A on profiles), reactions (6 types), polls
- FYP ranker (engagement × watch-quality × recency × author-affinity, diversify,
  exploration slots, reported-reel exclusion), search (operators), hashtags, trending
- Notifications (realtime-pushed), message requests, scheduled posts, drafts,
  bookmarks (+folders), lists, chat folders, playlists, profile Q&A,
  verified-creator verification requests, memorialized accounts, multiple profiles,
  trusted contacts + account recovery, legacy contact import, quiet mode, privacy controls,
  screen-time limits, app lock, sessions manager, data saver, safety mode, discoverable toggle

### Messaging
- DMs + groups + channels (+handles `@handle`, invite links, topic rooms, emoji reactions,
  message entities (bold/italic/mono/spoiler/links, contact cards, GIFsand payments in chat),
  polls + quizzes, nicknames, themes, pins, typing actions, delete-for-me/undo,
  slow mode, custom emoji, end-to-end keys + SAS fingerprint verification,
  voice notes + waveforms, drafts, silent messages, bots (+custom mini-apps, invoices,
  inline queries), community notes, realtime presence + fanout relay (C++ edge)

### Calls & Live
- 1:1 audio/video calls, group meetings, rooms (slug link,, live rooms
  (discoverable, co-hosts, gifts + leaderboard, RTMP ingest, HLS playout,
  peak viewers), audio-only rooms (+recordings), screenshare (request/notify),
  call recordings, network-quality monitor + adaptive auto-downgrade (call-quality badge)
- SFU: group calls, meetings, live broadcasting + embedded STUN/TURN (+ optional
  C++ TURN relay forwarder for NAT traversal). Go/Pion based, no external media kit.

### Creator & Monetization
- Creator tiers + recurring subscriptions (auto-renew/expiire,, one-off tips, gifts,
  live gifts, premium plans + subscriptions, paid posts (one-time content purchase),
  marketplace listings + checkout (affiliate revenue share,, fundraisers, brand deals,
  ads + campaigns (impression/click, 55% rev-share to creator,, creator earnings
  dashboard (RPM from reels, minus payouts))
- **Earnings payout in crypto**: `POST /api/creator/payouts` requests a payout in any
  supported asset to an on-chain address; admin approves/paid. Earnings also flow
  the normal way: USD internal wallet → convert → on-chain withdrawal...

### Wallet & Crypto (multi-chain)
- Multi-chain wallet: BTC, ETH, SOL, BNB, MATIC, USDT (Ethereum/Tron/BSC/Polygon),
  USDC (Ethereum/Polygon/Solana/Base), USD (internal)
- Deterministic self-custody deposit addresses (HKDF-SHA256 from `WALLET_MASTER_SEED`,
  bech32 SegWit/base58check legacy+Tron/keccak EVM/base58 Solana), no private keys stored.

- **Deposits**: on-chain watchers (EVM token logs, BTC, Tron, Solana via own node RPC
  or `rpc_url` per token; `chain_deposits` + idempotent ledger credit + notification.
 Own-node mode via platform token `rpc_url`.

- **Withdrawals**: KYC-gated, per-chain address validation, min + fee checks, holds
  amount+fee immediately (`withdrawal_hold`), risk score (account age/new address/velocity/
  USD value), auto-approve under threshold (HMAC `withdraw|...` signature), manual admin
  review otherwise; reject → compensating `withdrawal_refund`; execute → on-chain broadcast
  via custody provider or `signed` queued status + transaction hash on completion.

- **P2P**: offers (buy/sell, fiat rails per country, 881 local payment methods), merchant
  tiers + caps (verified merchant badge `owner_is_merchant`), trades with crypto locked in
  escrow on the ledger from open until release/cancel/dispute; admin dispute resolution;
  notifications at every state.

- **Convert**: atomic USD↔crypto swaps at admin-maintained `convert_rates`
  (also derived from P2P order book visas orderbook source,, both directions, ledger
  entries `convert_out`/`convert_in`, quoted rate computed in SQL numeric — no float drift.



- **Staking**: multi-asset staking (APY + durations, min/max), positions with APY frozen at
  open + simple-interest rewards (amount×apy×days/365, quantized to 18 dp), settled from
  treasury in the same tx; unlock early → `unlock_requested` + admin settle; full audit log
  of rate changes. Live prices via CoinGecko (cached in Redis), admin overrides,
  P2P order-book fallback.



- **Virtual crypto cards**: Luhn-valid PAN (platform private range 990099), SHA-256
  stored only + last4; issue up to 5, KYC-gated; charge endpoint (blazing platform POS
  processors: PAN+CVV+expiry, status (active|frozen|terminated), daily/monthly limits ,
  USD internal balance); top-up (any crypto → USD atomically via convert engine); refund;
  admin card management (+status). Everything moves on the double-entry ledger with the FOR
  UPDATE lock+decline insert in the same tx (no self-deadlock).





- **Transfers**: user-to-user P2P transfer (KYC-gated, ledger `p2p_send`/`recv`,
  notifications), admin transfer oversight + reversal (compensating double-entry).



### Bots, Mini Apps & Platform
- Bots (create/manage via `POST /api/bots`, per-bot token, getMe/getChat/
  editMessageText idempotent, createInvoice + pay via wallet, inline queries,
  mini-app registry launcher (`GET /api/miniapps`) with add/remove owner-auth.

### Guest Access — Fully Anonymous Chat, Calls, Group Chat & Group Calls (TorChat/Simplex/Briar/Session-style)
- **No-account entry**: login/register pages surface a **“Continue without account”** button;
  one tap boots a **device-local ephemeral guest session** (`chatapp.guest` in `localStorage`),
  persisting across tab closes until explicit logout — no username,, no password,
  no email,, no phone,, no server-side account row created..
- **Fully anonymous chat, calls, group chat & group calls**: guests use the app exactly
  like **TorChat / Simplex / Briar / Session** — 1:1 messages,, group chats,
  audio/video calls,, and group calls,, all **end-to-end encrypted**,, with **no one able
  to track who talked to whom** (no username,, no phone,, no IP-bound identity,, no
  record of conversations linked back to a person). The relay carries ciphertext only;
  nothing is stored that could identify the participants afterwards..
- **Full feature surface reachable without auth**: all public content is browsable —
  feed,, FYP,, reels and fyp ranking,, stories,and moments,, groups,, pages,, channels,
  events,, hashtags,, trending,, search,, public profiles,, chat/call lobbies,
  marketplace listings,, price tickers,, staking asset catalog,, media playback
  (signed-grant downloads need a member login); admin plane stays login-only..
- **Zero signup friction**: guests roam every client the same way a logged-in user does —
  web inherits the member UI where write actions softly prompt for login..
- **Promotion in place**: the moment a guest registers or logs in, the existing
  session takes over as the full member session (no re-entry,, no lost context),
  and the full write surface — posting,, messaging,, wallet,, creator tools —
  unlocks immediately..
- **Sessions manager parity**: member sessions remain fully manageable (multi-account
  switcher,, device list,, remote revoke); guest sessions are managed device-locally
  (clear `chatapp.guest` = logout)..

### Admin & Trust & Safety
- Admin roles (dynamic `admin_role_defs`,permissions incl. p2p.resolve ,
  convert.manage, withdrawals.review, tokens.manage, staking.manage), separate
  admin login plane.
- KYC (own ML auto-verify ≥0.75 + sanctions-clean, manual review fallback,
  uploads of docs/selfie, admin queue with score/checks).
- Sanctions screening (OFAC/EU/UN CSV import, trigram name match,, hits surfaced on
  KYC + P2P + withdrawals).
- Reports + moderation (content moderation, user reports trilogy, block/unblock,
  blocked-media hash + ML verdicts,, word filters,(creator comment auto-hide), community notes.
- Custom emoji admin registry; media uploads require signed grants (C++ edge verifies via
  Rust security service); download URLs are unguessable 128-bit CSPRNG IDs, no auth.


### Full Secure Stack
- **Rust** (authn+security) owns passwords/JWT/TOTP/OTP/custody/co-sign/grants;
  Go delegates when `AUTHN_SERVICE_URL`/`SECURITY_SERVICE_URL` configured.

- **C++** owns the ultra-low-latency data plane (realtime fanout, media edge,
  transcode/ffmpeg, hot counters, TURN relay).
- **Go** owns the high-load control plane; Redis caching;; Postgres double-entry ledger;
  idempotent internal worker contracts (`/internal/transcode/claim|complete`,
  `/internal/counters/flush`).
- Production gates: `APP_ENV=production` requires length≥32 secrets (JWT,
  WALLET_MASTER_SEED, WITHDRAW_SIGNING_KEY, SIGNING_SECRET,AUTHN_SECRET,
 etc.). and the services refuse to boot otherwise. No hardcoded production secrets.

---

## Development & Test

Requirements: Go 1.25, Rust 1.85, C++17 compiler, Python 3.11+, Node 20+,
ffmpeg, Postgres 17, Redis 7.  `docker compose up --build` brings up the whole
stack (api:8080, web:3000, admin:3100, sfu:8095, media:8100, realtime:8300,
security:8090, authn:8400, counters:8600, ml:8200, TURN:3479).

Local sandbox (no Docker):

```bash
sudo apt-get install postgresql postgresql-contrib golang-go ffmpeg rustc cargo redis-server
sudo service postgresql start && sudo service redis-server start
sudo -u postgres psql -c "CREATE USER chatapp PASSWORD 'chatapp' SUPERUSER" -c "CREATE DATABASE chatapp OWNER chatapp"
for f in infra/db/0*.sql; do sudo -u postgres psql -d chatapp -q -f "$f"; done
cd services/api && go build -o /tmp/chatapp-api .
cd services/sfu && go build -o /tmp/sfu .
cd services/security && cargo build --release && cd ../authn && cargo build --release
python -m uvicorn main:app --app-dir services/ml --port 8200 &
```

Test suites (`tests/`): finance(staking, cards, p2p, convert, withdrawals), gaps→gaps10
(every gap pack), features, integration (153 E2E checks), authn (Rust delegation),
counters (C++ engine + flush), parity_check (443 routes across all clients),
sfu_turn (TURN relay). Run spaced ≥1 min apart (register rate limit: 10/min).

```bash
python tests/finance_test.py && sleep 60
python tests/features_test.py && sleep 60
python tests/gaps_test.py && sleep 60
# …  + tests/gaps2_test.py … gaps10_test.py, tests/staking_test.py
python tests/integration_test.py && python tests/authn_test.py
python tests/counters_test.py && python tests/sfu_turn_test.py && python tests/parity_check.py
```

Verified full sweep: integration **153/153**, features **72/72**, finance **44/44**,
gaps **92**, gaps2 **70**, gaps3 **82**, gaps4 **96**, gaps5 **39**, gaps6 **91**,
gaps7 **85**, gaps8 **32**, gaps9 **15**, gaps10 **8**, staking **56**, authn **14**,
counters **12**, sfu-turn **19**, parity **OK**; `go test ./...` OK; web `next build` OK;
admin `tsc` OK; all C++ + Rust services build OK.

---

# Part II — AI Agent Master Implementation Specification

<details>
<summary>Contents of the full master specification (129 sections)</summary>

Sections covered: 1. Agent Operating Contract · 2. Absolute Non-Negotiable Rules · 3. First Action: Repository Discovery ·· 4. Implementation Order ·· 5. Architectural Master Rule ·· 6. Identity and Account ·· 7. Authentication and Session System ·· 8. Authorization Model ·· 9. Private Connection System ·· 10. Private Messaging ·· 11. End-to-End Encryption ·· 12. Group Messaging ·· 13. Channels ·· 14. Realtime System ·· 15. Offline and Local Communication ·· 16. Voice Messages ·· 17. Voice and Video Calls ·· 18. File and Media Pipeline ·· 19. Social Profile ·· 20. Friend and Follow ·· 21. Posts ·· 22. Comments and Threads ·· 23. Feed ·· 24. Hashtags and Trends ·· 25. Stories ·· 26. Short Video Platform ·· 27. Video Discovery ·· 28. Live Streaming ·· 29. Communities ·· 30. Forums ·· 31. Events ·· 32. Search ·· 33. Notifications ·· 34–37. Creator Economy,Tips,Subscriptions,Payouts ·· 38–43. Multichain Wallet,Security,Convert,P2P,Staking,Card ·· 44–49. LuckyDraw (model, pricing, winner selection, unique-user rule, prize allocation) ·· 50–54. Administration,Approvals,Audit,Moderation,Blocking ·· 55–56. Database and Financial Ledger Rules ·· 57–74. API, Trust, Validation, Rate Limits, Secrets, Errors, Jobs, Media, Caching, Observability, Security Testing, Testing Requirements, Definition of Done, Parity, Migrations, Dependencies, AI Agent Workflow, Feature Template ·· 75–80. Required Execution List, Release Gates, Quality Standard, Performance/Security/Multi-Language Architecture, Shared Native Core ·· 81–129. Expanded platform requirements (offline mesh, WebRTC/media, backend language allocation, performance, security-by-language, wallet/mesh separation, admin mesh control, feature documentation, final platform requirements, feature trees, competitor integration, cross-platform completeness, gap analysis, final architecture summary).

</details>

---

# ChatApp AI Agent Master Instructions
## Production Implementation Specification for Autonomous Coding Agents

**Document purpose:** This is the master instruction set for AI coding agents implementing, auditing, repairing, extending, and maintaining ChatApp.

**Architecture update:** This version explicitly covers Rust, C++, Go, Next.js/TypeScript, Kotlin, Swift, shared native cores, and a real multi-hop offline mesh architecture using Bluetooth, local Wi-Fi, Wi-Fi Direct and other platform-supported peer-to-peer transports.

---

# 1. AGENT OPERATING CONTRACT

You are working on a production platform named **ChatApp**.

ChatApp is intended to provide:
- Private messaging
- Groups, channels, and communities
- Voice and video communication
- Social networking
- Public posts, threads, hashtags, and trends
- Short and long-form video
- Live streaming
- Creator tools and monetization
- Multichain crypto wallet functionality
- Crypto conversion
- P2P trading functionality
- Staking
- Crypto card integrations
- Daily and monthly LuckyDraw functionality
- Full administration, moderation, compliance, security, and audit systems

Competitor capability inspiration includes publicly observable functionality from:
- Facebook
- Telegram
- X
- TikTok
- imo
- WhatsApp
- TorChat
- SimpleX Chat
- Session
- Briar

Do **not** copy competitor source code, proprietary designs, branding, assets, or private APIs. Independently implement required capabilities.

---

# 2. ABSOLUTE NON-NEGOTIABLE RULES

The coding agent MUST NOT:

1. Claim a feature is complete when only UI exists.
2. Use mock data in production paths.
3. Return fake success responses.
4. Create demo-only implementations and present them as production.
5. Leave stubs, TODO-only handlers, empty services, placeholder APIs, or fake transactions.
6. Bypass authentication or authorization for convenience.
7. Trust client-side validation as security.
8. Hardcode secrets, passwords, API keys, private keys, tokens, or production credentials.
9. Invent cryptographic algorithms.
10. Disable security checks to make tests pass.
11. Silently swallow errors.
12. Delete tests merely to make a build pass.
13. Break existing working features without migration and regression testing.
14. Expose private messages, keys, financial information, or sensitive user data.
15. Implement a hidden universal decryption backdoor.
16. Represent simulated financial activity as real.
17. Represent estimated rewards as guaranteed rewards.
18. Implement regulated financial or prize functionality without required compliance boundaries.
19. Give administrators unrestricted access unless explicitly required and audited.
20. Mark a repository as production-ready without verifying actual code paths.

Every implemented feature must have:
- UI where applicable
- API/interface
- Backend business logic
- Persistent storage where applicable
- Authentication
- Authorization
- Validation
- Error handling
- Logging appropriate to sensitivity
- Tests
- Monitoring/operational hooks where applicable

---

# 3. FIRST ACTION: REPOSITORY DISCOVERY

Before changing code, inspect the actual repository.

Do not rely exclusively on:
- README files
- architecture documents
- task descriptions
- package descriptions
- comments
- generated documentation

Perform actual code inspection.

## 3.1 Build an inventory

Identify:
- Monorepo or multi-repository structure
- Applications
- Packages
- Libraries
- Backend services
- APIs
- Databases
- ORM/data layers
- Authentication systems
- Queues
- Storage
- WebSocket/realtime systems
- Media processing
- Blockchain integrations
- Third-party providers
- CI/CD
- Tests
- Infrastructure configuration

## 3.2 Identify every client

Check for:
- Android
- iOS
- Web
- Desktop
- Windows
- macOS
- Linux
- Browser extension
- Shared SDKs

Create a feature parity matrix.

## 3.3 Search for incomplete implementation

Search actual source code for indicators such as:
- TODO
- FIXME
- XXX
- HACK
- NotImplemented
- throw new Error("not implemented")
- placeholder
- mock
- fake
- dummy
- sample
- simulation
- temporary
- return null
- empty catch blocks
- disabled tests

Do not automatically assume every occurrence is a defect. Inspect each actual execution path.

## 3.4 Trace features end-to-end

For every feature trace:

Client UI
→ state management
→ API call
→ authentication
→ authorization
→ request validation
→ service/business logic
→ persistence
→ asynchronous processing
→ response
→ client state update
→ error handling
→ tests

A feature is incomplete if a required link in its production path is missing.

---

# 4. IMPLEMENTATION ORDER

Do not attempt to build the entire platform randomly.

Use dependency order.

## Phase 0 — Repository Baseline
- Inventory
- Build verification
- Test baseline
- Dependency audit
- Existing architecture analysis
- Broken-file identification

## Phase 1 — Foundation
- Configuration
- Environment management
- Database
- Migrations
- Identity
- Authentication
- Sessions
- Authorization
- Audit logging

## Phase 2 — Core Communication
- Contacts
- Private messaging
- Groups
- Attachments
- Realtime delivery
- Notifications
- Privacy controls

## Phase 3 — Calling and Media
- Voice messages
- Voice calls
- Video calls
- Media processing
- File delivery

## Phase 4 — Social Platform
- Profiles
- Posts
- Feed
- Comments
- Followers/friends
- Stories
- Hashtags
- Search

## Phase 5 — Communities
- Groups
- Channels
- Communities
- Forums
- Events
- Moderation

## Phase 6 — Video
- Short video
- Discovery
- Creator tools
- Long video
- Live streaming

## Phase 7 — Creator Economy
- Eligibility
- Earnings events
- Ledger
- Tips
- Subscriptions
- Payout workflows

## Phase 8 — Finance
- Wallet boundaries
- Multichain adapters
- Transaction workflows
- Conversion
- P2P
- Staking

## Phase 9 — Regulated/High-Risk Features
- Crypto card integration
- LuckyDraw

## Phase 10 — Hardening
- Security
- Performance
- Reliability
- Disaster recovery
- Observability
- Full regression testing

---

# 5. ARCHITECTURAL MASTER RULE

ChatApp is one product, but not one unrestricted trust domain.

Use explicit boundaries:

1. Identity Domain
2. Communication Domain
3. Social Domain
4. Media Domain
5. Community Domain
6. Creator Economy Domain
7. Finance Domain
8. LuckyDraw Domain
9. Administration Domain
10. Security/Observability Domain

Do not create a single service with unrestricted database access to every domain.

Prefer clear ownership.

---

# 6. IDENTITY AND ACCOUNT IMPLEMENTATION

## Required identity concepts

Implement distinct concepts:
- Internal immutable account ID
- Public username
- Display name
- Profile
- Authentication identifiers
- Verified recovery methods

Never expose internal database IDs as public identifiers.

## Registration

Support architecture for:
- Email
- Phone where enabled
- Username
- Privacy-preserving registration methods where legally and operationally appropriate

## Account security

Implement:
- Secure password handling where passwords exist
- Passkeys where supported
- MFA where supported
- Device/session management
- Login alerts
- Recovery flows
- Remote logout

Passwords:
- Never plaintext
- Never reversible storage
- Never logged

## Username rules

Implement:
- Uniqueness
- Reserved names
- Unicode safety rules
- Normalization rules
- Change rate limits
- Impersonation controls

---

# 7. AUTHENTICATION AND SESSION SYSTEM

Every authenticated session must have:
- Session identifier
- User identity
- Creation time
- Expiration policy
- Revocation state
- Device/client metadata appropriate to privacy policy

Implement:
- Login
- Logout
- Logout all sessions
- Revoke selected device
- Refresh/re-authentication policy
- New-device detection

Authorization checks must happen server-side.

Never implement:
if (clientSaysAdmin) allow();

Instead derive authority from authenticated server-controlled identity and roles.

---

# 8. AUTHORIZATION MODEL

Implement least privilege.

Recommended role concepts:
- User
- Creator
- Business
- Community owner
- Community administrator
- Moderator
- Support agent
- Finance operator
- LuckyDraw operator
- Compliance officer
- Security administrator
- Auditor
- Super administrator

Roles alone may not be sufficient.

Use:
- Role
- Permission
- Resource ownership
- Context

Every sensitive action should verify:
1. Authenticated identity
2. Permission
3. Resource ownership/context
4. Policy conditions

---

# 9. PRIVATE CONNECTION SYSTEM

Implement ways to connect:
- Username
- QR code
- Controlled links
- One-time links
- Temporary links
- Contact requests

One-time/temporary links require:
- Random high-entropy token
- Expiration
- Revocation
- Usage state
- Rate limits
- Abuse controls

Do not use predictable invitation tokens.

---

# 10. PRIVATE MESSAGING

## Required capabilities

Implement:
- Text
- Emoji
- Reactions
- Replies
- Mentions
- Edits
- Deletes
- Forwarding
- Pinning
- Saved messages
- Attachments
- Voice messages
- Search subject to privacy

## Message state machine

Use explicit states.

Example:
DRAFT
→ QUEUED
→ SENDING
→ SENT
→ DELIVERED
→ READ

Failure states:
FAILED
RETRYING

Do not report delivery or read status unless actual server/protocol state supports it.

## Message identifiers

Use stable globally appropriate identifiers.

Avoid relying solely on client-generated sequential integers.

## Idempotency

Sending must tolerate retries.

A network retry must not automatically create duplicate messages.

Use idempotency/deduplication mechanisms.

---

# 11. END-TO-END ENCRYPTION

For private encrypted communication:

- Use established, independently reviewed protocols/libraries.
- Do not create custom cryptography.
- Do not invent encryption schemes.
- Do not store plaintext private keys.
- Do not log sensitive key material.
- Design key lifecycle explicitly.

Required design topics:
- Device identity
- Session establishment
- Key rotation
- Forward secrecy where supported
- Device verification
- Multi-device behavior
- Lost-device handling
- Recovery limitations

Never claim E2EE merely because transport uses HTTPS/TLS.

Transport encryption and end-to-end encryption are different.

Do not add a hidden universal administrator decryption capability.

---

# 12. GROUP MESSAGING

Implement:
- Group identity
- Owner
- Administrators
- Moderators
- Members
- Invitations
- Join approval
- Rules
- Pinned messages
- Member management

Every administrative operation must check permissions.

Examples:
- Only authorized roles can remove members.
- Only authorized roles can alter group configuration.
- A removed user must not continue receiving new group content.

---

# 13. CHANNELS

Implement channels separately from normal groups.

Channel capabilities:
- Broadcast publishing
- Subscriber access
- Administrators
- Public/private modes
- Scheduled publishing
- Analytics
- Optional comments

Do not allow subscriber privileges to become publisher privileges.

---

# 14. REALTIME SYSTEM

Use an architecture suitable for:
- WebSocket or equivalent realtime communication
- Connection authentication
- Reconnection
- Backpressure
- Duplicate event handling
- Ordered handling where required
- Offline delivery

Do not trust a client-provided user ID in a socket message.

Bind realtime connections to authenticated server sessions.

---

# 15. OFFLINE AND LOCAL COMMUNICATION

Inspiration may be taken from Briar-style local communication concepts.

Possible transports:
1. Internet
2. Local Wi-Fi
3. Wi-Fi Direct where supported
4. Bluetooth where supported
5. Store-and-forward

Do not make WiMAX a platform dependency.

## Store-and-forward requirements

Implement:
Created
→ encrypted queued state
→ transport unavailable
→ retry
→ transport available
→ delivery
→ acknowledgement

Requirements:
- Persistent queue
- Retry policy
- Expiration
- Duplicate prevention
- Battery/resource awareness
- Explicit relay consent where applicable

Never silently discard user messages.

---

# 16. VOICE MESSAGES

Implement:
- Permission request
- Start recording
- Pause/resume where supported
- Cancel
- Preview
- Upload
- Processing
- Playback

Handle:
- Interrupted recording
- Permission denial
- Network failure
- Corrupted audio

---

# 17. VOICE AND VIDEO CALLS

Required lifecycle:
IDLE
→ INITIATING
→ RINGING
→ CONNECTING
→ CONNECTED
→ ENDED

Handle:
- Rejection
- Busy
- Timeout
- Network loss
- Reconnection
- Permission failure

Capabilities:
- Mute
- Speaker
- Camera switch
- Video enable/disable
- Device switching
- Network adaptation

For group calls:
- Participants
- Moderators
- Host controls
- Raise hand
- Screen sharing where supported

Never expose call media to unrelated administrators.

---

# 18. FILE AND MEDIA PIPELINE

All uploads follow:

Upload
→ authentication
→ authorization
→ file validation
→ malware/security processing where appropriate
→ media processing
→ storage
→ access control
→ delivery

Validate:
- Size
- Type
- Content signature where appropriate
- Malformed input
- Permissions

Never trust the filename extension.

Use server-side authorization for download/access.

---

# 19. SOCIAL PROFILE SYSTEM

Implement:
- Avatar
- Cover
- Display name
- Username
- Bio
- Links
- Privacy settings
- Content tabs

Account types:
- Personal
- Creator
- Business
- Organization

Visibility rules must be enforced server-side.

---

# 20. FRIEND AND FOLLOW SYSTEMS

Keep friend and follow relationships distinct.

Friend:
REQUESTED → ACCEPTED

Follow:
Follower → Followed account

Implement:
- Request
- Accept
- Reject
- Remove
- Unfollow
- Block

Prevent duplicate relationships with database constraints and idempotent operations.

---

# 21. POSTS

Post types:
- Text
- Image
- Video
- Link
- Poll

Actions:
- React
- Comment
- Reply
- Share
- Repost
- Quote
- Save
- Report

Every post must have:
- Author
- Visibility
- Creation time
- State
- Content policy state

Private content must not become public through:
- Share
- Quote
- Cache
- Search indexing
- Preview metadata

---

# 22. COMMENTS AND THREADS

Implement hierarchical discussion safely.

Requirements:
- Parent reference validation
- Depth limits where necessary
- Pagination
- Deleted parent behavior
- Moderation
- Reporting

Do not load unbounded entire comment trees.

---

# 23. FEED SYSTEM

Support conceptual feeds:
- Friends
- Following
- Communities
- Discover
- Trending

Separate:
- Candidate generation
- Eligibility filtering
- Ranking
- Delivery

Do not let ranking bypass privacy.

A post must first be authorized for the viewer before ranking.

User controls:
- Not interested
- Hide
- Reduce recommendations
- Following-first mode where supported

---

# 24. HASHTAGS AND TRENDS

Implement:
- Normalization
- Search
- Aggregation
- Spam resistance
- Manipulation detection

Do not calculate trends solely from raw counts.

Consider:
- Time windows
- Unique participants
- Spam
- Coordinated manipulation
- Geographic/language context

---

# 25. STORIES

Lifecycle:
Draft
→ Published
→ Active
→ Expired

Implement:
- Viewer privacy
- Expiration
- Access checks
- Media processing

Do not depend only on client-side timers for expiration.

---

# 26. SHORT VIDEO PLATFORM

Required:
- Upload
- Recording
- Draft
- Trim/crop where implemented
- Captions
- Cover
- Publishing
- Likes
- Comments
- Sharing
- Saving
- Following

Video lifecycle:
UPLOAD
→ VALIDATING
→ PROCESSING
→ READY
→ PUBLISHED

Failures:
PROCESSING_FAILED

Never show a video as ready before processing succeeds.

---

# 27. VIDEO DISCOVERY

Separate:
- Following feed
- Recommendation feed
- Trending
- Search

Recommendation signals may include:
- Explicit follows
- Watch behavior
- Likes
- Saves
- Shares

Do not use private content as a public recommendation source.

---

# 28. LIVE STREAMING

Lifecycle:
SCHEDULED
→ STARTING
→ LIVE
→ ENDING
→ PROCESSING
→ ENDED

Features:
- Chat
- Moderation
- Reactions
- Hosts
- Guests
- Replay

Implement moderation independently from stream transport.

---

# 29. COMMUNITIES

Community can contain:
- Members
- Posts
- Chats
- Channels
- Events
- Rules

Roles:
- Owner
- Administrator
- Moderator
- Member

Implement permission matrices explicitly.

Do not spread authorization logic as random if-statements across clients.

---

# 30. FORUMS

Model:
Forum
→ Category
→ Topic
→ Reply

Implement:
- Pagination
- Search
- Pinning
- Moderation
- Locked topics
- Deleted content behavior

---

# 31. EVENTS

Implement:
- Title
- Description
- Time
- Time zone
- Location or online details
- RSVP

Use timezone-aware storage and conversion.

Never assume all users are in one timezone.

---

# 32. SEARCH

Global search can include:
- Users
- Posts
- Videos
- Groups
- Channels
- Communities
- Hashtags

Security rule:

Search authorization is authorization.

Never index private content into public search.

Private search results require the same permission checks as direct access.

---

# 33. NOTIFICATIONS

Notification pipeline:

Event
→ eligibility
→ preference check
→ deduplication
→ delivery
→ state update

Support:
- Push
- In-app
- Email where enabled

Respect:
- User settings
- Quiet hours
- Rate limits
- Privacy

Never put sensitive private content in a push notification by default without considering device privacy settings.

---

# 34. CREATOR ECONOMY

Earning methods may include:
- Advertising programs
- Creator programs
- Tips
- Subscriptions
- Memberships
- Premium content

Do not calculate authoritative money on the client.

## Earnings event model

Raw event
→ fraud validation
→ eligibility validation
→ earning calculation
→ ledger entry
→ available balance
→ payout

Use an auditable ledger.

Do not mutate historical balances without recording why.

---

# 35. FRAUD PREVENTION FOR EARNINGS

Detect and review:
- Automated traffic
- Fake views
- Artificial engagement
- Fake followers
- Click fraud
- Multiple-account abuse

Do not silently punish users based solely on an unreliable model.

Use:
- Risk scoring
- Review states
- Evidence/audit trails
- Appeal workflows where appropriate

---

# 36. TIPS AND SUBSCRIPTIONS

For every payment:
- Validate payer
- Validate recipient
- Validate amount
- Validate currency
- Prevent duplicate processing
- Record immutable transaction references

Subscription states:
ACTIVE
PAST_DUE
CANCELED
EXPIRED

Access control must derive from actual subscription state.

---

# 37. PAYOUTS

Lifecycle:
EARNED
→ AVAILABLE
→ PAYOUT_REQUESTED
→ UNDER_REVIEW
→ PROCESSING
→ COMPLETED

Failure states:
FAILED
REVERSED where applicable

Do not mark payout completed until authoritative provider/system confirmation exists.

---

# 38. MULTICHAIN WALLET ARCHITECTURE

Create a wallet architecture with clear chain adapters.

Conceptually:

Wallet Core
├── Account Management
├── Asset Registry
├── Transaction Service
├── Signing Boundary
├── Chain Adapter A
├── Chain Adapter B
└── Chain Adapter N

Do not put all blockchain-specific behavior into one giant conditional chain.

---

# 39. WALLET SECURITY

Critical rules:
- No private key logging
- No plaintext secret persistence
- Secure storage
- Hardware-backed storage where available
- Clear transaction confirmation
- Network identification
- Address validation

A wallet transaction flow:

Recipient
→ validate network/address
→ amount
→ fee estimate
→ confirmation
→ signing
→ broadcast
→ tracking

Never sign a transaction silently without user authorization.

---

# 40. CRYPTO CONVERSION

Conversion lifecycle:
REQUEST_QUOTE
→ QUOTED
→ USER_CONFIRMED
→ PROCESSING
→ SETTLED

Quote includes:
- Input
- Output estimate
- Rate
- Fees
- Limits
- Expiration

Quotes expire.

Do not execute using a stale quote without explicit rules and user disclosure.

---

# 41. P2P SYSTEM

Offer:
- Buy/sell
- Asset
- Price
- Limits
- Payment method
- Availability

Order lifecycle:
CREATED
→ ACCEPTED
→ PAYMENT_PENDING
→ PAYMENT_CONFIRMED
→ SETTLED

Exceptional:
CANCELED
EXPIRED
DISPUTED

Disputes require:
- Evidence
- Authorized reviewers
- Audit logs
- Controlled resolution

Never automatically trust client assertions that external payment occurred.

---

# 42. STAKING

Display:
- Asset
- Network
- Amount
- Terms
- Lock period
- Reward methodology
- Fees
- Risks
- Unstaking rules

Lifecycle:
CREATED
→ ACTIVE
→ UNSTAKING
→ COMPLETED

Do not describe variable or estimated rewards as guaranteed.

---

# 43. CRYPTO CARD

Treat card functionality as a separately governed integration.

Possible capabilities:
- Virtual card
- Physical card
- Freeze
- Unfreeze
- Limits
- Transaction history
- Notifications

Do not build card processing as a fake internal simulation.

Use appropriate regulated partners/providers where legally required.

---

# 44. LUCKYDRAW — HIGH-RISK FEATURE

The proposed system includes:
- Daily draws
- Monthly draws
- Paid tickets
- Admin-configured future ticket prices
- A defined percentage of ticket sales allocated to prizes
- Random selection
- Potential unique-user winner rules

Because paid entry plus random chance plus prizes may be regulated, the coding agent must not treat this as an ordinary game feature.

Before production activation require:
- Legal classification
- Jurisdiction rules
- Licensing review
- Geographic restrictions
- Age restrictions
- Official published rules
- Tax review
- Consumer protection review
- Compliance approval

The system must be configurable so it can be disabled where required.

---

# 45. LUCKYDRAW DATA MODEL

Concepts:
- Draw
- Draw schedule
- Ticket
- Ticket purchase
- Payment
- Eligibility
- Prize pool
- Winner
- Settlement
- Audit record

Ticket record:
- Immutable ticket ID
- Draw ID
- Account ID
- Purchase timestamp
- Price paid
- Currency
- Payment reference
- Eligibility state

Never retroactively change the price paid for an issued ticket.

---

# 46. LUCKYDRAW TICKET PRICING

Admins may configure future prices.

Safe flow:
Draft price
→ authorized approval
→ effective timestamp
→ active for new sales

Requirements:
- Historical pricing retained
- Audit log
- Approval for sensitive changes
- No silent retroactive changes

---

# 47. LUCKYDRAW WINNER SELECTION

Do not allow a normal administrator to manually choose winners secretly.

Required lifecycle:
Sales closed
→ payment finalization
→ eligibility locked
→ participant dataset frozen
→ selection process
→ winner validation
→ prize settlement
→ result publication according to policy

Use an auditable, reviewed selection mechanism.

Do not claim “provably fair” unless the complete mechanism actually satisfies and is independently reviewed for that claim.

---

# 48. UNIQUE USER WINNER RULE

If one unique user may win at most once in a draw:

Eligible set
→ select winner
→ mark account as winner
→ exclude according to published rule
→ select next winner

Define before sales:
- Whether multiple tickets increase chances
- Whether one user can win once or multiple times
- What happens with linked/duplicate accounts

Do not change rules after ticket sales begin.

---

# 49. PRIZE ALLOCATION

The requested business concept discussed allocating approximately 80%–90% of ticket sales to prizes.

The production system must not hardcode an ambiguous percentage.

Each draw configuration must explicitly define:
- Prize allocation
- Fees
- Operational allocation
- Tax treatment where applicable
- Refund/cancellation rules

Lock the applicable rules according to the draw lifecycle.

---

# 50. ADMINISTRATION SYSTEM

Admin domains:
- User administration
- Content moderation
- Community administration
- Support
- Finance operations
- LuckyDraw operations
- Compliance
- Security
- Auditing

Recommended separation:
- Super Admin
- User Admin
- Moderator
- Support
- Finance Admin
- LuckyDraw Manager
- Compliance Officer
- Security Admin
- Auditor

Do not use one “isAdmin” boolean for all privileges.

---

# 51. ADMIN APPROVAL WORKFLOWS

Sensitive changes should support:

REQUESTED
→ REVIEWED
→ APPROVED
→ EXECUTED

or:
REQUESTED
→ REJECTED

Examples:
- Financial configuration
- Prize allocation
- Ticket price
- High-value limits
- Payout policies
- Production security configuration

The requester should not automatically approve their own high-risk request unless policy explicitly permits it.

---

# 52. AUDIT LOGGING

Sensitive actions require audit records.

Record:
- Actor
- Action
- Resource
- Timestamp
- Before state where appropriate
- After state where appropriate
- Result
- Correlation/request identifier

Audit logs must have strong access control.

Normal administrators must not be able to silently erase history.

---

# 53. MODERATION

Reportable targets:
- Users
- Posts
- Comments
- Videos
- Groups
- Channels
- Communities

Report lifecycle:
SUBMITTED
→ TRIAGED
→ UNDER_REVIEW
→ RESOLVED

Actions:
- No action
- Warning
- Content removal
- Restriction
- Suspension
- Ban

Preserve evidence and audit history according to policy and law.

---

# 54. BLOCKING

Blocking should affect applicable interactions:
- Messaging
- Calls
- Mentions
- Invitations
- Following where appropriate

Do not rely solely on hiding UI.

Enforce restrictions on backend operations.

---

# 55. DATABASE RULES

Use:
- Migrations
- Constraints
- Foreign keys where appropriate
- Unique constraints
- Transactions
- Indexes based on actual query needs

Never depend solely on application code to enforce critical uniqueness.

Examples:
- One username per normalized namespace
- One relationship record per relationship pair
- Idempotency key uniqueness where required

---

# 56. FINANCIAL LEDGER RULES

Financial domains require explicit accounting/ledger design.

Do not update balances with arbitrary:
balance = balance + amount

without:
- Transaction boundary
- Idempotency
- Auditability
- Reference
- State

Prefer immutable ledger entries with controlled derived balances.

Every financial operation requires:
- Unique operation ID
- Idempotency protection
- State machine
- Audit trail

---

# 57. API DESIGN

Every API should define:
- Authentication
- Authorization
- Request schema
- Response schema
- Error schema
- Rate limits where appropriate
- Idempotency where needed

Do not expose internal stack traces to users.

Use consistent error categories.

---

# 58. CLIENT-SERVER TRUST RULE

Clients may be:
- Modified
- Automated
- Replayed
- Outdated
- Malicious

Therefore the server must independently enforce:
- Authorization
- Amounts
- Limits
- Eligibility
- Pricing
- State transitions

Never trust:
- Client-calculated money
- Client-calculated prize
- Client role
- Client eligibility
- Client transaction success

---

# 59. INPUT VALIDATION

Validate:
- Type
- Length
- Range
- Format
- Ownership
- State

Use allowlists where practical.

Validate again at service boundaries for critical operations.

---

# 60. RATE LIMITING AND ABUSE CONTROL

Apply appropriate controls to:
- Registration
- Login
- Password recovery
- Contact requests
- Messaging
- Comments
- Uploads
- Search
- Financial actions
- LuckyDraw purchases

Rate limits should consider:
- Account
- IP/network context where appropriate
- Device/session
- Risk level

Do not use a single simplistic global limit for all operations.

---

# 61. SECRET MANAGEMENT

Never commit:
- Private keys
- API keys
- Database passwords
- Production tokens
- Seed phrases

Use environment/configuration and secure secret infrastructure.

Add secret scanning to CI.

If a secret is discovered in repository history or production configuration:
- Rotate it
- Investigate exposure
- Remove future exposure
- Do not merely rename the variable

---

# 62. ERROR HANDLING

Every production operation must have defined failure behavior.

Handle:
- Validation failure
- Authentication failure
- Authorization failure
- Dependency timeout
- Database failure
- Queue failure
- Storage failure
- Network failure
- Duplicate request

Do not use empty catch blocks.

Do not return success after a failed operation.

---

# 63. ASYNCHRONOUS JOBS

Use jobs/queues for appropriate work:
- Media processing
- Notifications
- Video transcoding
- Analytics
- Retries
- External settlement tracking

Jobs require:
- Unique identity/idempotency
- Retry policy
- Maximum attempts
- Dead-letter/failure handling
- Observability

A retry must not duplicate financial operations.

---

# 64. MEDIA PROCESSING

For image/video pipelines:
Upload
→ validate
→ queue
→ process
→ store variants
→ mark ready

Keep state authoritative in backend.

Client must handle:
- Uploading
- Processing
- Ready
- Failed

---

# 65. CACHING

Caching must not bypass:
- Authentication
- Authorization
- Privacy

Do not cache private responses under publicly reusable keys.

Include proper invalidation strategy.

---

# 66. OBSERVABILITY

Implement:
- Structured logs
- Metrics
- Health checks
- Error tracking
- Tracing where appropriate

Do not log:
- Passwords
- Private keys
- Seed phrases
- Raw sensitive financial secrets
- Plaintext E2EE content

Use correlation IDs for distributed operations.

---

# 67. SECURITY TESTING

Test:
- Authentication
- Authorization
- Object ownership
- Input validation
- Rate limits
- Session revocation
- File upload
- Access control
- Financial idempotency

Perform security review before declaring sensitive features complete.

---

# 68. TESTING REQUIREMENTS

Every feature requires appropriate tests.

## Unit tests
Business rules and isolated logic.

## Integration tests
Database, services, queues, external boundaries.

## API tests
Authentication, authorization, validation, response behavior.

## End-to-end tests
Real user flows.

## Regression tests
Previously fixed defects.

## Load tests
High-volume messaging, feeds, media, realtime operations.

Do not delete a failing test simply to get green status.

Understand the failure first.

---

# 69. DEFINITION OF DONE

A feature is DONE only when all applicable conditions are true:

[ ] Requirements understood
[ ] Data model exists
[ ] Migration exists
[ ] API/interface implemented
[ ] Authentication implemented
[ ] Authorization implemented
[ ] Validation implemented
[ ] Business logic implemented
[ ] Persistent storage implemented
[ ] Error handling implemented
[ ] Idempotency implemented where required
[ ] UI implemented where applicable
[ ] Loading state implemented
[ ] Empty state implemented
[ ] Failure state implemented
[ ] Tests added
[ ] Existing tests pass
[ ] Logging/monitoring considered
[ ] Security review completed for sensitive features
[ ] Documentation updated where necessary
[ ] No production mock/stub/fake behavior remains

---

# 70. CROSS-PLATFORM FEATURE PARITY

Maintain a matrix:

Feature | Android | iOS | Web | Windows | macOS | Linux

For every feature record:
- Supported
- Unsupported
- Planned
- Native limitation
- Alternative behavior

Do not place a non-functional button on one platform simply because another platform supports the feature.

If unsupported:
- Hide it appropriately, or
- Clearly state limitation, or
- Implement a legitimate alternative

---

# 71. MIGRATIONS AND BACKWARD COMPATIBILITY

Before changing schemas:
- Inspect existing data
- Write migration
- Consider rollback
- Preserve critical data
- Test upgrade path

Do not modify production schemas manually without migration strategy.

For API changes:
- Consider old clients
- Version when necessary
- Avoid unnecessary breaking changes

---

# 72. DEPENDENCY MANAGEMENT

Before adding a dependency:
- Check maintenance
- Security history
- License
- Bundle/runtime impact
- Necessity

Do not add dependencies merely to avoid writing a small safe utility.

For security-critical functionality:
- Prefer established, maintained libraries.

---

# 73. AI AGENT WORKFLOW FOR EVERY TASK

Use this exact workflow.

## Step 1 — Understand
Read the requested feature.

## Step 2 — Inspect
Find existing relevant code.

## Step 3 — Map
Identify:
- UI
- API
- Service
- Database
- Tests

## Step 4 — Identify gaps
Determine what actually does not exist.

## Step 5 — Plan
Produce a concise implementation plan.

## Step 6 — Implement
Implement complete vertical slices.

## Step 7 — Validate
Run:
- Type checks
- Lint
- Unit tests
- Integration tests
- Relevant build

## Step 8 — Security review
Check:
- Auth
- Authorization
- Input
- Data exposure
- Sensitive logging

## Step 9 — Regression review
Verify related features.

## Step 10 — Report truthfully
Report:
- Files changed
- What was implemented
- Tests run
- Tests not run
- Remaining known limitations

Never say “complete” if known work remains.

---

# 74. FEATURE IMPLEMENTATION TEMPLATE

For every new feature create or follow this checklist.

## FEATURE NAME

### User requirement
What user problem does it solve?

### Actors
Who can use it?

### Permissions
Who can create/read/update/delete?

### Data model
What entities exist?

### State machine
What states exist?

### API
What operations exist?

### UI
What screens/states exist?

### Security
What are the abuse risks?

### Privacy
What data is visible?

### Failure handling
What can fail?

### Tests
What must be tested?

### Monitoring
What operational signals are needed?

### Definition of done
What proves production readiness?

---

# 75. REQUIRED FEATURE-BY-FEATURE EXECUTION LIST

## FOUNDATION
[ ] Configuration
[ ] Environment separation
[ ] Database
[ ] Migrations
[ ] Error framework
[ ] Logging
[ ] Metrics
[ ] Health checks

## IDENTITY
[ ] Account creation
[ ] Login
[ ] Logout
[ ] Password security
[ ] Passkeys
[ ] MFA
[ ] Recovery
[ ] Session management
[ ] Device management
[ ] Username

## COMMUNICATION
[ ] Contact requests
[ ] Private chat
[ ] Message states
[ ] Replies
[ ] Reactions
[ ] Edits
[ ] Deletes
[ ] Forwarding
[ ] Pinning
[ ] Saved items
[ ] Attachments
[ ] Voice messages
[ ] Search

## PRIVACY
[ ] Visibility controls
[ ] Read receipts
[ ] Typing indicators
[ ] Online status
[ ] Last seen
[ ] Blocking
[ ] Disappearing messages
[ ] Contact verification

## GROUPS AND CHANNELS
[ ] Groups
[ ] Roles
[ ] Permissions
[ ] Invitations
[ ] Public/private modes
[ ] Channels
[ ] Broadcast
[ ] Scheduling
[ ] Analytics

## CALLING
[ ] Voice call
[ ] Video call
[ ] Call states
[ ] Network recovery
[ ] Group calls
[ ] Screen sharing where supported

## OFFLINE
[ ] Local transport abstraction
[ ] Local Wi-Fi
[ ] Wi-Fi Direct
[ ] Bluetooth
[ ] Store-and-forward
[ ] Retry
[ ] Deduplication

## SOCIAL
[ ] Profiles
[ ] Friends
[ ] Followers
[ ] Posts
[ ] Comments
[ ] Replies
[ ] Reactions
[ ] Reposts
[ ] Quotes
[ ] Feed
[ ] Stories
[ ] Hashtags
[ ] Trends

## COMMUNITIES
[ ] Communities
[ ] Forums
[ ] Events
[ ] Moderation
[ ] Reports

## VIDEO
[ ] Upload
[ ] Processing
[ ] Short video
[ ] Long video
[ ] Discovery
[ ] Creator tools
[ ] Live streaming
[ ] Replay

## CREATOR ECONOMY
[ ] Eligibility
[ ] Earnings events
[ ] Ledger
[ ] Fraud prevention
[ ] Tips
[ ] Subscriptions
[ ] Premium content
[ ] Payouts

## FINANCE
[ ] Wallet architecture
[ ] Chain adapters
[ ] Asset registry
[ ] Send
[ ] Receive
[ ] Transaction history
[ ] Fees
[ ] Conversion
[ ] P2P
[ ] Disputes
[ ] Staking
[ ] Card integration

## LUCKYDRAW
[ ] Draw configuration
[ ] Ticket purchase
[ ] Payment verification
[ ] Ticket records
[ ] Pricing versions
[ ] Eligibility
[ ] Sales closure
[ ] Dataset locking
[ ] Selection mechanism
[ ] Unique winner policy
[ ] Prize settlement
[ ] Audit
[ ] Geographic/age controls
[ ] Administrative approval

## ADMINISTRATION
[ ] Roles
[ ] Permissions
[ ] User management
[ ] Content moderation
[ ] Finance administration
[ ] LuckyDraw administration
[ ] Compliance
[ ] Security administration
[ ] Audit

---

# 76. FINAL RELEASE GATE

Do not declare ChatApp production-ready until:

## Code
- No critical broken builds
- No known production stubs
- No fake production paths
- No critical TODO implementation gaps

## Security
- Authentication reviewed
- Authorization reviewed
- Secrets managed
- Sensitive logging reviewed
- Financial boundaries reviewed

## Testing
- Core tests pass
- Integration tests pass
- End-to-end critical flows pass

## Operations
- Monitoring exists
- Error visibility exists
- Backup/recovery strategy exists
- Incident procedures exist

## Financial/regulated features
- Compliance gates are explicitly approved
- External providers are real and configured
- No simulated transactions are presented as real

---

# 77. MASTER QUALITY STANDARD

The coding agent must always prefer:

Correctness over speed.
Security over convenience.
Real implementation over demo appearance.
Explicit failure over fake success.
Auditable behavior over hidden behavior.
Least privilege over unrestricted access.
Small verified changes over large unverified rewrites.

---

# 78. FINAL INSTRUCTION TO THE AI CODING AGENT

You are not a demo generator.

You are a production engineering agent.

Your responsibility is to inspect actual code, understand the existing architecture, identify real gaps, and implement complete production paths.

For every feature:

REQUIREMENT
→ ARCHITECTURE
→ DATA MODEL
→ MIGRATION
→ AUTHENTICATION
→ AUTHORIZATION
→ VALIDATION
→ BUSINESS LOGIC
→ API
→ ASYNCHRONOUS PROCESSING
→ UI
→ ERROR HANDLING
→ TESTING
→ MONITORING
→ SECURITY REVIEW

Do not fake any step.

Do not claim implementation without verification.

Do not replace a missing backend with mock data.

Do not bypass security.

Do not create hidden privileged access.

Do not treat financial or regulated systems as ordinary UI features.

Maintain one coherent ChatApp ecosystem while preserving strict boundaries between communication, social data, media, creator earnings, financial assets, LuckyDraw operations, administration, and security.

**The final goal is a real, maintainable, secure, testable, observable, cross-platform production platform — not a prototype that only looks complete.**


---

# 79. PERFORMANCE, SECURITY, AND MULTI-LANGUAGE ARCHITECTURE

ChatApp MUST NOT force every component into one programming language.

Use each technology where its operational characteristics are appropriate.

The final technology choices must depend on the existing repository, team expertise, deployment environment, security review, benchmarks, and maintenance cost.

Do not rewrite working services into another language merely because a language is considered faster.

## 79.1 Recommended responsibility boundaries

### Rust — security-critical and high-performance core components

Rust is a strong candidate for:
- Cryptographic protocol integration
- Secure protocol engines
- Message synchronization engines
- Mesh routing engines
- Bluetooth/Wi-Fi transport abstractions
- High-performance media/data processing
- File processing
- Native cross-platform libraries
- Wallet signing boundaries
- Transaction validation
- Sensitive parsers
- Security-sensitive FFI components

Requirements:
- Prefer established audited cryptographic libraries.
- Never implement custom cryptography.
- Use memory-safe Rust patterns.
- Minimize unsafe code.
- Every unsafe block requires explicit justification and review.
- Fuzz parsers and network protocol implementations.
- Expose small, stable APIs to other languages.

### C++ — media and performance-intensive native integration

C++ may be used where required by:
- Existing WebRTC/native media stacks
- High-performance codecs
- Mature platform libraries
- Camera/audio integrations
- Existing native multimedia dependencies

Requirements:
- Prefer modern C++.
- Avoid manual ownership where safer abstractions exist.
- Use RAII.
- Enable sanitizers in development/CI where practical.
- Treat all network/media input as untrusted.
- Avoid unnecessary custom C++ code when a maintained library already solves the problem.

Do not choose C++ for cryptographic implementation merely for speed.

### Go — scalable network and backend infrastructure

Go is a strong candidate for:
- Realtime gateways
- WebSocket services
- Notification services
- API services
- Queue consumers
- Internal service communication
- Media orchestration
- Presence services
- High-concurrency network services

Requirements:
- Context cancellation must be handled.
- Avoid goroutine leaks.
- Bound queues and concurrency.
- Use timeouts.
- Make retries idempotent.
- Use structured logging.

### Next.js / TypeScript — web application and administrative interfaces

Next.js is suitable for:
- Web application
- Server-rendered public pages where appropriate
- Creator dashboards
- Administrative dashboards
- Internal operational interfaces
- Shared web components

TypeScript requirements:
- Strict type checking.
- Runtime validation at trust boundaries.
- Do not treat TypeScript types as security validation.
- Never expose secrets in browser bundles.
- Keep server-only logic server-only.

### Kotlin — Android

Use Kotlin as the preferred Android application language.

Appropriate responsibilities:
- Android UI
- Platform permissions
- Bluetooth APIs
- Wi-Fi Direct APIs
- Local discovery
- Background service integration
- Native core bindings

### Swift — iOS

Use Swift as the preferred iOS application language.

Appropriate responsibilities:
- iOS UI
- Bluetooth APIs
- Multipeer/local connectivity where available
- Platform permissions
- Background behavior
- Native core bindings

### Python

Python may be used for:
- Offline data analysis
- Internal tools
- Machine learning pipelines
- Moderation analysis
- Operational automation

Do not place security-critical transaction signing or high-volume realtime hot paths in Python without a measured architectural reason.

### SQL

Database logic requires:
- Explicit schema
- Constraints
- Transactions
- Indexes
- Migration history

Business authorization must not depend solely on hidden application assumptions.

---

# 80. SHARED NATIVE CORE ARCHITECTURE

For functionality required across Android, iOS, desktop, and potentially web, prefer a carefully designed shared core.

Conceptual architecture:

ChatApp Applications
├── Android Client (Kotlin)
├── iOS Client (Swift)
├── Web Client (Next.js/TypeScript)
├── Desktop Clients
│
├── Shared Application Protocol
├── Shared Cryptographic Core (Rust where appropriate)
├── Shared Synchronization Core
├── Shared Mesh/Transport Core
└── Platform Adapters

The shared core MUST NOT contain:
- Platform UI
- Platform-specific permissions
- Hardcoded credentials
- Uncontrolled global state

Platform adapters are responsible for:
- Bluetooth APIs
- Wi-Fi APIs
- Background execution
- Notifications
- Audio/video devices
- Platform lifecycle behavior

The shared core is responsible for:
- Protocol state
- Message identity
- Encryption integration
- Routing
- Deduplication
- Synchronization

---

# 81. OFFLINE MULTI-HOP DEVICE MESH COMMUNICATION

The requested functionality is:

A user may have no mobile internet and no normal Wi-Fi internet.

Users should still communicate using nearby devices.

Example:
Device A cannot directly reach Device Z.
Intermediate devices B through Y can relay encrypted data.
Therefore:

A → B → C → D → ... → Z

The system must support a MULTI-HOP MESH architecture where platform capabilities permit it.

Important:

A phone hotspot does not automatically create a 10-kilometre network.

Bluetooth and ordinary Wi-Fi radios have limited range, and mobile operating systems impose restrictions.

A 10-kilometre communication path can theoretically be formed by many devices if there is a sufficiently dense chain of participating devices, each device can communicate with nearby peers, routing works correctly, and the operating systems permit the required transport behavior.

This MUST be treated as a best-effort mesh network, not as a guaranteed 10-kilometre radio connection.

The implementation must never claim unlimited range.

---

# 82. MULTI-HOP MESH DESIGN

Conceptual layers:

Application
↓
Message Protocol
↓
Encryption
↓
Routing
↓
Transport Abstraction
├── Bluetooth transport
├── Bluetooth Low Energy transport
├── Wi-Fi Direct transport
├── Local Wi-Fi transport
├── Platform-supported peer-to-peer transport
└── Internet transport when available

The application layer must not need to know whether a message traveled through:
- Internet
- Bluetooth
- Wi-Fi Direct
- Local hotspot
- Multiple relay devices

Use a common transport interface.

Example conceptual interface:

Transport:
- discoverPeers()
- connect(peer)
- disconnect(peer)
- send(packet)
- receive(packet)
- connectionState()
- transportCapabilities()

Do not expose a fake transport implementation in production.

---

# 83. PEER DISCOVERY

Peer discovery must be designed separately for each transport.

Potential methods:
- Bluetooth discovery
- BLE advertisements
- Local Wi-Fi discovery
- Wi-Fi Direct discovery
- Platform-supported peer discovery

Discovery requirements:
- Do not expose unnecessary personal information.
- Use rotating or privacy-preserving identifiers where appropriate.
- Avoid broadcasting permanent user IDs.
- Rate limit discovery.
- Avoid continuous battery-heavy scanning.
- Respect platform permissions.

A nearby device must not automatically become a trusted contact.

Discovery and trust are separate.

---

# 84. MESH NODE IDENTITY

Each device participating in the mesh requires a device-level identity.

Do not use:
- MAC addresses as permanent user identity
- Bluetooth device names as identity
- IP addresses as identity

Use cryptographically authenticated device identities integrated with the account/device model.

Separate:
- User identity
- Account identity
- Device identity
- Temporary transport identifier

A temporary radio identifier should not reveal a permanent account identifier when avoidable.

---

# 85. MESH RELAY PRINCIPLE

A relay device should transport encrypted packets without needing to read the message content.

Conceptually:

Sender encrypts payload for recipient/session.

Relay devices:
- Receive packet
- Validate packet structure
- Apply routing rules
- Deduplicate
- Forward when permitted

Relays should not:
- Decrypt private message content
- Modify authenticated payloads
- Pretend to be the sender

Relay participation must be controlled by user settings and resource policy.

Possible relay settings:
- Relay disabled
- Relay while app active
- Relay while charging
- Relay only on external power
- Relay only with user approval

Actual background relay capability depends on the operating system.

Do not promise identical relay behavior on Android and iOS without verifying platform limitations.

---

# 86. MESH ROUTING

Do not begin with an unnecessarily complex routing algorithm.

The routing system must support:
- Neighbor knowledge
- Packet identifiers
- Duplicate suppression
- Hop limits
- Expiration
- Loop prevention
- Retry behavior

## 86.1 Packet requirements

Every mesh packet should include appropriate metadata such as:
- Protocol version
- Packet ID
- Sender device/session information
- Destination information in privacy-preserving form where possible
- Creation timestamp or monotonic lifetime mechanism
- TTL/hop limit
- Message class
- Authentication/integrity information

Do not expose plaintext message content in routing metadata.

## 86.2 Duplicate prevention

Mesh networks can produce duplicate packets.

Maintain bounded recently-seen packet identifiers.

Rules:
- Do not forward indefinitely.
- Do not allow unlimited memory growth.
- Expire deduplication records.

## 86.3 TTL

Every forwarded packet must have a bounded lifetime.

For example:
TTL > 0:
  process
  decrement before forwarding

TTL == 0:
  do not forward

The exact value must be configurable and tested.

Do not use unlimited flooding.

---

# 87. ROUTING STRATEGIES

The agent must benchmark and select appropriate routing strategies.

Possible strategies:

## Controlled flooding

A packet is forwarded to eligible peers except where already seen.

Advantages:
- Simple
- Useful in small/dynamic networks

Disadvantages:
- Battery use
- Bandwidth amplification
- Congestion

## Store-and-forward

Nodes temporarily retain packets until a suitable peer appears.

Advantages:
- Works with intermittent connectivity

Disadvantages:
- Storage requirements
- Delayed delivery

## Destination-aware routing

Nodes maintain information about known paths.

Advantages:
- Lower unnecessary traffic when routes are accurate

Disadvantages:
- Route maintenance complexity

The initial production design should favor correctness, bounded resources, and explicit testing over theoretical sophistication.

---

# 88. STORE-AND-FORWARD MESH

A message may move through devices over time.

Example:

A sends message to Z.

A meets B.
B stores encrypted packet.

Later B meets C.
B forwards packet.

Later C meets D.

Eventually a device meets Z.

This is delay-tolerant networking behavior.

Requirements:
- Persistent encrypted queue
- Expiration
- Storage quota
- Deduplication
- Delivery acknowledgement
- Retry policy
- User-configurable resource controls

Relays must not become unlimited storage providers.

---

# 89. MESH MESSAGE DELIVERY STATES

Use explicit states:

CREATED
→ ENCRYPTED
→ QUEUED
→ DISCOVERING_ROUTE
→ RELAYING
→ DELIVERED_TO_DESTINATION
→ ACKNOWLEDGED

Failure/terminal states may include:
EXPIRED
CANCELED
FAILED

Do not equate:
“sent to first relay”
with
“delivered to recipient”.

The UI must clearly distinguish:
- Queued
- Relayed
- Delivered
- Read

---

# 90. OFFLINE VOICE MESSAGES

Voice messages are practical for store-and-forward mesh communication.

Flow:

Record
→ Encode
→ Encrypt
→ Packetize
→ Queue
→ Multi-hop relay
→ Reassemble
→ Authenticate
→ Decrypt
→ Playback

Requirements:
- Chunking for large media
- Missing chunk detection
- Integrity verification
- Retry
- Expiration
- Storage limits

Do not assume every relay can carry unlimited media.

---

# 91. OFFLINE AUDIO CALLS

Real-time multi-hop audio calling is substantially more difficult than messaging.

The implementation must distinguish:

A. Store-and-forward voice messages
B. Near-real-time audio calls
C. Internet calls

For mesh audio calls, requirements include:
- Low latency
- Continuous route availability
- Sufficient bandwidth
- Jitter handling
- Packet loss handling
- Adaptive bitrate

Every additional relay hop can increase:
- Latency
- Packet loss
- Battery use
- Instability

Therefore the application MUST NOT guarantee a high-quality audio call across an arbitrary 10-kilometre multi-hop Bluetooth chain.

Implement capability negotiation.

Before starting a mesh call:
- Discover possible route/transport
- Estimate route quality where possible
- Select codec/bitrate
- Monitor quality
- Downgrade when necessary

Possible result:
- Full-quality audio
- Reduced-bitrate audio
- Voice-message fallback

The user must receive truthful connection state.

---

# 92. OFFLINE VIDEO CALLS

Real-time multi-hop video calling is much more demanding than audio.

Requirements:
- High bandwidth
- Low latency
- Continuous connectivity
- Video encoding
- Adaptive bitrate
- Congestion control

A 500-device multi-hop path is not automatically suitable for real-time video.

The coding agent MUST implement realistic capability negotiation:

VIDEO_SUPPORTED
AUDIO_ONLY
MESSAGING_ONLY

A long, congested, or unstable mesh route may support:
- Messages
- Voice messages

while not supporting:
- Real-time video

Do not show a successful “connected video call” unless media is actually flowing.

Possible fallback:

Video call requested
→ route quality test
→ video available?
   YES → start video
   NO → audio available?
       YES → offer audio call
       NO → offer voice message/messaging

---

# 93. MULTI-HOP CALL CONTROL

Call signaling must work separately from media.

Example:

CALL_INVITE
→ multi-hop routed signaling
→ recipient receives invite
→ ACCEPT
→ route negotiation
→ media transport selected
→ CONNECTED

States:
IDLE
OUTGOING
ROUTING
RINGING
NEGOTIATING
CONNECTED
RECONNECTING
ENDED
FAILED

Do not confuse successful signaling with successful media connectivity.

---

# 94. HOTSPOT AND LOCAL WI-FI

Support local connectivity when technically available.

Possible architecture:
- One device provides local Wi-Fi access
- Nearby devices join
- Devices communicate locally
- Local discovery identifies eligible peers
- Mesh routing extends communication through other links

Important:
A mobile hotspot normally provides a local network around the hotspot owner.

It does not automatically bridge to another hotspot several kilometres away.

A multi-hop system requires actual links between participating nodes.

Potential topology:

Device A
↔ Wi-Fi/Bluetooth ↔ Device B
↔ Wi-Fi/Bluetooth ↔ Device C
↔ Wi-Fi/Bluetooth ↔ Device D

The routing layer must support heterogeneous links.

---

# 95. BLUETOOTH TRANSPORT

Bluetooth implementation must consider:
- Classic Bluetooth
- Bluetooth Low Energy
- Platform support
- Discovery restrictions
- Connection limits
- Throughput
- Battery consumption

Do not assume:
- Every phone can maintain unlimited Bluetooth connections.
- Background discovery works indefinitely.
- iOS and Android behave identically.

Test actual supported device behavior.

---

# 96. WIFI DIRECT AND PEER-TO-PEER

Where supported, Wi-Fi Direct or platform peer-to-peer technology can provide higher bandwidth than Bluetooth for nearby devices.

Potential uses:
- Large file transfer
- Media transfer
- Nearby high-bandwidth calls

Requirements:
- Permission management
- Connection lifecycle
- Group ownership behavior
- IP/address handling
- Reconnection
- Platform compatibility testing

Do not make a feature visible on a platform until it has a real supported implementation.

---

# 97. TRANSPORT SELECTION ENGINE

Create a transport manager.

Priority must not be permanently hardcoded without configuration.

Example policy:

1. Existing direct secure local link
2. Best available local high-bandwidth link
3. Bluetooth/local low-bandwidth link
4. Store-and-forward relay
5. Internet transport

Selection should consider:
- Availability
- User preference
- Cost
- Battery
- Bandwidth
- Latency
- Privacy
- Security

All transports must provide equivalent authentication and integrity guarantees.

A faster transport must not silently weaken security.

---

# 98. MULTIPATH COMMUNICATION

Where architecture permits, the protocol may support multiple paths.

Example:
- Some chunks via local Wi-Fi
- Some control messages via Bluetooth
- Internet becomes available later

Multipath requires:
- Message identity
- Ordering/reassembly
- Duplicate handling
- Integrity verification

Do not implement multipath by blindly sending every packet everywhere.

This can create battery and congestion attacks.

---

# 99. MESH SECURITY THREAT MODEL

The coding agent MUST explicitly consider hostile mesh nodes.

Threats include:
- Malicious relay
- Packet dropping
- Replay
- Packet flooding
- Route manipulation
- Sybil attacks
- Metadata collection
- Battery exhaustion
- Storage exhaustion

Mitigations may include:
- Authenticated packets
- Replay protection
- TTL
- Rate limits
- Storage quotas
- Peer reputation/risk controls designed carefully
- Resource limits
- User consent

Do not assume every nearby device is trustworthy.

---

# 100. MESH PRIVACY

A relay should learn the minimum necessary information.

Do not broadcast:
- Full contact list
- Private messages
- Passwords
- Wallet keys
- Seed phrases

Minimize routing metadata where technically practical.

Privacy design must consider that a nearby device may observe:
- Packet timing
- Packet size
- Neighbor presence

Do not falsely claim that mesh communication hides all metadata.

---

# 101. BATTERY AND RESOURCE MANAGEMENT

Mesh networking can consume significant resources.

Implement:
- Scan intervals
- Adaptive scanning
- Connection limits
- Queue limits
- Relay quotas
- Battery-aware behavior
- Charging-aware behavior

Never run unrestricted scanning loops.

Do not wake devices continuously without a justified platform-supported mechanism.

---

# 102. LARGE MESH SCALE

The example of 500 devices across approximately 10 kilometres requires realistic design.

The coding agent must not test only two devices and claim 500-device readiness.

Create test scenarios for:
- 2 nodes
- 10 nodes
- 50 nodes
- 100 nodes
- 500 simulated protocol nodes in controlled tests

However:

A simulated protocol topology is NOT proof that 500 real mobile devices will operate identically.

Production validation requires:
- Real device testing
- Different manufacturers
- Android versions
- iOS versions
- Mixed transports
- Background behavior
- Battery testing
- Mobility testing

Document measured limits.

---

# 103. WEB AND MESH LIMITATIONS

Web browsers generally have different access to:
- Bluetooth
- Wi-Fi Direct
- Background execution
- Peer discovery

Do not promise complete native mesh parity on web.

Maintain the feature parity matrix.

Example:

Messaging via internet:
Android: Yes
iOS: Yes
Web: Yes

Bluetooth multi-hop relay:
Android: Depends on tested implementation
iOS: Depends on platform restrictions
Web: Likely restricted/not equivalent

Every platform must implement the same product semantics where possible, but native hardware features may require different implementations.

---

# 104. MESH PROTOCOL TESTING

Test:

## Correctness
- Packet delivery
- Multi-hop forwarding
- Duplicate suppression
- TTL expiration
- Reassembly

## Failure
- Relay disappears
- Recipient disappears
- Packet corruption
- Storage full
- Connection loss

## Security
- Replay
- Malformed packet
- Oversized packet
- Flooding
- Unauthorized injection

## Scale
- Large topology simulation
- Queue pressure
- Routing churn

## Real devices
- Android
- iOS
- Different manufacturers
- Different operating system versions

---

# 105. WEBRTC AND MEDIA ARCHITECTURE

For internet-based real-time audio/video, use established real-time media technology where appropriate.

Separate:
- Signaling
- Session negotiation
- Media transport
- Media encryption
- NAT traversal
- Relay infrastructure

Do not invent a custom real-time media protocol unless there is a compelling, reviewed reason.

For offline mesh:
Do not assume the same internet media architecture will automatically work.

Mesh media requires separate transport capability and route-quality management.

---

# 106. BACKEND LANGUAGE ALLOCATION EXAMPLE

This is a starting architecture, not an absolute rule.

## Rust
- Shared secure core
- Mesh engine
- Protocol parser
- Crypto integration
- Wallet signing boundary

## Go
- Realtime gateway
- Presence
- Notification workers
- High-concurrency backend services

## TypeScript/Next.js
- Web application
- Creator dashboard
- Admin console
- Web API edge/BFF where appropriate

## Kotlin
- Android application
- Android transport adapters

## Swift
- iOS application
- iOS transport adapters

## C++
- Existing native media/codec integration where required

## SQL
- Relational persistence and transactional integrity

The architecture must use stable contracts between components.

Do not create unnecessary network microservices simply because multiple languages are used.

A modular monolith may be preferable in some domains until scale justifies service separation.

---

# 107. PERFORMANCE ENGINEERING

Performance decisions must be measured.

Establish benchmarks for:
- Message send latency
- Realtime connection capacity
- Feed generation
- Search
- Media upload
- Video processing
- Mesh forwarding
- Wallet transaction preparation

Measure:
- p50 latency
- p95 latency
- p99 latency
- Error rate
- CPU
- Memory
- Network
- Battery for mobile features

Do not optimize based only on assumptions.

---

# 108. SECURITY ARCHITECTURE BY LANGUAGE

Every language component must follow equivalent security standards.

## Rust
- Dependency auditing
- Minimal unsafe
- Fuzzing

## C++
- Sanitizers
- Memory safety review
- Fuzzing
- Strict ownership

## Go
- Race detection
- Context handling
- Timeout handling

## TypeScript
- Strict types
- Runtime validation
- XSS/CSRF protections appropriate to architecture

## Kotlin/Swift
- Secure storage
- Permission boundaries
- Platform lifecycle testing

Security is an architectural property, not a language name.

Using Rust does not automatically make an insecure design secure.

---

# 109. CRYPTO WALLET AND MESH ABSOLUTE SEPARATION

The offline mesh transport MUST NOT become a trusted signing environment.

Wallet private keys:
- Remain protected in the wallet security boundary.
- Must never be included in mesh relay packets.
- Must never be exposed to relay nodes.

A mesh message may carry a transaction proposal or public transaction data, but private signing material must remain protected.

---

# 110. ADMIN CONTROL OF MESH FEATURES

Administrators may configure operational policy but must not gain the ability to decrypt private mesh messages.

Allowed administrative controls may include:
- Feature enablement
- Version requirements
- Abuse limits
- Network protocol deprecation

Sensitive changes require:
- Authorization
- Audit logging
- Rollout plan
- Compatibility testing

Do not allow administrators to secretly rewrite user messages or impersonate devices.

---

# 111. EXPANDED DEFINITION OF DONE FOR OFFLINE MESH

A mesh feature is DONE only when:

[ ] Real transport adapter exists
[ ] Peer discovery exists
[ ] Authentication exists
[ ] Packet integrity exists
[ ] Encryption integration exists
[ ] Routing exists
[ ] TTL exists
[ ] Duplicate prevention exists
[ ] Store-and-forward exists where claimed
[ ] Resource limits exist
[ ] Battery controls exist
[ ] Failure states exist
[ ] Security tests exist
[ ] Multi-hop tests exist
[ ] Real-device tests exist
[ ] Platform limitations are documented
[ ] UI does not overstate capabilities

---

# 112. MASTER REQUIREMENT: COMPLETE FEATURE DOCUMENTATION

For EVERY ChatApp feature, the coding agent must document:

1. Feature purpose
2. User flow
3. Actors
4. Permissions
5. Privacy
6. Data model
7. Database constraints
8. State machine
9. API
10. Realtime events
11. Background jobs
12. Client UI
13. Loading state
14. Empty state
15. Error state
16. Offline behavior
17. Security threats
18. Validation
19. Rate limiting
20. Audit requirements
21. Tests
22. Performance requirements
23. Monitoring
24. Platform support
25. Definition of done

No feature should be represented only by a button or screen.

---

# 113. FINAL EXPANDED PLATFORM REQUIREMENTS

The complete ChatApp platform must support, where implemented and legally/technically appropriate:

## COMMUNICATION
- Private messaging
- Encrypted communication where claimed
- Groups
- Channels
- Communities
- Voice messages
- Audio calls
- Video calls
- File sharing

## LOCAL/OFFLINE COMMUNICATION
- Bluetooth transport where supported
- Local Wi-Fi transport where supported
- Wi-Fi Direct/peer-to-peer where supported
- Multi-hop routing
- Store-and-forward
- Relay controls
- Audio/video capability negotiation
- Messaging fallback

## SOCIAL
- Profiles
- Friends
- Followers
- Posts
- Comments
- Threads
- Reactions
- Shares
- Stories
- Search
- Hashtags
- Trends

## VIDEO
- Short video
- Long video
- Discovery
- Creator tools
- Live streaming

## CREATOR ECONOMY
- Earnings
- Ledger
- Tips
- Subscriptions
- Payout workflows
- Fraud controls

## FINANCE
- Multichain wallet
- Asset management
- Conversion
- P2P
- Staking
- Card integration through legitimate providers

## LUCKYDRAW
- Daily draws
- Monthly draws
- Ticketing
- Configured pricing
- Auditable eligibility
- Auditable winner selection
- Prize accounting
- Compliance gates

## PLATFORM ENGINEERING
- Rust where appropriate
- C++ where appropriate
- Go where appropriate
- Next.js/TypeScript for web where appropriate
- Kotlin for Android
- Swift for iOS
- Shared secure/native cores where justified
- Strict API contracts
- Cross-platform parity tracking

---

# 114. FINAL INSTRUCTION FOR THIS EXPANDED ARCHITECTURE

Do not implement the multi-device offline network as a demo animation.

A real implementation requires:

REAL DEVICE DISCOVERY
→ REAL LOCAL TRANSPORT
→ AUTHENTICATED PEERS
→ ENCRYPTED/AUTHENTICATED PACKETS
→ REAL ROUTING
→ MULTI-HOP FORWARDING
→ DEDUPLICATION
→ TTL
→ STORE-AND-FORWARD
→ DELIVERY ACKNOWLEDGEMENT
→ RESOURCE LIMITS
→ SECURITY TESTING
→ REAL DEVICE VALIDATION

Likewise, do not implement “offline calling” as a fake call screen.

A real call requires:
- Real signaling
- Real transport
- Actual media packets
- Actual audio/video processing
- Connection quality handling
- Real failure detection

If the mesh cannot support video, the system must say so and offer a legitimate fallback.

Truthful capability reporting is mandatory.

The goal is not to claim that ChatApp can communicate infinitely far without infrastructure.

The goal is to build a real, secure, multi-hop, delay-tolerant, local-device communication architecture that can extend communication distance through participating devices when suitable physical links and platform capabilities exist.


# 115. MASTER PLATFORM FEATURE TREE — COMPLETE PRODUCT MAP

This is the mandatory architecture map for every requested ChatApp feature.

```text
CHATAPP PLATFORM
│
├── FOUNDATION
│   ├── Configuration
│   ├── Environments
│   ├── Database & Migrations
│   ├── Cache & Queues
│   ├── Object Storage
│   ├── API Contracts
│   └── Shared SDKs
│
├── IDENTITY & ACCOUNT
│   ├── Registration
│   ├── Login
│   ├── Username & Profile
│   ├── Sessions & Devices
│   ├── MFA & Passkeys
│   ├── Recovery
│   └── Verification
│
├── SECURITY
│   ├── IDENTITY
│   │   ├── Authentication
│   │   ├── Sessions
│   │   ├── MFA
│   │   ├── Passkeys
│   │   └── Device Trust
│   ├── APPLICATION
│   │   ├── Authorization
│   │   ├── Validation
│   │   ├── Rate Limits
│   │   ├── Abuse Prevention
│   │   ├── Secure APIs
│   │   └── Monitoring
│   ├── DATA & PRIVACY
│   │   ├── Encryption in Transit
│   │   ├── Encryption at Rest
│   │   ├── End-to-End Encryption
│   │   ├── Key Lifecycle
│   │   ├── Data Minimization
│   │   └── Retention / Deletion
│   ├── FINANCIAL
│   │   ├── Wallet
│   │   ├── Transactions
│   │   ├── Signing Boundary
│   │   ├── Ledger
│   │   ├── Idempotency
│   │   ├── Approval
│   │   └── Fraud / Risk
│   └── INFRASTRUCTURE
│       ├── Secrets
│       ├── Dependencies
│       ├── CI/CD
│       ├── Network Security
│       ├── Backups
│       └── Disaster Recovery
│
├── PRIVATE COMMUNICATION
│   ├── Contact Discovery & Requests
│   ├── Private Chat
│   ├── E2EE / Privacy Modes
│   ├── Multi-Device Sync
│   ├── Replies, Reactions, Forwarding
│   ├── Voice Messages & Files
│   ├── Disappearing Messages
│   └── Search
│
├── GROUPS, CHANNELS & COMMUNITIES
│   ├── Groups
│   ├── Channels & Broadcast
│   ├── Communities
│   ├── Forums & Topics
│   ├── Events
│   ├── Roles & Permissions
│   └── Moderation
│
├── CALLING & REALTIME MEDIA
│   ├── Voice Calls
│   ├── Video Calls
│   ├── Group Calls
│   ├── Screen Sharing
│   ├── Voice Rooms
│   └── Recovery / Network Adaptation
│
├── OFFLINE & MESH NETWORK
│   ├── Bluetooth / BLE
│   ├── Local Wi-Fi
│   ├── Wi-Fi Direct / P2P
│   ├── Hotspot / Local Networks
│   ├── Peer Discovery
│   ├── Device Identity
│   ├── Multi-Hop Routing
│   ├── Relay Nodes
│   ├── Store-and-Forward
│   ├── TTL & Deduplication
│   ├── Offline Messaging
│   ├── Voice Messages
│   └── Audio / Video Capability Negotiation
│
├── SOCIAL NETWORK
│   ├── Profiles
│   ├── Friends / Followers
│   ├── Posts
│   ├── Comments / Threads
│   ├── Reactions / Shares / Reposts / Quotes
│   ├── Feed
│   ├── Stories
│   ├── Hashtags & Trends
│   └── Search / Discovery
│
├── VIDEO & LIVE
│   ├── Short Video
│   ├── Long Video
│   ├── Upload & Transcoding
│   ├── Captions
│   ├── Discovery & Recommendations
│   ├── Creator Tools
│   ├── Live Streaming
│   ├── Live Chat
│   └── Replay
│
├── CREATOR ECONOMY
│   ├── Eligibility
│   ├── Monetization Programs
│   ├── Advertising Revenue
│   ├── Tips
│   ├── Subscriptions / Memberships
│   ├── Premium Content
│   ├── Earnings Events
│   ├── Earnings Ledger
│   ├── Fraud Detection
│   └── Payouts
│
├── FINANCE & CRYPTO
│   ├── Multichain Wallet
│   ├── Assets & Networks
│   ├── Send / Receive
│   ├── Transaction History
│   ├── Conversion
│   ├── P2P
│   ├── Staking
│   ├── Crypto Card Integration
│   └── Financial Ledger
│
├── LUCKYDRAW
│   ├── Daily Draw
│   ├── Monthly Draw
│   ├── Draw Configuration
│   ├── Ticket Pricing
│   ├── Ticket Purchase
│   ├── Payment Verification
│   ├── Eligibility
│   ├── Unique User Rules
│   ├── Sales Closure & Dataset Freeze
│   ├── Random Selection
│   ├── Prize Pool & Settlement
│   ├── Results
│   ├── Audit
│   └── Compliance Controls
│
├── MODERATION & TRUST
│   ├── Reports
│   ├── Content Review
│   ├── Spam Prevention
│   ├── Blocks / Restrictions / Suspension
│   └── Appeals & Enforcement Audit
│
├── ADMINISTRATION
│   ├── User Administration
│   ├── Content & Community Administration
│   ├── Finance Administration
│   ├── LuckyDraw Administration
│   ├── Compliance
│   ├── Security Administration
│   ├── Roles & Permissions
│   ├── Approval Workflows
│   └── Audit Logs
│
└── PLATFORM OPERATIONS
    ├── Logging
    ├── Metrics
    ├── Tracing
    ├── Health Checks
    ├── Alerts
    ├── Backups
    ├── Disaster Recovery
    ├── Scaling
    └── Release Management
```

# 116. SECURITY MASTER TREE

```text
SECURITY
                        │
       ┌────────────────┼────────────────┐
       │                │                │
   IDENTITY         APPLICATION       FINANCIAL
       │                │                │
Authentication     Authorization      Wallet
Sessions           Validation         Transactions
MFA                Rate Limits        Signing Boundary
Passkeys           Secure APIs        Ledger
Recovery           Monitoring         Idempotency
Device Trust       Abuse Controls     Approval
       │                │                │
       └────────────────┼────────────────┘
                        │
              DATA & INFRASTRUCTURE
                        │
       ┌────────────────┼────────────────┐
       │                │                │
    PRIVACY           DATA         INFRASTRUCTURE
       │                │                │
E2EE              Encryption        Secrets
Metadata          Access Control     Dependencies
Minimization      Retention          CI/CD
Deletion          Backups            Disaster Recovery
```

Every branch requires actual implementation, server-side enforcement where applicable, tests, monitoring and auditability.

# 117. WHERE COMPETITOR FEATURES BELONG

| Platform inspiration | ChatApp section | Capability categories |
|---|---|---|
| TorChat | Private Communication / Privacy | Privacy-oriented identity, direct communication, metadata minimization |
| SimpleX | Identity / Private Communication / Privacy | Private contact methods, temporary links, revocation, metadata reduction |
| Session | Private Communication / Privacy | Privacy modes, encrypted messaging, private identifiers |
| Briar | Offline & Mesh Network | Bluetooth/local communication, store-and-forward, delay-tolerant communication |
| Facebook | Social / Communities / Creator | Profiles, friends, feed, posts, groups, events, stories, creator/business tools |
| Telegram | Messaging / Groups / Channels | Multi-device messaging, channels, large communities, files, topics, public discovery |
| WhatsApp | Messaging / Calls / Privacy | Private messaging, voice messages, calls, groups, privacy controls |
| X/Twitter | Social / Discovery | Short posts, threads, reposts, quote posts, hashtags, trends |
| TikTok | Video / Creator Economy | Short video, discovery, recommendations, live, creator tools |
| imo | Calling / Media | Voice/video calling, network adaptation, low-bandwidth optimization |

This is an independent feature architecture. Never copy proprietary source code, private protocols, branding or protected assets.

# 118. DETAILED COMPETITOR FEATURE INTEGRATION

## TorChat-inspired concepts → Private Communication and Privacy
Use historical lessons only: privacy-oriented communication, minimized unnecessary identity exposure and direct private interaction. Do not depend on the abandoned implementation or copy its protocol.

## SimpleX-inspired concepts → Identity, Private Communication and Privacy
Implement privacy-conscious contact establishment, revocable contact links, optional temporary links and separation of public social identity from private communication identifiers.

## Session-inspired concepts → Private Communication and Privacy
Support privacy modes and private communication identifiers that do not force the user's public social identity to become the communication identity.

## Briar-inspired concepts → Offline & Mesh Network
This is the primary home of Bluetooth, local Wi-Fi, Wi-Fi Direct where supported, peer discovery, store-and-forward, encrypted relays, multi-hop routing and delay-tolerant communication.

## Facebook-inspired categories → Social, Communities, Video and Creator Economy
Profiles, friends, posts, feed, comments, reactions, sharing, stories, groups, communities, events, business/creator tools and monetization workflows.

## Telegram-inspired categories → Communication, Groups and Channels
Fast multi-device messaging, large groups, broadcast channels, files, replies, reactions, pinned content, topics and public/private discovery.

## WhatsApp-inspired categories → Communication, Calls and Privacy
Simple private messaging, encrypted communication modes where implemented, voice messages, reliable voice/video calling, groups, privacy controls and multi-device behavior.

## X/Twitter-inspired categories → Social and Discovery
Short public posts, threads, reposts, quote posts, follow graph, hashtags, trends and real-time public conversation.

## TikTok-inspired categories → Video and Creator Economy
Short-form video, recording/editing, discovery, recommendations, live streaming, creator analytics and monetization.

## imo-inspired categories → Calling and Media
Efficient voice/video communication, adaptive bitrate, reconnection, low-bandwidth behavior and connection quality management.

# 119. FEATURE TREE: COMMUNICATION

```text
COMMUNICATION
│
├── PRIVATE CHAT
│   ├── Text
│   ├── Emoji
│   ├── Reactions
│   ├── Replies
│   ├── Mentions
│   ├── Edit / Delete
│   ├── Forward
│   ├── Pin / Save
│   └── Message States
│
├── MEDIA
│   ├── Images
│   ├── Video
│   ├── Files
│   ├── Documents
│   └── Voice Messages
│
├── PRIVACY
│   ├── E2EE Modes
│   ├── Read Receipts
│   ├── Typing
│   ├── Online Status
│   ├── Last Seen
│   ├── Blocking
│   └── Disappearing Messages
│
├── GROUPS
│   ├── Members
│   ├── Roles
│   ├── Permissions
│   ├── Invitations
│   └── Moderation
│
├── CHANNELS
│   ├── Broadcast
│   ├── Subscribers
│   ├── Administrators
│   ├── Scheduling
│   └── Analytics
│
└── CALLS
    ├── Audio
    ├── Video
    ├── Group Calls
    ├── Screen Sharing
    └── Connection Recovery
```

# 120. FEATURE TREE: SOCIAL

```text
SOCIAL
│
├── IDENTITY
│   ├── Profile
│   ├── Username
│   ├── Bio
│   ├── Avatar
│   └── Verification
├── CONNECTIONS
│   ├── Friends
│   ├── Followers
│   ├── Following
│   └── Blocks
├── CONTENT
│   ├── Posts
│   ├── Images
│   ├── Video
│   ├── Links
│   ├── Polls
│   └── Stories
├── CONVERSATION
│   ├── Comments
│   ├── Replies
│   ├── Threads
│   └── Mentions
├── DISTRIBUTION
│   ├── Feed
│   ├── Following
│   ├── Friends
│   ├── Discover
│   └── Trending
└── DISCOVERY
    ├── Search
    ├── Hashtags
    ├── Topics
    └── Recommendations
```

# 121. FEATURE TREE: FINANCE

```text
FINANCE
│
├── MULTICHAIN WALLET
│   ├── Accounts
│   ├── Addresses
│   ├── Assets
│   ├── Networks
│   ├── Send / Receive
│   ├── Fees
│   ├── Signing
│   └── Transaction Tracking
├── CONVERSION
│   ├── Quote
│   ├── Rate
│   ├── Fee
│   ├── Expiration
│   ├── Confirmation
│   └── Settlement
├── P2P
│   ├── Listings
│   ├── Orders
│   ├── Limits
│   ├── Payment Workflow
│   ├── Disputes
│   └── Settlement
├── STAKING
│   ├── Terms
│   ├── Stake
│   ├── Rewards
│   ├── Locking
│   └── Unstaking
├── CARD
│   ├── Provider Integration
│   ├── Virtual / Physical Card
│   ├── Freeze
│   ├── Limits
│   └── Transaction History
└── LEDGER
    ├── Creator Earnings
    ├── Wallet Operations
    ├── Conversion
    ├── P2P
    ├── Staking
    ├── LuckyDraw
    └── Payouts
```

# 122. FEATURE TREE: LUCKYDRAW

```text
LUCKYDRAW
│
├── DRAW TYPES
│   ├── Daily
│   └── Monthly
├── CONFIGURATION
│   ├── Ticket Price
│   ├── Sales Period
│   ├── Eligibility
│   ├── Winner Policy
│   ├── Prize Allocation
│   └── Geographic / Age Rules
├── TICKETS
│   ├── Purchase
│   ├── Payment Verification
│   ├── Ticket Number
│   ├── Immutable Price Record
│   └── Eligibility
├── DRAW EXECUTION
│   ├── Close Sales
│   ├── Finalize Payments
│   ├── Freeze Dataset
│   ├── Select Winners
│   ├── Validate Winners
│   └── Publish Results
├── PRIZES
│   ├── Prize Pool
│   ├── Allocation
│   ├── Settlement
│   └── Tax / Compliance Handling
└── GOVERNANCE
    ├── Admin Roles
    ├── Approval
    ├── Audit
    ├── Legal Controls
    └── Dispute Handling
```

Paid-entry random-prize systems can be regulated. Production activation requires jurisdiction, age, licensing, consumer-protection and tax/compliance review.

# 123. FEATURE TREE: CREATOR ECONOMY

```text
CREATOR ECONOMY
│
├── ELIGIBILITY
│   ├── Program Rules
│   ├── Account Requirements
│   └── Compliance
├── MONETIZATION
│   ├── Advertising Revenue
│   ├── Tips
│   ├── Subscriptions
│   ├── Memberships
│   └── Premium Content
├── EARNINGS
│   ├── Raw Events
│   ├── Validation
│   ├── Fraud Checks
│   ├── Calculation
│   ├── Ledger
│   └── Available Balance
├── PAYOUTS
│   ├── Request
│   ├── Review
│   ├── Processing
│   ├── Completion
│   └── Failure / Reversal
└── CREATOR TOOLS
    ├── Analytics
    ├── Content Management
    ├── Audience Insights
    └── Monetization Dashboard
```

# 124. FEATURE TREE: OFFLINE MULTI-HOP MESH

```text
OFFLINE & MESH NETWORK
│
├── TRANSPORTS
│   ├── Bluetooth
│   ├── BLE
│   ├── Local Wi-Fi
│   ├── Wi-Fi Direct
│   ├── Hotspot / Local Network
│   └── Platform P2P
│
├── PEERS
│   ├── Discovery
│   ├── Temporary Transport IDs
│   ├── Device Identity
│   └── Authentication
│
├── ROUTING
│   ├── Neighbor Knowledge
│   ├── Multi-Hop Forwarding
│   ├── Relay Nodes
│   ├── TTL
│   ├── Loop Prevention
│   └── Deduplication
│
├── DELAY TOLERANCE
│   ├── Persistent Queue
│   ├── Store-and-Forward
│   ├── Retry
│   ├── Expiration
│   └── Delivery Acknowledgement
│
├── COMMUNICATION
│   ├── Text
│   ├── Files
│   ├── Voice Messages
│   ├── Audio Calls (route-quality dependent)
│   └── Video Calls (route-quality dependent)
│
└── RESOURCE CONTROL
    ├── Battery
    ├── Storage
    ├── Bandwidth
    ├── Relay Consent
    └── Abuse Limits
```

A chain of devices may extend communication distance only when actual physical links form a connected path. Do not claim guaranteed 10 km or unlimited range. Real-time audio/video depends on measured latency, bandwidth, packet loss and operating-system support.

# 125. MANDATORY DETAIL TEMPLATE FOR EACH FEATURE

For EVERY feature and functionality above, the coding agent must document and implement:

```text
FEATURE
│
├── PURPOSE
│   └── User problem solved
├── USER EXPERIENCE
│   ├── Entry Point
│   ├── Primary Flow
│   ├── Loading
│   ├── Empty
│   ├── Offline
│   └── Error
├── ACTORS & PERMISSIONS
│   ├── User
│   ├── Creator
│   ├── Moderator
│   └── Administrator
├── DATA
│   ├── Entities
│   ├── Schema
│   ├── Constraints
│   ├── Indexes
│   └── Retention
├── BACKEND
│   ├── API
│   ├── Business Logic
│   ├── State Machine
│   ├── Background Jobs
│   └── Realtime Events
├── SECURITY
│   ├── Authentication
│   ├── Authorization
│   ├── Validation
│   ├── Rate Limits
│   ├── Abuse Prevention
│   └── Audit
├── PRIVACY
│   ├── Visibility
│   ├── Encryption
│   ├── Metadata
│   └── Deletion
├── CLIENTS
│   ├── Android
│   ├── iOS
│   ├── Web
│   └── Desktop
├── QUALITY
│   ├── Unit Tests
│   ├── Integration Tests
│   ├── E2E Tests
│   ├── Security Tests
│   └── Performance Tests
└── OPERATIONS
    ├── Logging
    ├── Metrics
    ├── Alerts
    └── Recovery
```

A feature is NOT complete when only one layer exists.

# 126. FEATURE PLACEMENT RULE FOR ALL OTHER PLATFORM FEATURES

When adding a feature inspired by any other platform:

```text
NEW FEATURE
     │
     ▼
What user problem does it solve?
     │
     ▼
Which ChatApp domain owns it?
     │
     ├── Security
     ├── Communication
     ├── Mesh
     ├── Social
     ├── Communities
     ├── Video
     ├── Creator Economy
     ├── Finance
     └── Administration
     │
     ▼
Already exists?
     ├── YES → Extend/unify existing implementation
     └── NO  → Create full feature specification
     │
     ▼
Privacy + Security Review
     │
     ▼
Data Model + State Machine + API
     │
     ▼
Cross-Platform Plan
     │
     ▼
Implementation + Tests + Monitoring
```

# 127. CROSS-PLATFORM COMPLETENESS TABLE

Maintain this table using actual repository status:

| Feature | Domain | Android | iOS | Web | Desktop | Backend | Database | Security | Tests |
|---|---|---|---|---|---|---|---|---|---|
| Private Chat | Communication | Required | Required | Required | Required | Required | Required | Required | Required |
| Multi-Hop Mesh | Mesh | Native review | Native review | Limited | Platform dependent | Required | Required | Required | Required |
| Voice/Video Calls | Media | Required | Required | Required | Required | Required | As needed | Required | Required |
| Social Feed | Social | Required | Required | Required | Required | Required | Required | Required | Required |
| Short Video | Video | Required | Required | Required | Required | Required | Required | Required | Required |
| Creator Earnings | Economy | Required | Required | Required | Required | Required | Required | Critical | Required |
| Multichain Wallet | Finance | Required | Required | Required | Required | Required | Required | Critical | Required |
| LuckyDraw | LuckyDraw | Required | Required | Required | Required | Required | Required | Critical | Required |

# 128. MASTER GAP ANALYSIS TREE

```text
FEATURE AUDIT
│
├── EXISTS?
│   ├── NO → Missing Feature
│   └── YES
│       ├── REAL IMPLEMENTATION?
│       │   ├── NO → Stub / Mock / Demo / Fake
│       │   └── YES
│       │       ├── END-TO-END?
│       │       │   ├── NO → Missing Layer
│       │       │   └── YES
│       │       │       ├── SECURE?
│       │       │       │   ├── NO → Security Gap
│       │       │       │   └── YES
│       │       │       │       ├── TESTED?
│       │       │       │       │   ├── NO → Quality Gap
│       │       │       │       │   └── YES
│       │       │       │       │       ├── CROSS-PLATFORM?
│       │       │       │       │       │   ├── NO → Parity Gap
│       │       │       │       │       │   └── YES → Verify Production Readiness
```

# 129. FINAL PLATFORM ARCHITECTURE SUMMARY

```text
CHATAPP
│
├── Privacy-Oriented Communication
│   ├── TorChat-inspired concepts
│   ├── SimpleX-inspired concepts
│   ├── Session-inspired concepts
│   └── Briar-inspired offline/local concepts
│
├── Global Communication
│   ├── Telegram-style capabilities
│   ├── WhatsApp-style capabilities
│   └── imo-style calling capabilities
│
├── Social Platform
│   ├── Facebook-style capability categories
│   └── X/Twitter-style public conversation categories
│
├── Video Platform
│   └── TikTok-style video, discovery and creator categories
│
├── Creator Economy
│   └── Earnings, ledger, fraud controls and payouts
│
├── Finance
│   ├── Multichain Wallet
│   ├── Conversion
│   ├── P2P
│   ├── Staking
│   └── Card Integration
│
├── LuckyDraw
│   ├── Daily
│   └── Monthly
│
└── Security & Administration
    ├── Identity
    ├── Application
    ├── Data & Privacy
    ├── Financial
    └── Infrastructure
```

The goal is one coherent platform, not an uncontrolled collection of copied competitor features. Every capability must be independently designed, assigned to an explicit domain, implemented end-to-end, secured, tested and truthfully documented.
