# TikTok — feature inventory vs ChatApp

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

## Video consumption

| Feature | Status | Notes |
|---|---|---|
| Vertical short-video feed (FYP) | ✅ | /reels surface |
| Adaptive bitrate streaming (HLS) | ✅ | Shipped: C++ ffmpeg transcode worker, HLS ladder 240p→1080p |
| Preloading / instant playback | ❌ | Gap: client prefetch strategy |
| Watch-time & completion signals | ✅ | Shipped: reel_watch_events → FYP ranking |
| Rewatch detection | ✅ | Shipped: rewatch signals in the same event stream |
| "Not interested" feedback | ✅ | watch signals incl. not-interested/down-weight (014/017) |
| Following feed (video-only) | 🚧 | Gap: filter reels by follows |
| Search (video, users, sounds, hashtags) | 🚧 | Users/hashtags exist; gap: video & sound search |
| Comments with likes & replies | ✅ | Comment likes + nested reply threads; reels are posts, so reel comments included |
| Auto-captions | ✅ | Shipped: ASR hook in the Python ML service |
| Video playback speed control | ❌ | Gap: client player control |

## Creation tools

| Feature | Status | Notes |
|---|---|---|
| In-app camera recording | ❌ | Gap: client recorder (getUserMedia/native) |
| Multi-clip stitching & trimming | ❌ | Gap: editor + ffmpeg pipeline |
| Filters & AR effects | ❌ | Gap: effects SDK; large surface |
| Sounds library (licensed music) | ❌ | Gap: licensing + audio fingerprinting |
| Voiceover & sound mixing | ❌ | Gap: editor |
| Text overlays with timing | ❌ | Gap: editor |
| Duet / Stitch | ❌ | Gap: side-by-side compositor |
| Green screen | ❌ | Gap: client effect |
| Templates | ❌ | Gap: low priority |
| Drafts (local + cloud) | ❌ | Gap: drafts storage |
| Photo mode (carousel posts) | 🚧 | Multi-image posts exist; gap: swipe player in reels surface |

## Social graph & interaction

| Feature | Status | Notes |
|---|---|---|
| Follow / followers | ✅ | |
| Likes on videos | ✅ | |
| Shares with counter | ❌ | Gap: shares entity |
| Favorites/collections | 🚧 | Bookmarks exist; gap: collections |
| Duets chains / remix attribution | ✅ | posts.remix_of + reel_watch analytics (017) |
| Comment pinning by creator | ✅ | posts.pinned_comment_id + PUT /api/posts/{id}/pinned-comment (018) |
| Q&A on profiles | ❌ | Gap: low priority |
| Live gifts during streams | ❌ | Gap: wallet-backed gifts |
| Co-hosting lives | ❌ | Gap: multi-publisher SFU |

## Live

| Feature | Status | Notes |
|---|---|---|
| Live streaming (mobile) | 🚧 | SFU live broadcast shipped; RTMP ingest phase 2 |
| Live chat | 🚧 | WS chat infra reusable; gap: live room entity |
| Live gifts & leaderboard | ❌ | Gap: gifts ledger |
| Live subscriptions | ✅ | Shipped: creator subscription tiers |
| LIVE Events (scheduled) | ❌ | Gap: scheduling metadata |

## Monetization

| Feature | Status | Notes |
|---|---|---|
| Creator fund / rewards program | 🚧 | RPM earnings exist; gap: qualified-views accounting |
| Creator marketplace (brand deals) | ❌ | Gap: brand-creator matching entity |
| Tips | ✅ | Shipped: wallet-backed tips |
| Subscriptions | ✅ | Shipped: creator tiers with recurring support |
| TikTok Shop / affiliate | ❌ | Gap: commerce surface, phase 2 |
| Series (paywalled content) | ❌ | Gap: paid content gating |

## Recommendation system

| Feature | Status | Notes |
|---|---|---|
| Collaborative filtering baseline | ✅ | Python ML service |
| Watch-time-weighted ranking | ✅ | Shipped: watch events ingestion + ranking |
| Cold-start exploration | 🚧 | Recent+follow fallback; gap: exploration budget |
| Content embeddings | ❌ | Gap: video/audio/text embedding workers |
| User interest vector | ❌ | Gap: implicit interest profile |
| Diversity/dedup in feed | ❌ | Gap: reranker rules |
| Report/feedback loop into ranking | ❌ | Gap: negative signals |

## Safety

| Feature | Status | Notes |
|---|---|---|
| Comment filters (keyword, auto) | ❌ | Gap: per-user filters |
| Restricted mode | ❌ | Gap: account-level content filter |
| Screen-time limits / reminders | ❌ | Gap: client feature |
| Family pairing | ❌ | Gap: guardian linking |
| Content levels (mature themes) | ❌ | Gap: content rating enum |
| Video moderation before publish | 🚧 | Rust scorer hook; gap: media pipeline integration |

## Profile

| Feature | Status | Notes |
|---|---|---|
| Profile views history | ✅ | profile_views + /api/me/profile-visitors (018) |
| Pinned videos | ✅ | users.pinned_post_id, profile shows pinned post first (016) |
| Playlists (creator-curated) | ✅ | playlists + playlist_items (018) |
| Bio links | 🚧 | bio text; gap: link metadata |

## Implementation priority for ChatApp

1. Transcoding pipeline (HLS/ABR) — prerequisite for everything video
2. Watch-time events → ML ranking (the FYP flywheel)
3. Reel comments & shares
4. Basic creation tools: trim + text overlay + captions
5. Live streaming (RTMP ingest)
6. Gifts/tips via wallet
