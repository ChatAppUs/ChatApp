# Facebook — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-30 (finance plane): the crypto finance plane shipped
after parity batches 4/5 — see "Crypto finance plane (beyond Facebook)" below.
The canonical ranked gap list is [../GAP_ANALYSIS.md](../GAP_ANALYSIS.md).

## Accounts & identity

| Feature | Status | Notes |
|---|---|---|
| Email/phone registration | ✅ | + username, 245 dial codes with flags |
| Google/SSO login | ✅ | Google ID-token sign-in (JWKS verified) |
| 2FA (TOTP/SMS) | ✅ | RFC 6238 TOTP; phone codes via our self-built OTP engine (crypto/rand codes, salted hashes, throttled resend) — no third-party verification service |
| Passkey / biometric login | ✅ | WebAuthn platform authenticators |
| Trusted contacts / account recovery | ❌ | Gap: nominate friends to help recover account |
| Profile lock (non-friends see limited info) | ✅ | users.profile_locked enforced in social + stories handlers |
| Legacy contact / memorialization | ❌ | Gap: low priority |
| Multiple profiles per account | ❌ | Gap: FB allows up to 4 extra profiles |

## Feed & posting

| Feature | Status | Notes |
|---|---|---|
| Text/image/video posts | ✅ | |
| Post audience selector (public/friends/only me/custom) | ✅ | public/followers/only-me enforced in feed queries; gap: custom lists |
| Feeling/activity stickers on posts | ✅ | posts.feeling (free text ≤60 chars), composer UI |
| Location check-in on posts | ✅ | posts.location place name, composer UI |
| Tag people in posts (not just @mention text) | ✅ | post_tags table + composer @tag UI |
| Reactions beyond like (love/haha/wow/sad/angry) | ✅ | post_reactions table, 6 types, picker UI; like = alias |
| Comment threads (nested replies) | ✅ | parent_id threading + comment likes |
| Comment ranking (top/newest) | ✅ | ?sort=top (like-weighted) | newest | oldest |
| Share/repost with quote | ✅ | Repost toggle + quote posts with embedded preview + share_count |
| Pinned post on profile | ✅ | users.pinned_post_id + pin/unpin endpoints, profile renders first |
| Post editing with history | ✅ | post_edits table; GET /api/posts/{id}/edits viewer |
| Scheduled posts | ✅ | posts.publish_at + due-post publisher worker + cancel endpoint |
| Albums / multi-photo grid | ✅ | albums + album_items entities, /albums page, profile strip |
| 3D photos / avatars | ❌ | Gap: out of scope, low value |

## Groups & Pages

| Feature | Status | Notes |
|---|---|---|
| Public/private groups with posts | ✅ | Shipped: content groups with membership, roles, join requests, invite links, group feed |
| Pages (business/creator) with followers | ✅ | Shipped: pages entity, followers, page posts (Events remain phase 2) |
| Events (RSVP, reminders) | ✅ | events + RSVPs + reminder notifications (24h sweep) |
| Marketplace | ❌ | Gap: listings entity — large surface, phase 2 |
| Fundraisers | ❌ | Gap: low priority |

## Stories

| Feature | Status | Notes |
|---|---|---|
| 24h photo/video stories | ✅ | |
| Story replies (DM from story) | ✅ | Replies delivered as DMs with story reference |
| Story reactions | ✅ | Emoji reactions on stories |
| Story highlights (permanent collections) | ✅ | story_highlights + /api/highlights routes |
| Story privacy (close friends) | ✅ | Shipped: close-friends list + audience flag |
| Story viewer list | ✅ | Deduped view tracking, author-only viewer list |
| Text stories / stickers / music | ❌ | Gap: composer tools |

## Reels / video

| Feature | Status | Notes |
|---|---|---|
| Short video feed | ✅ | |
| Watch-time tracking | ✅ | Shipped: reel_watch_events ingestion feeds FYP ranking |
| Reel comments/likes/shares | ✅ | reels are posts.type='reel'; comments/likes/share-to-chat all apply |
| Remix/duet | ❌ | Gap: needs editor |
| Sounds/music library | ❌ | Gap: licensed audio is a legal, not tech, problem |
| Creator monetization on reels | 🚧 | RPM-based earnings exist; gap: per-reel analytics |

## Messaging (Messenger parity)

| Feature | Status | Notes |
|---|---|---|
| 1:1 & group chat, realtime | ✅ | |
| Message edit/delete/reactions | ✅ | |
| Typing indicators, read receipts, presence | ✅ | |
| E2EE | ✅ | ECDH key relay; client-side cipher |
| Voice messages | ✅ | Recorder + inline player |
| Video notes | ❌ | Gap: recorder UI |
| Message requests (non-friends) | ✅ | Shipped: request inbox, accept/decline before chat opens |
| Nicknames per chat | ✅ | chat_nicknames table, PUT /api/conversations/{id}/nicknames/{userId} |
| Chat themes/colors | ✅ | conversations.theme + 🎨 gradient picker in chat header |
| Polls in chat | ✅ | Post polls exist; gap: attach to chat message |
| Location sharing (live) | ❌ | Gap: location message type |
| Payments in chat | 🚧 | Full finance plane shipped (deposits/withdrawals/P2P/convert — see below); gap: pay-in-chat UI hook |

## Calls

| Feature | Status | Notes |
|---|---|---|
| 1:1 audio/video | ✅ | WebRTC mesh |
| Group calls | ✅ | Self-built SFU (services/sfu) powers group calls/meetings/live |
| Messenger Rooms (drop-in video rooms) | 🚧 | Call rooms + join tickets shipped; persistent drop-in links partial |
| Screen sharing | ❌ | Gap: getDisplayMedia in client |
| Call recording | ❌ | Gap: needs SFU/compositor |
| AR effects/filters | ❌ | Gap: low priority |

## Monetization

| Feature | Status | Notes |
|---|---|---|
| In-stream ads revenue share | 🚧 | Flat RPM earnings exist; gap: true ad-share accounting |
| Stars (virtual gifts) | ✅ | gift catalog in handlers_monetization.go |
| Fan subscriptions | ✅ | Shipped: creator subscription tiers + tips + revenue dashboard |
| Professional dashboard (analytics) | ❌ | Gap: per-content analytics rollup |

## Safety & moderation

| Feature | Status | Notes |
|---|---|---|
| Report content/users | ✅ | reports table + admin queue |
| Block users | ✅ | Shipped: blocks table + feed/chat filtering |
| Comment filters / hidden words | ✅ | Shipped: per-user word filters |
| Community Notes equivalent | ❌ | Gap: low priority |
| AI moderation queue | 🚧 | Rust security service has heuristic scorer; gap: media moderation |

## Notifications

| Feature | Status | Notes |
|---|---|---|
| In-app notifications | ✅ | |
| Push (FCM/APNs/Web Push) | ✅ | Shipped: self-built Web Push (VAPID) + FCM/APNs gateway hooks |
| Email notifications | ❌ | Gap: digest worker (SMTP exists) |

## Implementation priority for ChatApp

1. Post audience selector + block list (privacy fundamentals)
2. Story replies/reactions/viewer list (engagement loop)
3. Nested comments + post reactions beyond like
4. Groups with content feeds (biggest structural gap)
5. Push notifications (retention)
6. Voice messages (messaging parity)

## Shipped in ChatApp (2026-08 parity batch 2)

The following gaps from this document are now implemented in ChatApp:
- **Repost toggle + unrepost** (DELETE /api/posts/{id}/repost) with share_count maintenance
- **Quote posts** with embedded quoted-post preview in all post payloads
- **Post editing** (PATCH /api/posts/{id}, author only, edited marker)
- **Threads** (posts.thread_parent_id + GET /api/posts/{id}/thread)
- **Pinned messages** (pin/unpin/list + WS fanout + pinned banner in chat UI)
- **Forward with attribution** (POST /api/messages/{id}/forward; encrypted messages cannot be forwarded)
- **Saved Messages** self-chat (POST /api/conversations/saved, one per user, private)
- **Story engagement**: view tracking (deduped), viewer list (author only), emoji reactions, story replies delivered as DMs with story reference
- **Audience selector** in the composer (public / followers / only me), enforced in feed queries

## Crypto finance plane (beyond Facebook — Meta sunset Novi/Diem)

Shipped 2026-08-30 (migration `015_finance.sql`, 44 new end-to-end checks in
`tests/finance_test.py`). Facebook has no shipped equivalent; this puts ChatApp
at exchange-grade money movement inside the social app:

- **Multi-chain deposits**: deterministic per-user deposit addresses derived
  from `WALLET_MASTER_SEED` (HKDF-SHA256) — BTC/LTC (bech32 SegWit + legacy
  base58check), ETH/EVM (keccak checksummed), Tron (base58check), Solana
  (base58). UI shows a scannable QR + copy button on web, Android (ZXing) and
  iOS (CoreImage).
- **Signed withdrawal pipeline**: every withdrawal is HMAC-signed
  (key-fingerprinted signing key from the master seed) over
  id/user/asset/chain/address/amount/fee. KYC-gated, per-asset address
  validation, immediate ledger hold. Risk scoring (account age, new address,
  velocity, USD amount vs `WITHDRAW_AUTO_THRESHOLD`); low-risk requests are
  signed and approved automatically in single-digit milliseconds (measured
  2–7 ms end-to-end), anything flagged waits for superadmin sign-off in the
  admin console. Rejection auto-refunds the hold. No admin role can move user
  funds without this signature trail.
- **P2P marketplace**: escrow-backed offers/trades with local payment rails
  seeded for all 238 countries (881 methods — UPI, Pix, OPay, M-Pesa, GCash,
  SPEI, PromptPay, Zelle, SEPA, …). Buyer opens trade → seller crypto locks in
  escrow → paid → release; disputes resolved by admins with `p2p.resolve`.
- **Convert engine**: instant asset-to-asset conversion using admin-managed
  USD rates (`convert_rates`), full history, ledger-atomic.
- **Per-token rails**: superadmin adds/removes any coin/token and toggles
  deposit/withdraw/P2P/convert per asset+chain, with min-withdraw and fee.
- **Dynamic admin roles**: superadmin creates/deletes role definitions
  (`admin_role_defs`) with arbitrary permission sets (`p2p.resolve`,
  `convert.manage`, `withdrawals.review`, `tokens.manage`, …) and grants them
  to any account; every finance action is audit-logged.
- **Clients**: web (`/wallet` + `/convert` + `/p2p` pages with QR display and
  camera QR-scan for withdrawal addresses), admin console finance tabs,
  Android `WalletScreen` (ZXing QR, ML Kit scanner), iOS `WalletView`
  (CoreImage QR, AVFoundation scanner). Desktop/extension inherit the web UI.

## Implemented parity — batch 4 (comment threads, share-to-chat, notifications)
- **Comment likes**: `POST/DELETE /api/comments/{id}/like`; comment list returns
  `like_count` + `liked_by_me`; liking notifies the comment author.
- **Nested comment replies** fully wired in the UI: indented reply threads,
  "Reply" button prefills `@username`, `parent_id` returned by the list API.
- **Share post to DM**: `POST /api/posts/{id}/share` sends the post (author +
  excerpt + post reference) into any conversation you belong to and increments
  `share_count`; PostCard "📤" button shows a conversation picker.
- **Notifications center**: `/notifications` page lists all notifications with
  unread highlighting; `POST /api/notifications/read` marks everything read.
