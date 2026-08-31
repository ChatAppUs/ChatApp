# imo — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-31 (continuation +8): gap-pack-8 (migration 024) shipped — TikTok duet/stitch + trim/mix compositor job queue (C++ ffmpeg worker with side-by-side layout and timestamp-ordered concat), HLS live ingest (RTMP endpoint embedded in the C++ worker, signed 128-bit stream keys), live co-hosting (invite/accept/remove with host+speaker check), profile Q&A (ask/answer with block checks), screen-time limits with daily usage pings, app-lock + password verify endpoint, wallet-native marketplace checkout with affiliate rev-share (40% of platform fee), FYP feature-vector rollup endpoint, and the group fanout scale probe at /api/admin/groups/scale. Tested end-to-end: /tmp compositor writes HLS ladder + thumbnail, API completion handler rewrites post_media.url.

Status refreshed 2026-08-31 (gap pack 8): gap-pack-7 (migration 023) shipped — E2E key + SAS verification (stable symmetric fingerprints + phrase endpoint, self-verify rejected) for call/chat trust, chunked resumable uploads (upload sessions over the C++ edge with Rust-signed grants), expanded bot surface, username profile links (/u/<username>), multi-account switcher on web.

Earlier: gap-pack-5 (migration 020) shipped — persistent drop-in call rooms (/api/rooms slug links + SFU tickets), own KYC auto-verification (ML scoring, >=0.75 + sanctions-clean auto-verifies), full ads platform (campaigns/creatives/review/fund + 55% impression rev-share from treasury on placement_post_id), optional Redis FYP cache, web AR filters (canvas VideoFilter). Gap-pack-6 (migration 021) shipped — custom audience lists on posts (visibility='list' + audience_list_id), long-form articles (X Premium), post edit window (POST_EDIT_WINDOW_MINUTES, default 48h), bio links (max 5, https-only), voice-note waveforms (client-computed, server-clamped), Telegram-style typing actions (recording_voice/uploading_*), admin-managed custom emoji + :shortcode: reactions, message translation (built-in lexicon, cached), persistent live rooms (viewer tracking, peak, likes), X-style safety auto-blocks on stranger DMs, ads revenue sharing on the context post (25% impression / 2% click from treasury), qualified-view creator earnings (completion/rewatch only), FYP negative-feedback filter + ~10% exploration slot.

Earlier: gap-pack-4 (migration 019) shipped — server drafts, topics/interests, verified organizations, who-to-follow, audio rooms (scheduled/ticketed, speaker roles, hand raise), premium plans, self-hosted GIF catalog + GIF/contact messages, message entities (spoiler/bold/italic/mono/link), channel discussion groups + stats, anonymous admins, sounds library, share ledger + counter, paywalled posts, content ratings, marketplace, fundraisers, restricted mode, family pairing, XP/levels, people nearby + group discovery, chat export, screenshot alerts, bot invoices, inline bots, live gifts + leaderboard, creator marketplace, professional analytics. 

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
| Group chats (up to 100k) | ✅ | group-scale probe: /api/admin/groups/scale reports largest groups + fanout strategy (024) |
| Stories | ✅ | |
| Chat backup/restore | ✅ | GET /api/me/export JSON export (019); import is client-side |
| Message translation | ✅ | Built-in lexicon translator + per-message cache (021); pluggable provider hook |
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
