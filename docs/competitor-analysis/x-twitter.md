# X (Twitter) — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-31: gap-pack-3 (migration 018) shipped — privacy
suite depth (presence/phone granularity, data saver, account self-destruct TTL,
safety mode flag), sessions management, chat archive, sticker packs, chat
folders, lists + list feed, bookmark folders, profile visitors, playlists,
paid-verification request/review flow, reply policy enforcement, content
warnings, alt text, hidden replies, creator comment pinning, public group
handles and granular group-admin permissions. Earlier rows shipped in gap-pack-2
(017) are also marked. Canonical ranked gap list:
[../GAP_ANALYSIS.md](../GAP_ANALYSIS.md).

## Core posting

| Feature | Status | Notes |
|---|---|---|
| Short posts (tweets) | ✅ | |
| Long posts (Premium long-form) | 🚧 | content_limit 5000 chars; gap: articles entity |
| Threads (connected posts) | ✅ | thread_parent_id + GET /api/posts/{id}/thread |
| Reply / quote / repost | ✅ | Comment replies, quote posts, repost toggle with counts |
| Post reactions (like) | ✅ | |
| Bookmarks (+ folders) | 🚧 | Bookmarks exist; gap: folders |
| Polls | ✅ | 2–4 options, upsert votes |
| Community Notes | ✅ | notes + helpful-votes + delete (017), web/Android/iOS |
| Post scheduling | ✅ | posts.publish_at + /api/me/scheduled-posts (016) |
| Edit post (time-limited) | 🚧 | Editing shipped (edited marker); gap: time-window enforcement |
| Drafts | ❌ | Gap: server-side drafts |
| Location on posts | ✅ | posts.location (016) |
| Content warnings / sensitive media flag | ✅ | posts.content_warning + sensitive flag (018) |
| Alt text on images | ✅ | post_media.alt_text end-to-end (018) |

## Discovery

| Feature | Status | Notes |
|---|---|---|
| Hashtags + trending | ✅ | |
| For You algorithmic feed | 🚧 | ML-ranked feed exists; gap: engagement signals depth |
| Following feed (chronological) | ✅ | /api/feed |
| Lists (curated user groups) | ✅ | lists + list_members + /api/lists/{id}/feed (018) |
| Topics / interests following | ❌ | Gap: topics entity |
| Advanced search (operators: from:, since:, filter:) | 🚧 | Basic search; gap: operator parser |
| Moments / curated news | ❌ | Gap: editorial product, low priority |

## Communities & social graph

| Feature | Status | Notes |
|---|---|---|
| Followers/following | ✅ | |
| Communities (reddit-like groups) | ✅ | Shipped: content groups with roles + feeds |
| Super Follows / subscriptions | ✅ | Shipped: creator subscription tiers |
| Verified organizations / affiliations | ❌ | Gap: org badges |
| Who-to-follow suggestions | ❌ | Gap: ML candidate generator |

## Spaces (live audio)

| Feature | Status | Notes |
|---|---|---|
| Live audio rooms with speakers/listeners | ❌ | Gap: room entity + SFU audio; our group calls are mesh |
| Space recording & replay | ❌ | Gap: SFU recording |
| Scheduled spaces | ❌ | Gap: scheduling metadata |
| Ticketed spaces | ❌ | Gap: payment gate on room join |

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
| Ad revenue share for creators | 🚧 | RPM earnings; gap: true ad-share |
| Tips | ✅ | Shipped: wallet-backed tips |
| Creator subscriptions | ✅ | Shipped: subscription tiers + revenue dashboard |
| Premium tiers (feature gating) | ❌ | Gap: subscription plans entity |

## Media

| Feature | Status | Notes |
|---|---|---|
| Image/video/GIF upload | ✅ | |
| Multi-image posts (4-grid) | ✅ | media_urls array |
| Video transcoding + adaptive bitrate | ✅ | C++ ffmpeg HLS worker, claim/complete control plane, post_media rewrite to HLS master |
| Live video streaming | 🚧 | live broadcast over the self-built SFU; RTMP ingest pending |
| GIF picker (GIPHY/Tenor) | ❌ | Gap: third-party API integration |

## Moderation & safety

| Feature | Status | Notes |
|---|---|---|
| Block / mute (accounts, words) | 🚧 | Blocks shipped; gap: mutes + word filters |
| Report flows | ✅ | |
| Reply limiting (who can reply) | ✅ | posts.reply_policy everyone|following|mentioned|nobody, enforced in comment path (018) |
| Hidden replies | ✅ | comments.hidden_at + hide/unhide endpoints (018) |
| Safety mode (auto-block) | 🚧 | users.safety_mode flag (018); reputation auto-block engine pending |

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
