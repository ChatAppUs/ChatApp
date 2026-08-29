# ChatApp — Consolidated Gap Analysis vs Facebook, X, Telegram, TikTok, WhatsApp, imo

Date: 2026-08-29. This is the single consolidated answer to "after deeply
scanning every competitor, what is ChatApp still missing?" Per-competitor,
feature-by-feature detail lives in [competitor-analysis/](competitor-analysis/README.md);
this file rolls the results up and ranks them.

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
- **Money**: built-in multi-chain wallet with admin-managed platform tokens
  (`platform_tokens` — superadmin/finance can add/enable/disable assets),
  KYC-gated P2P in 238 countries, creator RPM earnings + payouts, advertiser
  campaigns with geo/locale targeting and admin review.
- **Governance**: completely separate admin plane — standalone `apps/admin`
  console (port 3100), admin-scoped JWTs via `/api/admin/login` that are
  rejected on every user route, RBAC (superadmin/moderator/support/finance/
  ads_reviewer), audit log, user/reports/KYC/ads/payouts/platform-token/
  fleet management. Users can never reach admin functionality.
- **Clients**: Next.js web (8 locales incl. RTL), Android (Compose), iOS
  (SwiftUI), desktop (Tauri).

## 2. The 12 remaining gaps, ranked by competitive impact

| # | Gap | Competitors that have it | Status & implementation path |
|---|-----|--------------------------|------------------------------|
| 1 | **Video transcoding pipeline** (HLS/ABR ladder, thumbnails, progressive playback) | All six | ❌ New C++ `services/transcode` worker (ffmpeg/SVT-AV1), SKIP LOCKED job queue like the scheduler. This is the single biggest video-quality gap. |
| 2 | **SFU for large calls, audio rooms, live broadcast** | FB Rooms, X Spaces, Telegram voice chats, TikTok Live, imo rooms | ✅ **Shipped** — self-built SFU (`services/sfu`, Pion: own signaling + embedded STUN/TURN, HMAC room tickets) powering meetings, group calls and live broadcasting. Scale-out path: C++ RTP forwarder (see CPP_CONVERSION_PLAN.md). |
| 3 | **Facebook-style content Groups & Pages** (membership, roles, group feeds, business pages, events) | FB, X Communities | ❌ New `groups`/`pages`/`events` entities + feeds; reuses existing posts/comments/reactions. |
| 4 | **Push notifications** (FCM/APNs/Web Push) | All six | ❌ Device-token table + push worker; web Push API for the PWA. In-app + WS notifications already work. |
| 5 | **Watch-time signal ingestion → FYP ranking** | TikTok, FB reels | ❌ `reel_watch_events` table + ingestion endpoint feeding the Python ranker (watch %, rewatches, "not interested"). |
| 6 | **Reels creation tools** (trim, multi-clip, text overlay, captions, duet/stitch) | TikTok, FB, X | ❌ Client editor + transcode pipeline (#1). Auto-captions via a speech-to-text worker in the ML service. |
| 7 | **Monetization depth** (fan subscriptions, tips, gifts, ad-rev share) | X, TikTok, FB, TG | 🚧 Wallet rails + admin-managed platform tokens (`platform_tokens`) shipped; gap: recurring subscriptions, tips, gift catalog. |
| 8 | **Bot API & mini-apps platform** | Telegram, Discord-class platforms | ❌ Bot accounts + long-poll/webhook API reusing the message pipeline; inline bots phase 2. |
| 9 | **Messaging polish** (silent send, spoiler/formatting entities, link previews, custom emoji, invite links + public group handles, slow mode, topics, per-user delete, cross-device draft sync) | Telegram mostly | 🚧/❌ Small server-side increments on existing tables. |
| 10 | **Stories extras** (highlights, close-friends audience, composer tools) | FB, IG, WA | ❌ `highlights` entity + audience flag. |
| 11 | **Privacy suite depth** (custom audience lists, restricted list, profile lock, message-request inbox, mutes/word filters) | FB, X | 🚧 Blocks + audience selector shipped; the rest are settings + query-filter increments. |
| 12 | **Locale expansion to 30+** and low-bandwidth call profile (simulcast, audio-only downgrade, data-saver) | imo, WhatsApp | 🚧 8 locales shipped; bitrate adaptation comes with the SFU (#2). |

## 3. Explicit non-goals / notes

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

## 4. Maintenance & hardening references

- Security posture + this quarter's fixes: [SECURITY_AUDIT.md](SECURITY_AUDIT.md)
- Files to convert to Rust (safety-critical): [RUST_CONVERSION_PLAN.md](RUST_CONVERSION_PLAN.md)
- Files/services to build in C++ (ultra-low-latency): [CPP_CONVERSION_PLAN.md](CPP_CONVERSION_PLAN.md)
