# Competitor Deep-Scan & Gap Analysis

Date: 2026-08-29; status refreshed 2026-08-30. Scope: Facebook, X (Twitter), Telegram, TikTok, imo.

This directory contains a feature-by-feature deep scan of each competitor and
an honest status of ChatApp against every feature: ✅ implemented, 🚧 partially
implemented, ❌ missing. Sources: public product documentation, help centers,
newsroom posts, and feature reports (2024–2026).

## Files

| File | Competitor |
|---|---|
| [facebook.md](facebook.md) | Facebook / Meta social |
| [x-twitter.md](x-twitter.md) | X (Twitter) |
| [telegram.md](telegram.md) | Telegram |
| [tiktok.md](tiktok.md) | TikTok |
| [imo.md](imo.md) | imo |
| [infrastructure.md](infrastructure.md) | Self-built infra: OTP engine, SFU/STUN/TURN, admin plane, platform tokens |

## Cross-platform summary

### ChatApp already matches or exceeds

- Multi-identity accounts (email/username/phone, 238 country codes + flags)
- Google OAuth, passkeys (fingerprint/face/PIN), TOTP 2FA, QR login
- Posts, comments, @mentions, likes, follows, hashtags, trending, bookmarks
- Stories (24h), reels with view counts, polls
- Realtime 1:1/group chat, edit/delete, reactions, typing, read receipts, presence
- E2EE key relay (ECDH P-256) for secret chats
- Broadcast channels
- WebRTC audio/video calls + group meetings
- Multi-chain wallet, KYC-gated P2P transfers, creator earnings
- Ads with geo/locale targeting, budgets, admin review
- Admin RBAC panel
- i18n (EN/ES/FR/DE/PT/AR/HI/ZH, RTL), web + Android + iOS + desktop + browser-extension clients, all with light/dark theme

### Gap status (2026-08-30 update)

Shipped since the original scan: media pipeline (C++ HLS/ABR transcoder +
thumbnails), Groups & Pages, push notifications (Web Push + FCM/APNs hooks +
extension badge), watch-time discovery signals + FYP, monetization depth
(subscription tiers, tips, revenue dashboard), Bot API (getUpdates, webhooks,
sendMessage), SFU group calls/meetings/live broadcast, link previews, invite
links, close-friends audience, message-request inbox, mutes/word filters,
restricted list.

Still on the roadmap (see [../GAP_ANALYSIS.md](../GAP_ANALYSIS.md) for the
canonical ranked list):

1. **Reels creation depth**: duet/stitch, multi-clip editing, round video
   notes (text overlays, ASR captions, speed ramp shipped).
2. **Events** (group/page events).
3. **Advanced messaging polish**: silent send, spoiler entities, slow mode,
   topics, custom emoji.
4. **Stories**: highlights (permanent collections).
5. **Accessibility & safety**: alt text, content warnings, anti-spam
   reputation, transparency dashboard.
6. **Calls**: call recording, noise suppression, C++ RTP forwarding scale-out.
7. **Locale expansion to 30+** (8 shipped) and data-saver call profile.
8. **Bots**: inline bots and mini-apps platform (core Bot API shipped).

Each file lists the full feature inventory per competitor with status and an
implementation note for every gap.
