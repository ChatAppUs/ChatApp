# X (Twitter) — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-31 (continuation +8): gap-pack-8 (migration 024) shipped — TikTok duet/stitch + trim/mix compositor job queue (C++ ffmpeg worker with side-by-side layout and timestamp-ordered concat), HLS live ingest (RTMP endpoint embedded in the C++ worker, signed 128-bit stream keys), live co-hosting (invite/accept/remove with host+speaker check), profile Q&A (ask/answer with block checks), screen-time limits with daily usage pings, app-lock + password verify endpoint, wallet-native marketplace checkout with affiliate rev-share (40% of platform fee), FYP feature-vector rollup endpoint, and the group fanout scale probe at /api/admin/groups/scale. Tested end-to-end: /tmp compositor writes HLS ladder + thumbnail, API completion handler rewrites post_media.url.

Status refreshed 2026-08-31 (gap pack 8): gap-pack-7 (migration 023) shipped — moments (publish/draft/re-publish/unpublish with admin oversight), advanced search operators (minus words, filter:reels) in /api/search/posts, audio/Space recordings (host-only record/delete via the media edge), username profile links (/u/<username>), expanded bot surface, E2E key + SAS verification.

Earlier: gap-pack-5 (migration 020) shipped — persistent drop-in call rooms (/api/rooms slug links + SFU tickets), own KYC auto-verification (ML scoring, >=0.75 + sanctions-clean auto-verifies), full ads platform (campaigns/creatives/review/fund + 55% impression rev-share from treasury on placement_post_id), optional Redis FYP cache, web AR filters (canvas VideoFilter). Gap-pack-6 (migration 021) shipped — custom audience lists on posts (visibility='list' + audience_list_id), long-form articles (X Premium), post edit window (POST_EDIT_WINDOW_MINUTES, default 48h), bio links (max 5, https-only), voice-note waveforms (client-computed, server-clamped), Telegram-style typing actions (recording_voice/uploading_*), admin-managed custom emoji + :shortcode: reactions, message translation (built-in lexicon, cached), persistent live rooms (viewer tracking, peak, likes), X-style safety auto-blocks on stranger DMs, ads revenue sharing on the context post (25% impression / 2% click from treasury), qualified-view creator earnings (completion/rewatch only), FYP negative-feedback filter + ~10% exploration slot.

Earlier: gap-pack-4 (migration 019) shipped — server drafts, topics/interests, verified organizations, who-to-follow, audio rooms (scheduled/ticketed, speaker roles, hand raise), premium plans, self-hosted GIF catalog + GIF/contact messages, message entities (spoiler/bold/italic/mono/link), channel discussion groups + stats, anonymous admins, sounds library, share ledger + counter, paywalled posts, content ratings, marketplace, fundraisers, restricted mode, family pairing, XP/levels, people nearby + group discovery, chat export, screenshot alerts, bot invoices, inline bots, live gifts + leaderboard, creator marketplace, professional analytics. 

Canonical ranked gap list: [../GAP_ANALYSIS.md](../GAP_ANALYSIS.md).

## Core posting

| Feature | Status | Notes |
|---|---|---|
| Short posts (tweets) | ✅ | |
| Long posts (Premium long-form) | ✅ | posts.article jsonb {title, subtitle, body<=100k, cover_url} (021) |
| Threads (connected posts) | ✅ | thread_parent_id + GET /api/posts/{id}/thread |
| Reply / quote / repost | ✅ | Comment replies, quote posts, repost toggle with counts |
| Post reactions (like) | ✅ | |
| Bookmarks (+ folders) | ✅ | bookmark_folders + bookmarks.folder_id (018) |
| Polls | ✅ | 2–4 options, upsert votes |
| Community Notes | ✅ | notes + helpful-votes + delete (017), web/Android/iOS |
| Post scheduling | ✅ | posts.publish_at + /api/me/scheduled-posts (016) |
| Edit post (time-limited) | ✅ | POST_EDIT_WINDOW_MINUTES (default 48h) enforced server-side (021) |
| Drafts | ✅ | post_drafts CRUD /api/me/drafts (019) |
| Location on posts | ✅ | posts.location (016) |
| Content warnings / sensitive media flag | ✅ | posts.content_warning + sensitive flag (018) |
| Alt text on images | ✅ | post_media.alt_text end-to-end (018) |

## Discovery

| Feature | Status | Notes |
|---|---|---|
| Hashtags + trending | ✅ | |
| For You algorithmic feed | ✅ | ML-ranked + completion/rewatch signals, report negative filter, ~10% exploration slot (021) + viewer author-affinity boost from completed/rewatched authors (pack-10 feature-store signal) |
| Following feed (chronological) | ✅ | /api/feed |
| Lists (curated user groups) | ✅ | lists + list_members + /api/lists/{id}/feed (018) |
| Topics / interests following | ✅ | interest_topics + follows + per-user interest vector (019) |
| Advanced search (operators: from:, since:, filter:) | ✅ | Operator parser with minus-words + filter:reels/videos (023) |
| Moments / curated news | ✅ | Moments + admin review queue (023) |

## Communities & social graph

| Feature | Status | Notes |
|---|---|---|
| Followers/following | ✅ | |
| Communities (reddit-like groups) | ✅ | Shipped: content groups with roles + feeds |
| Super Follows / subscriptions | ✅ | Shipped: creator subscription tiers |
| Verified organizations / affiliations | ✅ | organizations + members + admin verify, affiliated_org_id badge (019) |
| Who-to-follow suggestions | ✅ | followers-of-followers ranked by mutuals, /api/me/suggestions (019) |

## Spaces (live audio)

| Feature | Status | Notes |
|---|---|---|
| Live audio rooms with speakers/listeners | ✅ | audio_rooms + participants (host/cohost/speaker/listener) + hand raise (019) |
| Space recording & replay | ✅ | Host-only record/delete on audio rooms (023) |
| Scheduled spaces | ✅ | audio_rooms.scheduled_at (019) |
| Ticketed spaces | ✅ | audio_rooms.ticket_price debits wallet on join (019) |

## DMs

| Feature | Status | Notes |
|---|---|---|
| DM realtime chat | ✅ | |
| Encrypted DMs | ✅ | E2EE key relay |
| DM reactions/replies | ✅ | |
| DM search | ✅ | Per-conversation search (member-only, 50 recent hits) |
| Message requests inbox | ✅ | message_requests + accept flow on first unsolicited DM |
| Voice messages | ✅ | Recorder + inline player |

## Monetization & verification

| Feature | Status | Notes |
|---|---|---|
| Verified badge (subscription) | ✅ | verification_requests + admin review grants is_verified (018) |
| Ad revenue share for creators | ✅ | placement_post_id attribution; 55% of impression cost ledgered from platform treasury to the placement author (020) |
| Tips | ✅ | Shipped: wallet-backed tips |
| Creator subscriptions | ✅ | Shipped: subscription tiers + revenue dashboard |
| Premium tiers (feature gating) | ✅ | premium_plans + subscriptions, is_premium flag + expiry worker (019) |

## Media

| Feature | Status | Notes |
|---|---|---|
| Image/video/GIF upload | ✅ | |
| Multi-image posts (4-grid) | ✅ | media_urls array |
| Video transcoding + adaptive bitrate | ✅ | C++ ffmpeg HLS worker, claim/complete control plane, post_media rewrite to HLS master |
 | Live video streaming | ✅ | RTMP ingest via C++ transcode role (024) |
| GIF picker (GIPHY/Tenor) | ✅ | self-hosted gif_catalog + search + gif messages (019) — no third-party dependency |

## Moderation & safety

| Feature | Status | Notes |
|---|---|---|
| Block / mute (accounts, words) | ✅ | Blocks + mutes + creator-side word filters on comments (023) |
| Report flows | ✅ | |
| Reply limiting (who can reply) | ✅ | posts.reply_policy everyone|following|mentioned|nobody, enforced in comment path (018) |
| Hidden replies | ✅ | comments.hidden_at + hide/unhide endpoints (018) |
| Safety mode (auto-block) | ✅ | Stranger DM senders auto-blocked when account <7d or 3+ reports (021) |

## Implementation priority for ChatApp

1. Repost/quote (the core X viral mechanic)
2. Threads (thread_parent_id)
3. Block/mute + reply limiting (safety fundamentals)
4. Lists (power-user retention)
5. Live audio spaces (SFU audio rooms)
6. Advanced search operators

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
