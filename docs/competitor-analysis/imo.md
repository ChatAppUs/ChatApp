# imo — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-31: gap-pack-4 (migration 019) shipped — server drafts, topics/interests, verified organizations, who-to-follow, audio rooms (scheduled/ticketed, speaker roles, hand raise), premium plans, self-hosted GIF catalog + GIF/contact messages, message entities (spoiler/bold/italic/mono/link), channel discussion groups + stats, anonymous admins, sounds library, share ledger + counter, paywalled posts, content ratings, marketplace, fundraisers, restricted mode, family pairing, XP/levels, people nearby + group discovery, chat export, screenshot alerts, bot invoices, inline bots, live gifts + leaderboard, creator marketplace, professional analytics. 

Canonical ranked gap list: [../GAP_ANALYSIS.md](../GAP_ANALYSIS.md).

imo is strongest in low-bandwidth markets (South Asia, Middle East, Africa):
free international calls over 2G/3G, lightweight client, massive group voice
rooms. These are the differentiators to match.

## Calls (imo's core strength)

| Feature | Status | Notes |
|---|---|---|
| Free international audio calls | ✅ | WebRTC; gap: PSTN is not imo's model either |
| Video calls on 2G/3G (adaptive quality) | 🚧 | WebRTC adapts; gap: explicit low-bitrate profile + simulcast |
| Group video calls | ✅ | Mesh ≤8 |
| Large group voice chat rooms (drop-in) | ✅ | Shipped: self-built SFU audio rooms |
| Call quality indicator | ❌ | Gap: client RTCP stats UI |
| Data-usage saver mode | ✅ | users.data_saver flag drives client bitrate caps (018) |
| Call on slow networks auto-audio-only | ❌ | Gap: adaptive downgrade logic |

## Messaging

| Feature | Status | Notes |
|---|---|---|
| Text/photo/video messages | ✅ | |
| Voice messages | ✅ | MediaRecorder capture, inline audio player |
| Stickers (big catalog) | ✅ | self-hosted sticker packs + sticker messages (018) |
| Group chats (up to 100k) | 🚧 | Groups exist; gap: scale validation |
| Stories | ✅ | |
| Chat backup/restore | ✅ | GET /api/me/export JSON export (019); import is client-side |
| Message translation | ❌ | Gap: translation hook |
| Disappearing messages | ✅ | Per-conversation TTL + sweeper with live WS removal |

## Discovery & social (imo's engagement layer)

| Feature | Status | Notes |
|---|---|---|
| People nearby | ✅ | opt-in discoverable + live location → GET /api/nearby (019) |
| Voice club / audio rooms discovery | ✅ | audio_rooms directory GET /api/audio-rooms (019) |
| Levels & gamification (active-user levels) | ✅ | user_levels XP ledger, level=√(xp/100)+1, /api/me/level (019) |
| Virtual gifts in rooms | ✅ | live_gifts wallet ledger + room leaderboard (019) |
| Big groups directory by interest | ✅ | conversations.category + GET /api/discover/groups (019) |
| Profile visitors ("who viewed me") | ✅ | profile_views + /api/me/profile-visitors (018) |

## International reach

| Feature | Status | Notes |
|---|---|---|
| Phone-number-first registration | ✅ | 245 countries, flags, verification codes |
| Low-end Android support | 🚧 | Native app exists; gap: APK size/memory budget |
| Offline message delivery | ✅ | Server-persisted, sync on connect |
| Multi-language UI | 🚧 | 8 locales (EN/ES/FR/DE/PT/AR/HI/ZH, RTL); gap: imo ships 30+ |

## Privacy

| Feature | Status | Notes |
|---|---|---|
| Block contacts | ✅ | Block/unblock enforced in messaging |
| Last-seen / online privacy | ✅ | last_seen_privacy everyone|contacts|nobody enforced on presence (018) |
| Screenshot alert (secret chats) | ✅ | POST /api/conversations/{id}/screenshot notifies the other party (019) |
| Encrypted chats | ✅ | E2EE key relay |

## Implementation priority for ChatApp

1. Low-bandwidth call profile (simulcast + adaptive audio-only downgrade)
2. Group voice rooms (SFU) — imo's stickiest feature
3. Presence privacy granularity
4. Locale expansion to 30+ languages
5. People-nearby with strict privacy defaults
6. Profile visitors + gamification
