# ChatApp — Consolidated Gap Analysis vs Facebook, X, Telegram, TikTok, WhatsApp, imo

Date: 2026-09-01 (gap-pack-9 shipped — remaining client gaps closed: TikTok in-app camera (CameraRecorder, getUserMedia+MediaRecorder), reels photo-mode deck (PhotoDeck), imo call-quality (SfuCall.networkQuality from inbound-rtp loss) and adaptive audio-only downgrade, imo low-bitrate handling, plus the "delete-for-me" undo endpoint (POST /api/messages/{id}/unhide). Tests: 15/15 gap-pack-9 + full sweep green. Earlier: gap-pack-8 shipped — migration 024 — TikTok-style
remix_mode duet/stitch on the client, plus the server-side duet/stitch/
trim/mix compositor queue with a C++ ffmpeg worker; HLS-over-RTMP live
ingest; profile Q&A; screen-time limits; app lock + verify-password;
marketplace wallet checkout with affiliate rev-share; FYP feature-vector
rollup; group fanout scale probe; 70/70 gap-pack-8 checks green). Earlier:
gap-pack-7 (migration 023) — chat quiz polls,
bot API expansion (getMe/getChat/editMessageText), chunked resumable upload
sessions, advanced search operators, creator-side word filters, E2E key + SAS
verification, moments, audio-room recordings, related-reels embeddings,
multi-account switcher, reels speed control + prefetch, /u/<username> links;
92/92 gap-pack-7 + 153/153 integration + 44/44 finance + all earlier suites
green). This is the single
consolidated answer to "after deeply scanning every competitor, what is ChatApp
still missing?" Per-competitor, feature-by-feature detail lives in
[competitor-analysis/](competitor-analysis/README.md); this file rolls the
results up and ranks them.

Status legend: ✅ shipped and working end-to-end · 🚧 partially shipped ·
❌ not built yet (roadmap, with implementation path).

## 1. Where ChatApp already matches or beats every competitor

- **Accounts**: email + username + phone registration, 238 country codes with
  flags, password reset, Google OAuth, WebAuthn passkeys, TOTP 2FA, QR login,
  refresh-token rotation. (WhatsApp has no passkeys; Telegram has no email
  signup; imo has no 2FA.)
- **Social**: posts (text/image/video), comments with nested replies + likes,
  @mentions, hashtags + trending, follow graph, repost/quote, post editing,
  threads, polls, bookmarks, story (24h) with views/viewers/reactions/replies,
  reels surface, audience selector, ML-ranked feed.
- **Messaging**: realtime 1:1/group chat over WebSocket, edit/delete,
  reactions, replies-with-quote, forward with attribution, pins, saved
  messages, scheduled messages, disappearing messages (TTL + sweeper),
  per-conversation search, voice messages, typing/presence/read receipts,
  E2EE secret chats (ECDH P-256, client-side cipher), broadcast channels,
  blocks, reports.
- **Calls**: WebRTC 1:1 plus self-built SFU for group calls, meetings and
  live broadcasting with embedded STUN/TURN — no external kit, no external
  signaling service.
- **Money**: a full exchange-grade crypto finance plane — deterministic
  multi-chain deposit addresses (BTC/LTC/EVM/Tron/Solana, HKDF-derived from
  `WALLET_MASTER_SEED`), HMAC-signed withdrawal pipeline with sub-second
  auto-approval under `WITHDRAW_AUTO_THRESHOLD` and superadmin review above it,
  escrowed P2P marketplace with 881 local payment methods across all 238
  countries, instant convert engine with admin-managed rates, per-token
  deposit/withdraw/P2P/convert switches, and dynamic superadmin-defined admin
  roles (all audit-logged). Plus: wallet transfers, admin-managed platform
  tokens, creator RPM earnings + payouts, advertiser campaigns. No competitor
  ships this depth of built-in finance.
- **Governance**: completely separate admin plane — standalone `apps/admin`
  console (port 3100), admin-scoped JWTs via `/api/admin/login` that are
  rejected on every user route, RBAC (superadmin/moderator/support/finance/
  ads_reviewer), audit log, user/reports/KYC/ads/payouts/platform-token/
  fleet management. Users can never reach admin functionality.
- **Clients**: Next.js web (8 locales incl. RTL), Android (Compose), iOS
  (SwiftUI), desktop (Tauri), Chrome/Firefox MV3 browser extension — every
  client with a persisted light/dark theme switch.

## 2. Gap status, ranked by competitive impact

| # | Gap | Competitors that have it | Status & implementation path |
|---|-----|--------------------------|------------------------------|
| 1 | **Video transcoding pipeline** (HLS/ABR ladder, thumbnails, progressive playback) | All six | ✅ **Shipped** — C++ `services/transcode` worker (ffmpeg HLS ladder 240p→1080p + thumbnails), SKIP LOCKED job queue (`transcode_jobs`), internal claim/complete control plane, auto requeue of stale jobs; done jobs rewrite `post_media.url` to the HLS master. |
| 2 | **SFU for large calls, audio rooms, live broadcast** | FB Rooms, X Spaces, Telegram voice chats, TikTok Live, imo rooms | ✅ **Shipped** — self-built SFU (`services/sfu`, Pion: own signaling + embedded STUN/TURN, HMAC room tickets) powering meetings, group calls and live broadcasting. Scale-out path: C++ RTP forwarder (see CPP_CONVERSION_PLAN.md). |
| 3 | **Facebook-style content Groups & Pages** (membership, roles, group feeds, business pages, events) | FB, X Communities | ✅ **Shipped** — groups (invite links, join requests, roles, pinned messages) and pages (create/follow/posts) with full API + UI on every client. Events shipped (RSVP + 24h reminder notifications). |
| 4 | **Push notifications** (FCM/APNs/Web Push) | All six | ✅ **Shipped** — self-built Web Push (VAPID ECDH/AES-GCM) for the PWA + browser extension badge; FCM/APNs gateway hooks behind config. In-app + WS notifications were already live. |
| 5 | **Watch-time signal ingestion → FYP ranking** | TikTok, FB reels | ✅ **Shipped** — `reel_watch_events` ingestion (completion %, rewatches, not-interested) feeding `/api/fyp` ranking; negative signals down-weight. |
| 6 | **Reels creation tools** (trim, multi-clip, text overlay, captions, duet/stitch) | TikTok, FB, X | ✅ Duet/stitch shipped (024: `posts.remix_mode` client layout + server compositor queue duet/stitch/trim/mix with C++ ffmpeg worker); text overlays, ASR captions and speed ramp shipped earlier. |
| 7 | **Monetization depth** (fan subscriptions, tips, gifts, ad-rev share) | X, TikTok, FB, TG | ✅ **Shipped** — wallet rails, platform tokens, creator subscription tiers, tips, revenue dashboard. Gift catalog shipped (handlers_monetization.go). |
| 8 | **Bot API & mini-apps platform** | Telegram, Discord-class platforms | ✅ **Shipped** — bot accounts, long-poll `getUpdates`, webhooks, `sendMessage`, token-authed payment invoices (`createInvoice` + wallet pay) and inline queries (019). Mini-apps remain phase 2. |
| 9 | **Messaging polish** (silent send, spoiler/formatting entities, custom emoji, slow mode, topics, per-user delete, cross-device draft sync) | Telegram mostly | ✅ **Shipped** — link previews, invite links, per-conversation + server-side post drafts, public group @handles + join-by-handle, chat archive, chat folders, sticker packs (018); spoiler/bold/italic/mono/link message entities, GIF catalog + gif/contact messages, channel discussion groups + stats, anonymous admins, screenshot alerts, chat export (019). Slow mode (enforced on send, owner/admin exempt) and forum topics shipped in 014. |
| 10 | **Stories extras** (highlights, close-friends audience, composer tools) | FB, IG, WA | ✅ **Shipped** — close-friends audience, story reactions/replies, story highlights (story_highlights + /api/highlights), and the text-story composer (story_background/story_stickers/story_music, 017). |
| 11 | **Privacy suite depth** (custom audience lists, restricted list, profile lock, message-request inbox, mutes/word filters, presence/phone granularity, account self-destruct TTL) | FB, X, Telegram, imo | ✅ **Shipped** — blocks, audience selector, message-request inbox, follow requests, mutes, word filters, restricted list (014); presence/phone privacy matrix, data-saver, safety-mode flag and account TTL worker (018). |
| 12 | **Locale expansion to 30+** and low-bandwidth call profile (simulcast, audio-only downgrade) | imo, WhatsApp | 🚧 8 locales + data-saver flag shipped (018); simulcast comes with the SFU scale-out (C++ RTP forwarder). |

## 3. Gap-pack-4 (migration 019) — shipped 2026-08-31

- **X parity**: server-side drafts, interest topics + follows + per-user
  interest vector, verified organizations (owner affiliates + superadmin
  verify, `affiliated_org_id` badge), who-to-follow (followers-of-followers
  ranked by mutuals), audio rooms (scheduled/ticketed, host/cohost/speaker/
  listener roles, hand raise, live directory), premium plans + subscriptions
  (`is_premium` flag + expiry worker), self-hosted GIF catalog.
- **Telegram parity**: message entities (spoiler/bold/italic/mono/link —
  sanitized server-side on WS and REST), GIF search + gif/contact card
  messages, channel discussion-group linking, channel stats (members/views/
  shares), anonymous group admins, inline bots, bot payment invoices, people
  nearby, premium subscriptions, chat export (JSON).
- **TikTok parity**: sounds library (self-hosted catalog + use counts), share
  ledger + `share_count` on share-to-chat, live gifts (wallet-debited) +
  room leaderboard, creator marketplace (brand deals), paywalled posts
  (Series), content ratings (everyone/mature), restricted mode, family
  pairing (guardian request/accept), interest vector from authored hashtags.
- **Facebook parity**: marketplace listings (CRUD + category browse),
  fundraisers with wallet donations + raised tracking, professional dashboard
  (`/api/me/analytics` rollup).
- **imo parity**: XP/levels (level = √(xp/100)+1, XP on messages), people
  nearby (opt-in discoverable + live location), group discovery by category,
  audio-room directory, virtual gifts in rooms, screenshot alerts, chat
  export.
- Tests: tests/gaps4_test.py — 96 end-to-end checks against the live API.

## 3a. Gap-pack-3 (migration 018) — shipped 2026-08-31

- **X parity**: reply policy on posts (everyone/following/mentioned/nobody,
  enforced in the comment path), hidden replies (author-visible), creator
  comment pinning, lists + list feed, bookmark folders, content warnings +
  sensitive flag, alt text on media, paid-verification request/review flow.
- **Telegram parity**: self-hosted sticker packs + sticker messages, chat
  folders, chat archive, public group @handles with join-by-handle, granular
  group-admin permissions + member role management, sessions list/revoke,
  account self-destruct TTL worker, phone-number privacy.
- **TikTok parity**: creator playlists, profile-visitors history, comment
  pinning, content warnings.
- **imo parity**: last-seen/online privacy matrix, data-saver mode, stickers,
  profile visitors.
- Tests: tests/gaps3_test.py — 82 end-to-end checks against the live API.

## 3.5 Shipped since (gap pack 5)

- **Drop-in rooms (Messenger Rooms parity)**: persistent shareable links —
  `drop_in_rooms` table (020), `POST/GET /api/rooms`, `GET /api/rooms/{slug}`,
  `POST /api/rooms/{slug}/join` (SFU ticket), `POST /api/rooms/{slug}/end`;
  web `/room/[slug]` page + create/copy from the chat sidebar; create/share
  affordances on Android and iOS.
- **Own KYC pipeline**: `services/ml/kyc_verify.py` (`POST /kyc/verify`) —
  doc-number format, decodability/resolution/sharpness/glare/aspect metrics,
  selfie liveness texture, distinct-capture, NCC face match; the auto-score
  is stored on the submission (020) and drives the kyc_status transition
  (≥0.75 and sanctions-clean auto-verifies; otherwise manual review), with
  the checks surfaced as ML evidence in the admin KYC queue.
- **Ads platform**: `ad_campaigns`/`ad_creatives` (020), create → creatives →
  submit → admin review → activate → wallet-funded budget (double-entry into
  the platform treasury); `POST /api/ads/serve` with target-country matching,
  campaign attribution on placed impressions, and 55% creator revenue share
  ledgered from the treasury (`revshare` reference in ledger_entries).
- **Redis scaling cache**: `services/api/cache.go` — optional Redis backend
  (`REDIS_URL`, fail-open on outage; no cache when unset); the FYP ranking
  query (`/api/fyp`) is cache-through with per-viewer keys
  (`fyp:<uid>:<limit>:<offset>`, 15s TTL).
- **AR effects**: canvas `VideoFilter` publishes the filtered outgoing track
  (remote participants see the effect) on web call, meeting, and room pages.
- **Pay-in-chat UI**: completed on web, Android, and iOS (backend shipped in
  gap pack 2).
- Tests: tests/gaps5_test.py — 39 end-to-end checks against the live API.

## 4. Explicit non-goals / notes

- **TikTok Shop / affiliate commerce**: the remaining commerce surface —
  phase 2 after the ads platform matures. (Marketplace listings and
  fundraisers shipped in 019.)
- **Licensed music library**: a legal/licensing dependency, not code. The
  self-hosted sounds catalog (019) carries original/user-owned audio.
- **GIF search (GIPHY/Tenor)**: shipped in 019 as the self-hosted
  `gif_catalog` — zero third-party content dependencies, per the platform's
  self-reliance rule.
- **External dependencies (by design, only two)**: the crypto-custody SDK
  (Fireblocks/BitGo/Coinbase WaaS) behind `CryptoProvider`, and the
  crypto-card provider API. Everything else — calls, meetings, SFU,
  live broadcasting, phone OTP verification (self-built engine replacing
  Twilio), messaging, voice/video messages, feed, ranking, moderation,
  KYC document/selfie verification (self-built ML `/kyc/verify` pipeline,
  gap pack 5), deposit watching via own chain nodes — is own code.

## 5. Maintenance & hardening references

- Security posture + this quarter's fixes: [SECURITY_AUDIT.md](SECURITY_AUDIT.md)
- Files to convert to Rust (safety-critical): [RUST_CONVERSION_PLAN.md](RUST_CONVERSION_PLAN.md)
- Files/services to build in C++ (ultra-low-latency): [CPP_CONVERSION_PLAN.md](CPP_CONVERSION_PLAN.md)
