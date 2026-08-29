# Competitor Deep-Scan & Gap Analysis

Date: 2026-08-29. Scope: Facebook, X (Twitter), Telegram, TikTok, imo.

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
- i18n (EN/ES/FR/DE/PT/AR/HI/ZH, RTL), web + Android + iOS + desktop clients

### Highest-impact gaps (ranked by competitor importance)

1. **Media pipeline** — real transcoding (HLS/ABR), image resizing, progressive
   upload, video compression. Every competitor's core experience depends on it.
   (Planned as a C++ service — see ../CPP_CONVERSION_PLAN.md.)
2. **Groups & Pages** (Facebook-style communities/business pages) — our groups
   are chat-only; no public content groups, pages, or events.
3. **Reels creation tools**: trimming, filters/effects, duet/stitch, sounds
   library, captions; round video notes.
4. **Push notifications** (FCM/APNs/Web Push) — currently in-app + WS only.
5. **Content discovery**: reel-level recommendation signals (watch time,
   rewatches), "For You" surface, suggested users/communities.
6. **Monetization depth**: subscriptions (fan→creator), tips, stars/gifts,
   ad revenue share payouts beyond flat RPM.
7. **Bots & mini-apps platform** (Telegram) — bot API, inline bots, web apps.
8. **Calls**: SFU for >8 participants (C++ plan), call recording, screen share,
   noise suppression, group call scheduling, live streaming (Live Audio Rooms /
   Spaces / Lives).
9. **Advanced messaging polish**: silent send, spoiler/formatting entities,
   link previews, cross-device draft sync, per-user delete, custom emoji,
   invite links/public group handles, slow mode, topics.
10. **Stories**: highlights (permanent collections), text/music composer tools,
    close-friends audience.
11. **Privacy suite**: custom audience lists, restricted list, profile lock,
    active-status control, message requests inbox, mutes/word filters.
12. **Accessibility & safety**: alt text, captions, content warnings, comment
    filters, anti-spam reputation, report categories, transparency dashboard.

Each file lists the full feature inventory per competitor with status and an
implementation note for every gap.
