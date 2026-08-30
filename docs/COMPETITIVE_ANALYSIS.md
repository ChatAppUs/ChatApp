# ChatApp — Competitive Deep-Scan & Gap Analysis

Goal: one platform that functionally covers **Facebook, X (Twitter), Telegram,
TikTok, WhatsApp, and imo**, with no feature the competitors have that we
structurally lack. Status legend: ✅ shipped · 🚧 backend ready, UI partial ·
🔴 not yet built (roadmap).

## 1. Feature matrix

| Feature | FB | X | TG | TT | WA | imo | ChatApp |
|---|---|---|---|---|---|---|---|
| **Accounts & identity** |
| Email/phone/username registration | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Country-code phone signup with flags | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (210 countries) |
| Password reset (token flow) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| TOTP 2FA | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ (RFC 6238) |
| Multi-language UI | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (8 locales, RTL) |
| **Social graph & feed** |
| Posts with text/images/video | ✅ | ✅ | ✅ | ✅ | – | ✅ | ✅ |
| Followers / following (asymmetric) | ✅ | ✅ | – | ✅ | – | – | ✅ |
| Friends (symmetric) | ✅ | – | – | – | – | – | 🚧 (follow requests + close friends) |
| Ranked feed (ML) | ✅ | ✅ | – | ✅ | – | – | ✅ (Python ML service) |
| @Mentions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| #Hashtags + trending | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| Repost / retweet | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| Emoji reactions (beyond like) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Bookmarks / saved posts | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| Polls | ✅ | ✅ | ✅ | – | ✅ | – | ✅ |
| Stories (24h, expiring) | ✅ | – | ✅ | ✅ | ✅ | ✅ | ✅ |
| Reels / short video feed | ✅ | ✅ | – | ✅ | – | ✅ | ✅ |
| Comments with nested threads | ✅ | ✅ | ✅ | ✅ | – | ✅ | ✅ (2-level) |
| Quote post | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| Lists / circles | ✅ | ✅ | ✅ | – | ✅ | – | 🚧 (close-friends list) |
| **Messaging (Telegram/WhatsApp core)** |
| 1:1 DM | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Group chat | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Broadcast channels | – | – | ✅ | – | ✅ | – | ✅ |
| Read receipts | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Typing indicators | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Online presence / last seen | ✅ | – | ✅ | ✅ | ✅ | ✅ | ✅ |
| Message edit / delete (for everyone) | ✅ | – | ✅ | – | ✅ | ✅ | ✅ |
| Message reactions | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Reply-to message | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (data model) |
| E2E-encrypted chats | – | 🚧 | ✅ (secret) | – | ✅ | ✅ | ✅ (ECDH P-256 + AES-GCM) |
| Disappearing messages | ✅ | – | ✅ | – | ✅ | – | ✅ (per-conversation TTL + sweeper) |
| Voice messages | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (record → media edge → inline player) |
| Stickers / GIFs | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | 🔴 |
| Block users | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Calls** |
| 1:1 audio call | ✅ | ✅ | ✅ | – | ✅ | ✅ | ✅ (WebRTC + WS signaling) |
| 1:1 video call | ✅ | ✅ | ✅ | – | ✅ | ✅ | ✅ |
| Group calls / meetings | ✅ | ✅ | ✅ | – | ✅ | ✅ | ✅ (mesh, P2P DTLS-SRTP) |
| Live streaming | ✅ | ✅ | ✅ | ✅ | – | ✅ | ✅ (self-built SFU broadcast) |
| **Media & monetization** |
| Media upload (resumable, range streaming) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (C++ edge) |
| Creator monetization (RPM on views) | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| Creator payouts (KYC-gated) | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| Subscriptions / paid channels | ✅ | ✅ | ✅ | ✅ | – | – | ✅ (creator tiers + tips) |
| **Advertising** |
| Self-serve ad campaigns | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| Geo + language + interest targeting | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| CPC/CPM bidding, budget, pacing | ✅ | ✅ | ✅ | ✅ | – | – | ✅ (ledger-backed) |
| Impression/click tracking | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |
| **Wallet & payments** |
| Multi-chain crypto wallet | – | – | 🚧 (TON) | – | – | – | ✅ (ledger + provider SDK hooks) |
| P2P transfers | – | – | ✅ | – | 🚧 | – | ✅ |
| KYC verification | – | – | ✅ | – | – | – | ✅ |
| **Platform & admin** |
| Web / Android / iOS / Desktop | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (Next.js / Kotlin+Java / SwiftUI / Tauri) |
| Admin panel + RBAC roles | ✅ | ✅ | ✅ | ✅ | – | – | ✅ (5 roles) |
| Content moderation queue | ✅ | ✅ | ✅ | ✅ | – | – | ✅ (+ ML scoring) |
| Admin audit log | ✅ | ✅ | ✅ | ✅ | – | – | ✅ |

## 2. Architecture vs competitors

| Competitor trait | ChatApp equivalent |
|---|---|
| FB: TAO social graph | PostgreSQL edges (follows/blocks) + MongoDB activity |
| X: fan-out timeline | Pre-materialized `posts` + ML re-rank service |
| TG: MTProto E2EE | WebCrypto ECDH P-256 → AES-GCM, keys relayed, never stored server-side in plaintext |
| WA: Signal protocol calls | WebRTC DTLS-SRTP media + WS signaling |
| TT: video edge | C++ low-latency ingest/stream with HTTP range support |

Polyglot split per requirement: **Go** core API (high-load, distributed),
**Rust** security service (token signing, rate limiting), **C++** media edge,
**Python** ML (ranking/moderation).

## 3. Remaining gaps (roadmap, prioritized)

> Updated 2026-08-30: items 1–8 from the original scan have all shipped
> (live broadcasting on the self-built SFU, disappearing messages with a
> sweeper, voice messages end-to-end, quote posts, creator
> subscriptions/tips, push notifications, and the C++ HLS transcode
> pipeline). The canonical, current gap list lives in
> [GAP_ANALYSIS.md](GAP_ANALYSIS.md). What remains:

1. 🔴 **Stickers/GIF packs** — own sticker catalog (self-reliance rule: no
   GIPHY/Tenor dependency).
2. 🔴 **Friends (symmetric) as a first-class surface** — follow requests +
   close friends exist; mutual-friends view is a query increment.
3. 🔴 **Events** (group/page events) — entity + feed reuse.
4. 🚧 **Reels creation depth** — duet/stitch, multi-clip editing (text
   overlays, ASR captions, speed ramp shipped).
5. 🚧 **Messaging polish** — silent send, spoiler entities, slow mode, topics.
6. 🚧 **SFU scale-out** — C++ RTP forwarding plane when one room saturates a
   core (see CPP_CONVERSION_PLAN.md).

No competitor has a *structural* capability we lack: every remaining
difference is execution depth, not missing concepts.
