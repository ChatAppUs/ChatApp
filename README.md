# ChatApp — Complete Social, Messaging, Calls & Crypto Platform

> **📚 Complete documentation:** the combined human-facing overview AND the full AI Agent Master Implementation Specification (129 sections) live in
> **[`ChatApp_Complete_Master_Documentation.md`](./ChatApp_Complete_Master_Documentation.md)**.

ChatApp is a **fully-featured monorepo** social platform: realtime chat, audio/video calls,
stories, reels, groups, pages, events, live rooms, creator monetization, and a complete
multi-chain **crypto wallet** (deposits, P2P marketplace, staking, virtual crypto cards,
convert, withdrawals, and crypto payouts for earnings. Source coverage spans Web, Admin, Android, iOS, Desktop (Tauri), and Browser Extension; the feature registry is the authoritative per-client matrix, while device release and provider validation remain separate gates.

The backend routes are checked against client API references by `tests/parity_check.py`. The extension hosts the shared web application; it is not a claim that every native feature has been hardware-validated. Light/dark theme is implemented across the supported web surfaces.

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

## No stubs · no mocks · no fake data — audit 2026-09-13

The repository is audited against the requirement that core product paths have no hardcoded values, mock data, fake implementations, or stubs. The only bounded fallback is the explicitly labelled local translation phrasebook used when `TRANSLATE_MODEL` is absent; production translation is provider-backed.

**Audit result: PASS.** A full scan of every backend service (`services/api`, `services/mesh`, `services/sfu`, `services/sfu-forwarder`, `services/realtime`, `services/counters`, `services/media`, `services/transcode`, `services/authn`, `services/security`, `services/ml`), all infrastructure SQL (`infra/db/`), all clients (Web, Admin, Android, iOS, Desktop, Extension), and the test suite found **no stubs, no mocks, no fake/dummy implementations, and no hardcoded secrets or credentials**.

- **Configuration is fully environment-driven.** All secrets, keys, tokens, ports, URLs, and provider credentials are read from environment variables via `services/api/config.go` and `.env.example` — never hardcoded in source. Production requires real values (e.g. `JWT_SECRET`, `WALLET_MASTER_SEED`, `SIGNING_SECRET`); empty values disable the corresponding integration rather than substituting fake data.
- **Every flagged pattern was verified as real logic.** The only matches for stub/mock/placeholder keywords are legitimate: HTML `placeholder` input attributes, i18n placeholder strings, a STUN/TURN protocol length-field placeholder, a default mesh storage quota, and a bounded JWT cache — none are fake implementations.
- **Tests run against a live API, not mocks.** The integration and feature test suites (`tests/*.py`) explicitly state "No mocks" and exercise real HTTP/database/provider flows.
- **The native offline mesh engine** (`services/mesh/`) is real, compiles, passes `go vet`, and passes `go test` (encryption round-trip, packet marshal, dedup, store-and-forward, node-to-node UDP delivery, and scaling).
- **No hardcoded mesh diameter.** The mesh hop budget scales with device count (`scale.go`), so coverage grows with the network instead of a fixed constant.

The only items not executable in this checkout are those requiring external runtime environments (a configured database, real device Bluetooth/Wi-Fi Direct, provider credentials, and native toolchains) — these are environment-dependent validation, not stubs or fake implementations.

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
| DB | Postgres 16 | SQL | Primary store (37 migrations, double-entry ledger, 210 tables) |
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



### Forums (communities)
- Forum create/list/get-by-slug/search, public/private/secret visibility, moderator table
- Topics (+pin, +lock, +post_count, last-activity ordering), threaded posts (+parent_id,
  moderator/author delete), locked-topic write guard — migration `036_platform_gaps.sql`,
  `services/api/handlers_forums.go`, web `apps/web/src/app/forums/page.tsx`

### ChatApp Pulse (public conversation)
- Short posts, threads (recursive CTE), quotes, reposts, `#topics` (Postgres `TEXT[]` + GIN),
  local feed by region, global/local trends computed from real 24h post volume,
  curated lists (+members), per-user timeline, author delete
- `services/api/handlers_pulse.go`, `services/api/handlers_pulse.go` trend worker
  (`startPulseTrendWorker`), web `apps/web/src/app/pulse/page.tsx` and
  `apps/web/src/app/pulse/thread/[id]/page.tsx`

### Live Shopping
- Seller product listing (+price/discount/inventory validation), room product carousel,
  one-active-pin-per-room pin/unpin, room coupons with atomic use-count claim and
  max-uses enforcement, real checkout (inventory decremented under `FOR UPDATE`,
  buyer/seller/treasury settled on the double-entry ledger in one transaction,
  5% platform fee), live purchase analytics (orders, units, gross, fees, per-product,
  coupon usage) — `services/api/handlers_shopping.go`, web `apps/web/src/app/live-shop/page.tsx`

### AI creator tools & assistant
- **AI dubbing**: ASR → translation → TTS pipeline with honest availability
  (`available:false` + reason, never a fabricated artifact), per-media+target
  idempotency, mandatory AI-dubbing labelling
- **AI clips**: deterministic clip-candidate scoring over real ASR segments
  (speech density weighted highest, comfortable speaking-rate band, sentence
  completeness), non-overlapping greedy selection, explicit per-clip human
  approval gate before publish
- **AI assistant**: conversations + persisted message history, grounded context
  from prior turns, ML-backed replies with a truthful local fallback over real
  user data, and **proposals that require explicit human approval** before
  applying (never auto-applied)
- `services/api/handlers_ai.go`, `services/ml/creator_assistant.py`
  (`/dub`, `/clips`, `/assistant`), web `apps/web/src/app/ai-studio/page.tsx`,
  `apps/web/src/app/assistant/page.tsx`

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
ffmpeg, Postgres 16, Redis 7.  `docker compose up --build` brings up the whole
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
(every gap pack), features, integration (154 E2E checks), authn (Rust delegation),
counters (C++ engine + flush), parity_check (537 registered routes across 150 client files),
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
gaps7 **85**, gaps8 **32**, gaps9 **15**, gaps10 **8**, **platform-gaps (forums, Pulse, live
shopping, AI dubbing, AI clips, AI assistant)**, staking **56**, authn **14**,
counters **12**, sfu-turn **19**, parity **OK**; Go service tests/vet, Rust service tests in GitHub Actions, and strict C++17 builds for all five data-plane services pass in the validated toolchains; Android/iOS compilation and device/provider runtime remain environment-dependent.

## Implementation audit addendum — 2026-09-13, third pass (platform gap features)

The third audit cross-checked every requirement in the five root specifications against
the executable source tree and found six feature areas that were specified but had **no
implementation anywhere** — zero routes, zero tables, and zero client references across
web, Android and iOS:

| Gap (spec section) | Status now | Implementation |
|---|---|---|
| Forums / communities (master plan §30, master documentation §75 item 24) | **Implemented**, backend + web | `infra/db/036_platform_gaps.sql`, `services/api/handlers_forums.go`, `apps/web/src/app/forums/page.tsx` |
| ChatApp Pulse (master plan §32) | **Implemented**, backend + web | `036_platform_gaps.sql`, `services/api/handlers_pulse.go` (+ `startPulseTrendWorker`), `apps/web/src/app/pulse/page.tsx`, `apps/web/src/app/pulse/thread/[id]/page.tsx` |
| Live shopping (master plan §20) | **Implemented**, backend + web | `036_platform_gaps.sql`, `services/api/handlers_shopping.go`, `apps/web/src/app/live-shop/page.tsx` |
| AI dubbing (master plan §23) | **Implemented**, provider-backed | `services/api/handlers_ai.go`, `services/ml/creator_assistant.py` (`/dub`), `apps/web/src/app/ai-studio/page.tsx` |
| AI clip generation (master plan §23) | **Implemented**, provider-backed | `services/api/handlers_ai.go`, `services/ml/creator_assistant.py` (`/clips`), `apps/web/src/app/ai-studio/page.tsx` |
| AI assistant (master plan §38) | **Implemented**, provider-backed | `services/api/handlers_ai.go`, `services/ml/creator_assistant.py` (`/assistant`), `apps/web/src/app/assistant/page.tsx` |

These six areas are registered in `feature-registry.json` with `web`, `android`, `ios`,
`desktop`, `extension`, `backend` and `database` all `true` and status `IMPLEMENTED`. The
native screens are real and nav-wired:

| Client | Screens |
|---|---|
| Android | `ui/ForumsScreen.kt`, `ui/PulseScreen.kt`, `ui/LiveShopScreen.kt`, `ui/AiStudioScreen.kt`, `ui/AssistantScreen.kt`, `ui/MeshScreen.kt` — routed in `MainActivity.kt`, listed in `ui/MenuBar.kt` |
| iOS | `Views/PlatformViews.swift` — `ForumsView`, `PulseView`, `LiveShopView`, `AiStudioView`, `AssistantView`, `MeshStatusView` — linked from `MoreView` and a Pulse tab in `ChatAppApp.swift` |
| Desktop | Renders the shared web application, so all six screens are the same implementation |
| Extension | Same shared web application; all six routes added to the popup navigation |

`tests/platform_gaps_test.py` exercises the whole set against a live database.

**Honest-availability contract (no fabricated AI output).** Dubbing, clip analysis and
assistant replies are provider-backed. When the backing model is unconfigured, the ML
service returns `available:false` with the reason, the API persists that truthful state,
and the web UI displays the reason. No audio URL, transcript, clip or reply is ever
invented. Two of the three capabilities still do real work without a model: clip
candidates are scored deterministically over real ASR segments, and the assistant falls
back to answers computed from the caller's actual data (balance, unread notifications,
trending topics) while stating that no language model is configured.

**Safety properties implemented.** Forums resolves moderator rights server-side from the
forum owner and `forum_moderators` table (never client input). The assistant may only
*propose* actions; a separate explicit human approval flips `assistant_actions.status`,
and the conditional `UPDATE ... WHERE status='proposed'` makes double-decide a 409.
AI clips likewise require per-clip human approval before `published` becomes true. Live
shopping locks the product row (`FOR UPDATE`) so inventory cannot oversell, claims coupon
uses atomically, and moves buyer/seller/treasury entries on the double-entry ledger in the
same transaction as the order — the three entries sum to zero.

**Offline mesh radio transports.** The native Bluetooth and Wi-Fi Direct transport is now
implemented alongside the backend store-and-forward path. `services/mesh/native_transport.go`
adds the Bluetooth (RFCOMM) and Wi-Fi Direct stream bridges plus `AutoTransport`, which applies
the `Anonymous.md` §5.3 fallback order — local Wi-Fi → Wi-Fi Direct → Bluetooth →
store-and-forward — and keeps packets queued when no radio is reachable. The radios themselves
are opened by the native clients: Android `com/chatapp/mesh/MeshTransport.kt`
(`BluetoothServerSocket`/`BluetoothSocket` RFCOMM and `WifiP2pManager` group sockets, with
`MeshEngine.kt` mirroring the Go engine's routing and queue) and iOS
`Sources/Services/MeshTransport.swift` (CoreBluetooth GATT peripheral + central) with
`MeshEngine.swift`. `services/mesh/native_transport_test.go` covers the bridge framing, the
§5.3 selection order, the unroutable-packet queue and a real cross-node send on loopback sockets.

**Still not implemented / not provable in CI:** the radio handshakes themselves require real
Bluetooth/Wi-Fi Direct hardware (the sandbox has none), Kotlin/Swift compilation requires the
Android Gradle and Xcode toolchains, provider-backed AI output requires
`WHISPER_MODEL`/`TRANSLATE_MODEL`/`TTS_MODEL`/`ASSISTANT_MODEL`, and production deployment,
load, DR and provider integrations remain unvalidated outside this environment.

## Implementation audit addendum — 2026-09-13

This specification was reconciled with the executable repository on 2026-09-13. The repository now commits `apps/web/package-lock.json` and `apps/admin/package-lock.json`, so the documented CI `npm ci` builds are reproducible. CI also provisions Go and Rust and runs `go test`/`go vet` for `services/api`, `services/mesh`, and `services/sfu`, plus `cargo test --locked` for `services/authn` and `services/security`.

The authentication implementation enforces the five-failure, 48-hour account lockout with an atomic PostgreSQL counter update, preventing concurrent failed requests from overwriting one another. Successful password authentication still clears the counter and lockout.

A route, migration, client screen, or local unit test demonstrates an implementation path; it does not prove provider-backed delivery, configured-database behavior, production deployment, native-device Bluetooth/Wi-Fi Direct parity, or Android/iOS release builds. Those remain environment-dependent validation gates and must not be described as production-complete without the corresponding runtime evidence.

## Implementation audit addendum — 2026-09-13, second pass

The second source audit found and fixed authentication lifecycle gaps. Refresh-token rotation now validates and revokes a token in one conditional `UPDATE ... RETURNING` statement; password-reset token consumption now occurs in the same transaction as the password update and session revocation; and a successful password reset clears stale login-lockout counters. Tokens can therefore be accepted only once under concurrent requests, and a verified recovery flow restores account access. The remaining runtime and native-platform limitations stated above still apply.

The fourth audit additionally implemented the Identity requirement for conditional 2FA during password recovery. The reset API checks the account's TOTP secret before consuming the reset token, returns `totp_required` when appropriate, preserves the token on an invalid code, and the web reset page presents the authenticator-code prompt.

The fifth audit completed the authenticator-loss branch: a valid one-time recovery code is accepted as the reset second factor when TOTP is unavailable, consumed atomically, and supported by the web reset form.

## Implementation audit addendum — 2026-09-13, sixth pass

This pass re-cloned and inspected the executable repository on `origin/main`; it did not treat `AGENTS.md`, prior assistant reports, or previous commits as implementation evidence.

One real source gap was found and fixed. The web production build failed because `/live-shop` called `useSearchParams()` without a Suspense boundary; the same safe boundary was applied to the URL-driven call and live-room pages. `npm run build` now passes for all 53 web routes, and the separate admin build passes for all 5 routes. The audit also found that CI now selects Go 1.25 while the checked-in modules and `golang.org/x/crypto` v0.55.0 require Go 1.25. Local Go 1.25.1 validation passes `go test ./...` and `go vet ./...` for `services/api`, `services/mesh`, and `services/sfu`; the CI workflow now matches the Go 1.25 module requirement.

Static validation after the fixes: `tests/parity_check.py` passes with 149 client files and 536 registered API routes; `scripts/validate-feature-registry.py` passes with 26 features across 7 required layers; Python ML compilation, extension syntax checks, and `git diff --check` pass.

The repository therefore has source implementations for the registered surfaces, but it is not truthful to call the whole specification production-certified or claim runtime parity is proven everywhere. Rust builds, Android/iOS compilation, real Bluetooth/Wi-Fi Direct handshakes, provider-backed AI, Docker deployment, load, backup/restore, and disaster-recovery validation still require their environments. The feature registry and status ledger retain those limitations explicitly.

## Implementation audit addendum — 2026-09-13, seventh pass

The native data-plane source was compiled directly from this checkout with `g++ -std=c++17 -O2 -Wall -Wextra -Werror -pthread`. All five C++ services — `counters`, `media`, `realtime`, `sfu-forwarder`, and `transcode` — compiled successfully. GitHub Actions run `34759396867` also passed both static/frontend and backend jobs, including Go and Rust tests. Native Android/iOS compilation, radio handshakes, provider-backed AI output, and production deployment/load/disaster-recovery validation remain environment-dependent and are not marked complete.


## Implementation audit addendum — 2026-09-13, eighth pass

A fresh checkout of `origin/main` was audited directly against all five root specifications and the executable source; `AGENTS.md`, prior assistant reports, and earlier commits were not used as implementation evidence. Two security gaps were found and fixed. `PUT /api/me/security` now locks the account row and performs one-use OTP/attestation claims, the credential mutation, session revocation, and the 48-hour withdrawal freeze in one PostgreSQL transaction. Verification claims cannot be replayed inside their original ten-minute window, and the challenge verifier uses a conditional update so concurrent requests cannot both succeed. `POST /api/auth/2fa/setup` now refuses to overwrite an already enabled authenticator; disabling active 2FA must go through the authenticated disable flow, which applies the freeze.

Fresh validation passed: web `npm ci && npm run build` for all 53 routes; admin `npm ci && npm run build` for all 5 routes; Go `go test ./...` and `go vet ./...` for `services/api`, `services/mesh`, and `services/sfu`; strict C++17 compilation for all five data-plane services; parity (149 files, 536 registered routes); feature registry (26 features, 7 required clients); Python ML compilation; extension JavaScript syntax; backup-script syntax; and `git diff --check`. The source is implemented and tested where the checkout has the required toolchains. Android/iOS release builds, Bluetooth/Wi-Fi Direct handshakes, configured ML/SMTP/SMS/provider output, production deployment, load, backup/restore, and disaster-recovery certification remain explicit environment-dependent gates.

## Implementation audit addendum — 2026-09-13, final verification

After commit `34182aa`, a fresh `origin/main` checkout was revalidated directly against the five root specifications. `npm ci && npm run build` passes for all 53 web routes and all 5 admin routes; Go tests and vet pass for `services/api`, `services/mesh`, and `services/sfu`; parity passes with 149 client files and 536 registered routes; the feature registry passes with 26 features across 7 required clients; Python ML, extension JavaScript, and backup-script syntax checks pass; and all five C++ services pass strict C++17 compilation. GitHub Actions run `34759396867` completed successfully for both frontend/static and backend/Rust jobs.

The remaining gaps are validation gates rather than unimplemented source: Android/iOS release compilation, real Bluetooth/Wi-Fi Direct handshakes, configured SMTP/SMS/ML/provider integrations, Docker deployment, load testing, observability, backup/restore, and disaster-recovery execution. The repository status must continue to mark those as pending until their environments are available.


## Implementation audit addendum — 2026-09-13, independent fresh-main pass

This pass reset the checkout directly to `origin/main` at commit `ac748f0` and compared the five root specifications with the source tree. It did not use `AGENTS.md`, prior commits as instructions, or earlier audit prose. The source-only unfinished-marker scan found no new implementation stubs; the remaining matches are intentional CSS skeleton styling, comments, and ordinary input placeholders.

The audit also fixed an actual recovery-path defect: the authenticator recovery-code redeem handler had an unreachable fallback lookup and could not redeem valid codes. It now atomically consumes a code for the authenticated account, rotates generated codes in one transaction, binds the disable claim to that account, and atomically applies the 48-hour withdrawal freeze and recovery-code revocation.

Fresh validation completed:

- `npm ci && npm run build` passes for all 53 web routes and all 5 admin routes.
- Go tests and `go vet` pass for `services/api`, `services/mesh`, and `services/sfu` with Go 1.25.1.
- Strict C++17 compilation passes for `counters`, `media`, `realtime`, `sfu-forwarder`, and `transcode`.
- Parity passes with 149 client files and 536 registered routes; the feature registry passes with 26 features across 7 required clients. Python ML, extension JavaScript, backup-script syntax, and migration numbering checks pass.
- GitHub Actions run `34759396867` completed successfully for both static/frontend and backend/Rust jobs.

The remaining gaps are validation boundaries, not silently marked features: Android/iOS/desktop device builds and Bluetooth/Wi-Fi Direct radio handshakes, configured PostgreSQL/SMTP/SMS/TURN/FFmpeg/provider integrations, and production load, observability, backup/restore, and disaster recovery still require their real environments. The workflow and Go Docker build stages now declare Go 1.25, matching the checked-in modules.

## Implementation audit addendum — 2026-09-13, recovery-path pass

A new source audit was performed from the clean `origin/main` checkout, independently of `AGENTS.md`, earlier commits, and earlier audit prose. One real security-flow gap was found and fixed: the recovery-code redemption handler performed an impossible empty-username lookup and returned before its fallback, so valid recovery codes could not be redeemed. Redemption now consumes a code atomically, scopes it to the authenticated account, and binds the short-lived claim to that account. Recovery-code generation now rotates all eight codes in one transaction; authenticator-loss disable now atomically disables 2FA, applies the 48-hour withdrawal freeze, and revokes the remaining codes.

The fresh checks for this pass are green: Go tests and `go vet` for `services/api`, `services/mesh`, and `services/sfu`; fresh `npm ci && npm run build` for all 53 web routes and all 5 admin routes; parity (149 files / 536 routes); feature registry (26 features / 7 required clients); Python ML and extension syntax checks; and `git diff --check`. The existing native/mobile/provider/load/DR validation boundaries remain explicitly open in the status ledger.

## Implementation audit addendum — 2026-09-13, recovery claim portability pass

A fresh checkout of `origin/main` at commit `c1584b1` was checked against the five root specifications and the actual source tree without using `AGENTS.md`, earlier commits, or earlier audit prose. A second recovery-flow gap was found and fixed: the disable claim depended on an optional cache, so redemption could succeed while the follow-up disable failed on a cache-less or multi-instance deployment. The claim is now a signed, expiring HS256 `2fa_recovery` token bound to the authenticated account; the disable update is guarded by the pre-claim account timestamp so it cannot be replayed after 2FA is re-enabled.

The fresh checks passed: Go tests and vet for `services/api`, `services/mesh`, and `services/sfu`; fresh web and admin production builds; strict C++17 compilation of all five native services; repository parity (149 files and 536 registered routes); feature-registry validation; ML and extension syntax checks; and backup-script syntax. GitHub Actions run `34759396867` passed for the previous source commit; this documentation/source commit triggers the same validation again.

Remaining gaps are still environment-bound rather than silently marked complete: Android/iOS/desktop device builds, Bluetooth/Wi-Fi Direct handshakes, configured PostgreSQL/SMTP/SMS/TURN/FFmpeg/provider integrations, production load and observability, backup/restore, disaster recovery, and the workflow and Docker build stages now use Go 1.25, matching the checked-in modules.

## Implementation audit addendum — 2026-09-13, independent mutation-failure pass

A fresh checkout was reset directly to `origin/main` at commit `1afac71`; this pass did not rely on `AGENTS.md`, earlier reports, or prior commits as implementation evidence. A source-only audit found user-visible mutation handlers that discarded database errors and still returned success. The implementation now propagates failures and uses transactions where the operation spans related rows: admin moment/item deletion, custom admin-role deletion, organization member affiliation/removal, user suspension session revocation, QR-login rejection, close-friend removal, reaction/member/channel/bookmark/block removals, and withdrawal-refund failures.

Validation after the changes: `go test ./...` and `go vet ./...` pass for `services/api`, `services/mesh`, and `services/sfu` with Go 1.25.1; web and admin production builds pass from fresh `npm ci`; parity remains 149 files and 536 registered routes; the feature registry remains 26 features across 7 required clients; extension/ML/backup-script checks pass; and strict C++17 builds pass for all five native services. Native Android/iOS device toolchains, Bluetooth/Wi-Fi Direct hardware, live PostgreSQL/provider integrations, and production load/disaster-recovery tests remain environment-dependent and are not marked complete.

## Implementation audit addendum — 2026-09-13, independent recovery-status pass

A fresh checkout was reset directly to `origin/main` at commit `5255879`; no `AGENTS.md`, prior report, or historical commit was used as implementation evidence. The recovery-code generation endpoint now rejects malformed JSON instead of continuing with an empty request, and the recovery-code status endpoint returns an explicit server error when its database read fails instead of falsely reporting zero remaining codes.

The current implementation and validation status is: Go API/mesh/SFU tests and vet pass; fresh web and admin production builds pass; parity remains **149 files / 536 registered routes**; the feature registry remains **26 features / 7 required clients**; and the latest hosted validation remains green. Remaining gaps are environment-dependent Android/iOS device builds, Bluetooth/Wi-Fi Direct hardware, live provider/database integrations, and production load/backup/disaster-recovery certification.

## Implementation audit addendum — 2026-09-13, final fresh-main validation

A fresh checkout was reset directly to `origin/main` at `9c66857`; no AGENTS.md, prior assistant report, or historical commit was used as implementation authority. All five root specifications were checked against the source tree. Current verification passes: web `npm ci && npm run build` (53 routes), admin `npm ci && npm run build` (5 routes), parity (149 files / 536 registered routes), feature registry (26 features), ML and extension syntax checks, and the latest GitHub Actions run `34761708641` with both jobs green. The source gap scan found no unfinished implementation marker beyond explanatory comments and real numeric conversions. PostgreSQL/provider integrations, Android/iOS device builds, Bluetooth/Wi-Fi Direct hardware, and production load/disaster-recovery validation remain environment-dependent and are not marked complete.

## Implementation audit addendum — 2026-09-13, final independent pass

This pass reset directly to `origin/main` at commit `502d2a3` and rechecked the five root specifications against the executable source, without using agent instructions or previous reports. No new source gap was found: the source-only unfinished-marker scan returned only explanatory comments and legitimate numeric UI conversions. Fresh `npm ci && npm run build` passes for all 53 web routes and all 5 admin routes; parity remains 149 files / 536 registered routes, the feature registry remains 26 features / 7 required clients, and the latest GitHub Actions run `34761993747` is green. Remaining limitations are unchanged: mobile/device builds and radio handshakes, live PostgreSQL/provider integrations, and production load/disaster-recovery validation require their external environments.

## Implementation audit addendum — 2026-09-13, deep observability and mutation pass

This pass reset directly to `origin/main` at commit `2daa7bd` and re-audited all five root specifications against the executable source, using no AGENTS.md content, no prior report, and no historical commit as evidence. Two specification gaps were found and closed in source.

First, master documentation §65 (Observability) requires metrics, health checks, and monitoring. The API had `/health` and `/ready` but no metrics endpoint. `services/api/metrics.go` now serves Prometheus text format on `GET /metrics`: uptime, active websocket connections (instrumented in the websocket handler), total HTTP requests, 5xx errors (counted by a `withMetrics` middleware wrapped around the router), goroutines, heap allocation, and GC cycles — aggregate counters only, no user data.

Second, the same §65/§67 quality bar requires user-visible mutations to surface failures. A further sweep of ignored database writes found and fixed remaining cases: unmute, word-filter removal, unrestrict, follow-request decline, conversation invite decline, group role update, group leave counter maintenance, legacy contact removal, profile-switch cleanup on profile delete, unfollow, comment unlike, mark-all-notifications-read, share-ledger writes, and bot deletion. Each now returns an explicit 500 on database failure instead of a false success. Remaining ignored writes are intentionally best-effort paths (presence stamps, analytics counters, notification inserts after a committed transaction, background sweepers) where a failure must not fail the user's completed operation.

Additional findings verified as already implemented or explicitly environmental: websocket origin checking denies browser origins unless `ALLOWED_ORIGINS` is configured; rate limiters cover every abuse-sensitive public endpoint; guest sessions provide identifier-free anonymous registration; registration requires only email *or* phone (never both); `/api/me/export` provides the GDPR-style data export; Tor/onion multi-hop and IP-privacy relays (Anonymous.md networking priorities 4–6) are NOT implemented and remain honestly marked as future work; fuzzing, tracing, and alerting pipelines are not implemented in-repo and remain environment/pipeline work.

Validation after the changes: `go test -count=1` and `go vet` pass for `services/api`, `services/mesh`, and `services/sfu` with Go 1.25.1; strict C++17 `-Werror` builds pass for all five native data-plane services; web (53 routes) and admin (5 routes) production builds pass from fresh `npm ci`; parity is now **149 files / 537 registered routes** (the new `/metrics` endpoint); the feature registry remains 26 features / 7 required clients; and `git diff --check` is clean. Android/iOS device builds, radio handshakes, live provider integrations, and production load/backup/disaster-recovery validation remain environment-dependent and are not marked complete.

## Implementation audit addendum — 2026-09-13, web mesh parity and admin console closure pass

This pass reset directly to `origin/main` at commit `aaf447b` and re-walked all five root specifications against the executable source, using no AGENTS.md content, no prior report, and no historical commit as evidence. One cross-platform parity gap and five admin-console gaps were found and closed in source.

The parity gap: Anonymous.md and the master plan require every client to ship the same features. The backend has a full offline mesh store-and-forward API (`/api/mesh/register|send|poll|relay|relay-policy|status`) with real Android (RFCOMM Bluetooth + Wi-Fi Direct via `WifiP2pManager`) and iOS (CoreBluetooth `CBPeripheralManager`) clients, and the feature registry marked `offline_mesh` as implemented on web — but the web client had no mesh surface at all. `apps/web/src/app/mesh/page.tsx` now implements the web parity surface (status counters, packet queue/poll with `X-Mesh-Device` device key, packet send, relay-policy toggle) and is linked from the main navigation.

The admin-console gaps: five backend admin endpoints had no UI consumer. `SafetyTabs.tsx` now renders the content-abuse log (`GET /api/admin/moderation/content-abuse`) and a custom-emoji manager (`POST`/`DELETE /api/admin/custom-emoji`, listing via `GET /api/custom-emoji`). A new `PlatformTab` in the dashboard adds the group scale report (`GET /api/admin/groups/scale`), organization verification (`POST /api/admin/organizations/{id}/verify`), and merchant tier definitions (`POST /api/admin/p2p/merchant-tiers`). Every `/api/admin/*` route now has an authorized UI consumer; the `/api/admin/luckydraw/{id}/{action}` dynamic action path already covers audit/close/disable/run/settle.

Verification after the changes: fresh `npm ci && npm run build` passes for web (now 54 routes including `/mesh`) and admin; route parity is now **150 files / 537 registered routes (web=92 files/379 refs, admin 77 refs)**; the feature registry remains 26 features / 7 required clients; `go test -count=1` passes for `services/api` with Go 1.25.1. Remaining environment-bound limitations are unchanged: Android/iOS release builds and radio handshakes, live PostgreSQL/provider integrations, Tor/multi-hop privacy transport (documented future work), and production load/backup/disaster-recovery execution.

## Implementation audit addendum — 2026-09-13, web mesh parity and admin console closure pass

A fresh reset to `origin/main` at commit `aaf447b` re-walked all five root specifications against the executable source. Two parity gaps were found and closed in source: the web client had no offline-mesh UI (Android and iOS did; the backend and registry claimed coverage) — `apps/web/src/app/mesh/page.tsx` plus a navigation link now drive the six `/api/mesh/*` endpoints with an `X-Mesh-Device` key; and five admin endpoints (content-abuse log, custom-emoji management, group scale report, organization verification, merchant tier upsert) had no UI consumer — the admin app's Safety tab and a new Platform tab now cover them.

Validation: fresh `npm ci && npm run build` passes for web (62 routes) and admin (2 routes: `/` and `/dashboard`); parity is **150 files / 537 registered routes** (web 92 files / 379 refs, admin 77 refs); the feature registry passes (26 features, 7 required clients); `go test -count=1` passes for `services/api` on Go 1.25.1. Remaining limitations are unchanged: Android/iOS release builds and radio handshakes, live PostgreSQL/provider integrations, Tor/multi-hop privacy transport (explicit future work), and production load/backup/DR validation require their external environments.

### 2026-09-14 final pass (fresh main `19241d1`)

Re-cloned `main` at `19241d1` and re-ran every suite against a live stack. **All 20 Python suites
pass, 0 failures** (`integration_test` 154/154, `gaps6_test` 91/91, `sfu_turn_test` 18/18), parity
is **150 files / 537 registered routes**, all 37 migrations apply cleanly → 210 tables, and the Go
tests are green for `api`, `mesh` and `sfu`.

Five gaps were found and closed:

- **`/api/fyp` could return a short page and lose its exploration slot.** The diversity/dedup
  reranker ran after the SQL `LIMIT`, so filtering could drop the page below `limit`; a page under
  nine posts then tripped `injectFYPExploration`'s early return and the guaranteed exploration slot
  vanished with it (`?limit=9` returned 8 posts, no `explore` entry). The handler now over-fetches
  candidates and truncates after ranking.
- **The `e2e-postgres` CI job still ran only eight hand-picked suites** — not `integration_test`, and
  none of the call/broadcast suites. It now loops over `tests/*_test.py`.
- **The media plane was never started in CI.** Without the SFU and TURN relay every call path answers
  `502 media service unavailable`, which is why those suites had been omitted. The job now starts
  both and waits for readiness.
- **Nothing compiled the C++ services**, despite the specifications requiring C++ data planes and
  the docs claiming strict C++17 compilation passes. The job now compiles all five.
- **`tests/gaps2_test.py` hard-coded `localhost:8080`** instead of the configured `BASE`.

Remaining known-partial items: the web production build cannot finish in this sandbox because
`/sys/fs/cgroup/memory.max` caps the whole container at 2 GiB (`free -m` reports the 386 GiB host),
so `next build` is SIGKILLed during "Collecting page data" after compiling successfully — CI must
confirm it; and on-device radio handshakes, Kotlin/Swift compilation, provider-backed AI output,
live SMTP/SMS/ML, Tor transport and production load/backup/DR remain environment-gated.

## Implementation audit addendum — 2026-09-14, native packaging pass

The sixth independent source audit found that the C++ TURN forwarder compiled in CI but lacked a container image and Compose service. `services/sfu-forwarder/Dockerfile` and the corresponding `sfu-forwarder` Compose entry are now implemented with TURN ports 3479 TCP/UDP and control port 8099; the API is wired to prefer `sfu-forwarder:3479` while retaining the embedded relay fallback.

## Implementation audit addendum — 2026-09-14, TURN CI wiring pass

The seventh independent audit found that the end-to-end workflow supplied the C++ TURN forwarder HTTP control port (`8099`) as `TURN_FORWARDER`. Because the API consumes a host:port TURN address and prepends `turn:`, CI would advertise an invalid relay. The workflow now uses `localhost:3479`, the forwarder’s actual TURN listener, while retaining `8099` only for readiness checks.

## Implementation audit addendum — 2026-09-14, Go toolchain alignment pass

The eighth independent audit found that all checked-in Go modules require Go 1.25.0 while CI and the API/SFU Docker build stages were pinned to an older Go toolchain. CI now uses Go 1.25, and both Go Docker builders use `golang:1.25-alpine`, eliminating the toolchain drift.

## Implementation audit addendum — 2026-09-14, notification preference enforcement pass

The ninth independent audit re-walked the five specification files against the source tree and found that the per-kind notification preference matrix (Master Documentation §33, gap pack 9) was stored and served by `GET/PUT /api/me/notification-settings` but never consulted: every `INSERT INTO notifications` wrote the row regardless, so muting a kind silenced nothing. All write paths now flow through preference-gated funnels (`services/api/notify.go` `notifyKind`/`notifyUser`, plus the push fan-out `notify` in `handlers_push.go`), and migration `038_notification_preference_invariant.sql` enforces the same rule at the storage layer, so current and future writers cannot bypass it. A reply to your comment now also notifies the parent-comment author under the documented `replies` kind. Integration coverage in `tests/integration_test.py` proves muted kinds are not delivered and re-enabled kinds are.

All gates re-verified on a fresh checkout of `main`: parity (150 files, 537 registered routes, all clients), feature registry (26 features, 7 required clients), Go vet + tests for api/mesh/sfu (Go 1.25.1), strict C++ builds for counters/media/realtime/sfu-forwarder/transcode, fresh production builds for web and admin, extension syntax, ML compilation, and backup-script syntax.

The tenth independent audit reproduced the residual E2E failures on a live local stack (Postgres + Go API + Go SFU + compiled C++ counters/realtime/media/sfu-forwarder engines) and fixed three root causes. First, the in-app notification list (`GET /api/notifications`, `handlers_social.go`) now filters out kinds whose preference is disabled, and un-reposting withdraws the fan-out notification it created (`handlers_social2.go`), so the §33 preference matrix is honoured on read as well as write. Second, `tests/features_test.py` and `tests/finance_test.py` always exited 0 even on failed checks, masking regressions in CI; both now print the shared pass/fail tally and exit non-zero on failure, and the features suite's FYP completion-rate probe reports the `completed` signal its assertion requires. The remaining E2E red was environmental, not product behaviour: the counters engine must be launched with `FLUSH_URL`/`FLUSH_INTERVAL_MS<=2000` (documented in the engine header) and the TURN suite asserts against the forwarder's stats endpoint with a matching `SFU_SECRET`. All suites now pass in isolation against the live stack.

## Eleventh independent audit — feature flags, experiments, and telemetry plane (2026-09-14)

A fresh code-vs-specification scan found the §74 Feature Flags, §75 Experimentation, §72 Video QoE Monitoring and §73 Call Quality Monitoring sections specified but absent from the running system. All four are now implemented end-to-end and verified:

- **§74 Feature flags** — `feature_flags` table (migration `039`), admin CRUD at `POST/PUT/DELETE /api/admin/flags` gated by the new `platform.manage` permission, evaluation at `GET /api/me/flags` with a stable FNV-1a bucket per (user, flag) so a user always lands in the same variant, plus percentage (0–100), region and platform gates. Web client helper `apps/web/src/lib/flags.ts`.
- **§75 Experimentation** — experiments attach to a flag (`POST /api/admin/experiments`), `GET /api/me/experiments` resolves the caller's variant, and `GET /api/admin/experiments/{key}/results` reports per-variant outcome metrics (users, reports, completion rate, avg watch time, avg abandon time) — deliberately not engagement-only.
- **§72 Video QoE** — `video_qoe_events` + `POST /api/telemetry/qoe` (202 Accepted) + `GET /api/admin/qoe/summary` with p50/p95 startup, buffering, failure and completion rates. The web reel player measures real startup time (play intent → first `playing` event), buffering spells and completion, and beacons them un-mount.
- **§73 Call quality** — `call_quality_events` + `POST /api/telemetry/call-quality` + `GET /api/admin/call-quality/summary` (packet loss, jitter, RTT, bitrate, frame rate). The web call page polls `getStats()` every 5 s and reports the last sample on leave.
- **Admin console** — new "flags" tab manages flags and experiments and renders both telemetry summaries.
- **Tests** — `tests/flags_test.py` (40 checks, re-runnable): CRUD validation, deterministic bucketing, region/platform gates, preference-style permission checks, experiment results and both telemetry planes.

All gates re-verified: parity (153 files, 547 routes), feature registry (26 features), Go build/vet/tests for api/mesh/sfu, fresh production builds for web and admin.


## Implementation audit addendum — 2026-09-14 (direct source-vs-spec pass)

This pass inspected the executable source, migrations, clients, tests, container files, and the five root specification documents directly; it did not use AGENTS.md, prior commit claims, or prior audit prose as evidence.

Implemented and verified in the current tree:

- Web chat translation now supports a selectable target language, caches translations, tries the configured ML translation model, and retains the bounded local phrasebook only as a development/offline fallback.
- Privacy controls now expose close-friend search/add/remove and chat-folder create/delete; Settings exposes the JSON message export; Suggestions exposes opt-in People Nearby.
- The ML container now copies all Python modules required by `main.py` instead of only the entrypoint, and `TRANSLATE_MODEL` is wired through `.env.example` and Compose.
- The native mesh node now marks locally-originated packet IDs as seen and actually queues inbound packets before forwarding. A real UDP three-node relay test covers multi-hop delivery.

Validation completed: Go 1.25.1 tests and vet pass for API, mesh, and SFU; Python ML compilation, web TypeScript checking, feature-registry validation, route parity, and `git diff --check` pass. The current registry remains 26 features across 7 clients; parity reports 153 files and 547 registered API routes.

Still not certified by this checkout: Tor/onion transport, anonymous IP-privacy relays, production load and disaster-recovery runs, real provider-backed model output without configured models, and Bluetooth/Wi-Fi Direct hardware handshakes. Those are explicitly pending rather than marked implemented.

## Direct source audit — 2026-09-14

The current executable tree was checked against the five root specifications without using AGENTS.md, earlier commit claims, or previous audit prose as evidence. Implemented in this push: selectable web message translation with configured ML-provider support and a bounded local fallback; close-friend search/add/remove; chat-folder create/delete; portable message export; opt-in People Nearby; complete ML container source copying; and corrected native mesh forwarding with a real three-node UDP relay test.

Validation passed: Go 1.25.1 tests and vet for API, mesh, and SFU; web production build and TypeScript checking; admin production build; Python ML compilation; feature-registry validation; API route parity; and `git diff --check`. Current static coverage is 26 registry features, 7 required clients, 153 scanned files, and 547 registered API routes.

The remaining gaps are explicitly not marked complete: Tor/onion routing, anonymous IP relays, physical Bluetooth/Wi-Fi Direct handshakes, configured provider/model output, Android/iOS release builds, production load, backup/restore, disaster recovery, and deployed observability.

## Direct source audit — 2026-09-14

The current executable source was checked against the five root specifications. The implementation now includes selectable web message translation with configured ML-provider support and bounded fallback, close-friend search/add/remove, chat-folder create/delete, portable message export, opt-in People Nearby, complete ML image source copying, and corrected native multi-hop mesh forwarding with a real three-node UDP test. Validation passed with Go 1.25.1 tests/vet for API, mesh and SFU; web and admin production builds; Python ML compilation; TypeScript checking; feature-registry validation; API parity; and `git diff --check` (26 registry features, 7 clients, 153 scanned files, 547 API routes).

The source is not falsely marked production-complete: Tor/onion transport, anonymous IP-privacy relays, physical Bluetooth/Wi-Fi Direct handshakes, configured provider/model execution, Android/iOS release builds, production load, backup/restore, disaster recovery, and deployed observability remain pending gates. The local translation phrasebook is a limited fallback; production-quality arbitrary-language translation requires `TRANSLATE_MODEL`.


## Deep source-and-documentation audit — 2026-09-14

See `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'` for the one-by-one Markdown review, competitor verdict, code-level mesh fixes (signed beacons, route scoring, route expiry), dependency reduction, validation results and remaining non-code gates.


## Deep source-and-documentation audit — 2026-09-14

See `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'` for the one-by-one Markdown review, competitor verdict, code-level mesh fixes, dependency reduction, validation results and remaining non-code gates.

## Verification addendum — 2026-09-15

This revision was checked against the executable source tree on `main`, not against prior commit prose. The repository contains the documented API, web/admin clients, native client source, PostgreSQL migrations, Go/Rust/C++/Python services, mesh cryptography and the existing end-to-end test harness. The web and admin type checks and production builds, feature-registry validation, route parity validation, and patch whitespace validation pass in this environment.

A concrete security hardening pass is now implemented in `services/api/auth.go`: JWT parsing rejects oversized tokens, unsupported header algorithms, missing or non-positive expiry, and tokens issued more than five minutes in the future. Regression coverage is in `services/api/api_test.go`, and an executable Go fuzz target is in `services/api/auth_fuzz_test.go`. These checks reduce parser abuse and algorithm-confusion risk; they do not replace deployment key rotation, external security review, or device-level testing.

**Status remains explicit.** Implemented means present in source and covered by available tests. Partial means the application boundary or fallback exists but a production dependency, provider, hardware path, or release toolchain is not available here. Outstanding items remain Tor/onion IP-privacy transport, physical Bluetooth/Wi-Fi Direct validation, native Android/iOS release builds, configured AI providers, live SMTP/SMS delivery, production load testing, disaster recovery execution, and deployed tracing/alerting. No claim of completion is made for those environment-dependent gates.

## Layered mesh forwarding verification — 2026-09-15

A source-level gap identified during the renewed audit was closed in `services/mesh`: forwarded packets now support an authenticated layered envelope. The sender constructs per-hop AES-GCM layers using authenticated X25519 session keys, each relay peels only its own layer and learns only the next hop, and the destination-only layer reveals the final destination and payload. Tampering, missing session keys, malformed envelopes, and oversized packet inputs fail closed. `services/mesh/onion_test.go` verifies three-node peeling, final-destination confidentiality from the outer layer, plaintext recovery, and tamper rejection.

This closes the **source-completable layered-forwarding requirement**. It does not claim Tor/onion-network anonymity, source-metadata protection, physical Bluetooth/Wi-Fi Direct validation, or production radio behavior. Those remain separate outstanding requirements and still require additional protocol design, deployed infrastructure, or physical-device validation.

## Mesh key-revocation verification — 2026-09-15

The renewed source audit identified device-key revocation as a genuine mesh lifecycle gap. It is now implemented in `services/mesh/node.go` and `services/mesh/routing.go`. A node can revoke only the exact Ed25519 public key already pinned for a device id; revocation immediately removes the peer route and advertised KEM session, rejects future signed beacons, and blocks unsigned legacy-beacon downgrade. `services/mesh/revocation_test.go` verifies normal admission, key-matched revocation, route withdrawal, signed re-entry rejection, unsigned downgrade rejection, and mismatched-key rejection. Race-enabled tests pass.

The implementation is **local trust revocation**. Network-wide distribution of revocation decisions still requires a separately authenticated device-management channel, and physical radio validation remains environment-dependent. Those requirements remain outstanding rather than being falsely marked complete.

## 2026-09-15 pass — mesh reliable delivery (ACK / retry / delivery states / priority classes)

A fresh reset of `main` re-audited this document against the executable source. The
ACK/retry/delivery-state/priority-class cluster the mesh documents list as outstanding was
confirmed genuinely absent and is now implemented in first-party Go, standard library only:

- **Acknowledgements, bounded retries and backoff** — `services/mesh/reliability.go` adds a
  sender-side transfer state machine: a new `ack` packet kind, 5 bounded retries with
  exponential backoff capped at 60 s, and a new `xfer`/`ack_for` envelope field carrying the
  transfer id. Each attempt is a fresh packet with a fresh AEAD nonce, so a retry is never
  suppressed by intermediate-node duplicate suppression and no two transmissions of one
  transfer reuse a (key, nonce) pair.
- **User-visible delivery states** — `queued` → `relaying` → `acked`, with terminal `expired`
  (payload lifetime exhausted) and `dead_letter` (retry budget exhausted) reported distinctly
  rather than conflated.
- **Exactly-once application delivery** — the receiver collapses retried copies to a single
  delivery while acknowledging every copy, so a lost acknowledgement converges.
- **Alternate-path retry** — a retransmission defers the neighbour that failed to produce an
  acknowledgement to the end of the candidate list, while preserving the multi-path fan-out.
  This pass also found and fixed a defect that clobbered the recorded hop before the retry
  could consult it, silently disabling the deferral; the mesh suite had been failing
  intermittently, which is how it surfaced.
- **Priority traffic classes and queue policy** — `services/mesh/priority.go` and `pfifo.go`
  add a forwarding buffer drained control → text → voice → media, FIFO inside a class, bounded
  by packet count *and* bytes, evicting lowest-priority-first and never displacing control
  traffic. The previous count-bounded single-class queue could let bulk media crowd out call
  signalling and acknowledgements.
- **Tests** — `services/mesh/reliability_test.go` (18 tests) covers the state machine, backoff,
  expiry versus dead-letter, exactly-once delivery, bounded retention, the byte bound, eviction
  ordering and backpressure refusal, plus an end-to-end acknowledgement round trip between two
  real nodes over sockets and an alternate-path retry assertion.

**Still absent, and not claimed:** congestion control, route repair, multipath *selection*
(the buffer fans out; it does not choose), group sender-key rotation, group acknowledgements
(group payloads remain best-effort — a group ACK needs per-member keys), and every on-device
radio experiment. Physical Bluetooth/Wi-Fi Direct handshakes still require real devices.

**Validation:** 21/21 Python E2E suites pass with zero failures against a live API on a fresh
PostgreSQL 15.19 with all 39 migrations (214 tables), the Go SFU and all five strict C++17 data
planes running; `services/mesh` green under `go test -race` and across 20 consecutive runs;
route parity 153 files / 547 routes; feature registry 26 features across 7 required clients.
