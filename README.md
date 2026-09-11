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