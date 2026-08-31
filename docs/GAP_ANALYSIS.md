# ChatApp — Consolidated Gap Analysis vs Facebook, X, Telegram, TikTok, WhatsApp, imo

Date: 2026-08-31 (gap-pack-3 shipped — migration 018; 153/153 integration +
92/92 gaps + 82/82 gap-pack-3 checks green). This is the single
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
| 6 | **Reels creation tools** (trim, multi-clip, text overlay, captions, duet/stitch) | TikTok, FB, X | 🚧 Text overlays, ASR captions (ML service) and speed ramp shipped; duet/stitch and multi-clip editing remain phase 2 (unblocked by #1). |
| 7 | **Monetization depth** (fan subscriptions, tips, gifts, ad-rev share) | X, TikTok, FB, TG | ✅ **Shipped** — wallet rails, platform tokens, creator subscription tiers, tips, revenue dashboard. Gift catalog shipped (handlers_monetization.go). |
| 8 | **Bot API & mini-apps platform** | Telegram, Discord-class platforms | ✅ **Shipped** — bot accounts, long-poll `getUpdates`, webhooks, `sendMessage`. Mini-apps/inline bots remain phase 2. |
| 9 | **Messaging polish** (silent send, spoiler/formatting entities, custom emoji, slow mode, topics, per-user delete, cross-device draft sync) | Telegram mostly | 🚧 Link previews, invite links, per-conversation drafts, public group @handles + join-by-handle, chat archive, chat folders and sticker packs shipped (018); silent send/spoilers/slow mode/topics remain phase 2. |
| 10 | **Stories extras** (highlights, close-friends audience, composer tools) | FB, IG, WA | 🚧 Close-friends audience + story reactions/replies shipped; story highlights shipped (story_highlights + /api/highlights). |
| 11 | **Privacy suite depth** (custom audience lists, restricted list, profile lock, message-request inbox, mutes/word filters, presence/phone granularity, account self-destruct TTL) | FB, X, Telegram, imo | ✅ **Shipped** — blocks, audience selector, message-request inbox, follow requests, mutes, word filters, restricted list (014); presence/phone privacy matrix, data-saver, safety-mode flag and account TTL worker (018). |
| 12 | **Locale expansion to 30+** and low-bandwidth call profile (simulcast, audio-only downgrade) | imo, WhatsApp | 🚧 8 locales + data-saver flag shipped (018); simulcast comes with the SFU scale-out (C++ RTP forwarder). |

## 3. Gap-pack-3 (migration 018) — shipped 2026-08-31

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

## 4. Explicit non-goals / notes

- **Marketplace, TikTok Shop, fundraisers**: commerce surfaces — phase 2
  after the ads platform matures.
- **Licensed music library**: a legal/licensing dependency, not code.
- **GIF search (GIPHY/Tenor)**: would be the *only* third-party content
  dependency; per the platform's self-reliance rule this ships only as an
  own sticker/GIF catalog.
- **External dependencies (by design, only two)**: the crypto-custody SDK
  (Fireblocks/BitGo/Coinbase WaaS) behind `CryptoProvider`, and the
  crypto-card provider API. Everything else — calls, meetings, SFU,
  live broadcasting, phone OTP verification (self-built engine replacing
  Twilio), messaging, voice/video messages, feed, ranking, moderation — is
  own code.

## 5. Maintenance & hardening references

- Security posture + this quarter's fixes: [SECURITY_AUDIT.md](SECURITY_AUDIT.md)
- Files to convert to Rust (safety-critical): [RUST_CONVERSION_PLAN.md](RUST_CONVERSION_PLAN.md)
- Files/services to build in C++ (ultra-low-latency): [CPP_CONVERSION_PLAN.md](CPP_CONVERSION_PLAN.md)
