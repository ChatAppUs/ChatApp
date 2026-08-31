# TikTok — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-31: gap-pack-5 (migration 020) shipped — persistent drop-in call rooms (/api/rooms slug links + SFU tickets), own KYC auto-verification (ML scoring, >=0.75 + sanctions-clean auto-verifies), full ads platform (campaigns/creatives/review/fund + 55% impression rev-share from treasury on placement_post_id), optional Redis FYP cache, web AR filters (canvas VideoFilter). Gap-pack-6 (migration 021) shipped — custom audience lists on posts (visibility='list' + audience_list_id), long-form articles (X Premium), post edit window (POST_EDIT_WINDOW_MINUTES, default 48h), bio links (max 5, https-only), voice-note waveforms (client-computed, server-clamped), Telegram-style typing actions (recording_voice/uploading_*), admin-managed custom emoji + :shortcode: reactions, message translation (built-in lexicon, cached), persistent live rooms (viewer tracking, peak, likes), X-style safety auto-blocks on stranger DMs, ads revenue sharing on the context post (25% impression / 2% click from treasury), qualified-view creator earnings (completion/rewatch only), FYP negative-feedback filter + ~10% exploration slot.

Earlier: gap-pack-4 (migration 019) shipped — server drafts, topics/interests, verified organizations, who-to-follow, audio rooms (scheduled/ticketed, speaker roles, hand raise), premium plans, self-hosted GIF catalog + GIF/contact messages, message entities (spoiler/bold/italic/mono/link), channel discussion groups + stats, anonymous admins, sounds library, share ledger + counter, paywalled posts, content ratings, marketplace, fundraisers, restricted mode, family pairing, XP/levels, people nearby + group discovery, chat export, screenshot alerts, bot invoices, inline bots, live gifts + leaderboard, creator marketplace, professional analytics. 

Canonical ranked gap list: [../GAP_ANALYSIS.md](../GAP_ANALYSIS.md).

## Video consumption

| Feature | Status | Notes |
|---|---|---|
| Vertical short-video feed (FYP) | ✅ | /reels surface |
| Adaptive bitrate streaming (HLS) | ✅ | Shipped: C++ ffmpeg transcode worker, HLS ladder 240p→1080p |
| Preloading / instant playback | ❌ | Gap: client prefetch strategy |
| Watch-time & completion signals | ✅ | reel_watch_events → FYP ranking; completion/rewatch drive creator RPM; reports filter the viewer's FYP; ~10% exploration slot (021) |
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
| Sounds library (licensed music) | ✅ | self-hosted sounds catalog + search + use_count (019); licensing remains a legal process |
| Voiceover & sound mixing | ❌ | Gap: editor |
| Text overlays with timing | ❌ | Gap: editor |
| Duet / Stitch | ❌ | Gap: side-by-side compositor |
| Green screen | ❌ | Gap: client effect |
| Templates | ❌ | Gap: low priority |
| Drafts (local + cloud) | ✅ | server-side post_drafts (019) |
| Photo mode (carousel posts) | 🚧 | Multi-image posts exist; gap: swipe player in reels surface |

## Social graph & interaction

| Feature | Status | Notes |
|---|---|---|
| Follow / followers | ✅ | |
| Likes on videos | ✅ | |
| Shares with counter | ✅ | share_ledger + posts.share_count on share-to-chat (019) |
| Favorites/collections | 🚧 | Bookmarks exist; gap: collections |
| Duets chains / remix attribution | ✅ | posts.remix_of + reel_watch analytics (017) |
| Comment pinning by creator | ✅ | posts.pinned_comment_id + PUT /api/posts/{id}/pinned-comment (018) |
| Q&A on profiles | ❌ | Gap: low priority |
| Live gifts during streams | ✅ | live_gifts wallet debits → creator (019) |
| Co-hosting lives | ❌ | Gap: multi-publisher SFU |

## Live

| Feature | Status | Notes |
|---|---|---|
| Live streaming (mobile) | 🚧 | SFU live broadcast shipped; RTMP ingest phase 2 |
| Live chat | ✅ | live_rooms entity + viewer join/leave/peak/likes (021); chat over the bound WS conversation |
| Live gifts & leaderboard | ✅ | GET /api/live/{room}/leaderboard (019) |
| Live subscriptions | ✅ | Shipped: creator subscription tiers |
| LIVE Events (scheduled) | ✅ | audio_rooms.scheduled_at + start/end lifecycle (019) |

## Monetization

| Feature | Status | Notes |
|---|---|---|
| Creator fund / rewards program | 🚧 | RPM earnings exist; gap: qualified-views accounting |
| Creator marketplace (brand deals) | ✅ | brand_deals create/list/accept (019) |
| Tips | ✅ | Shipped: wallet-backed tips |
| Subscriptions | ✅ | Shipped: creator tiers with recurring support |
| TikTok Shop / affiliate | ❌ | Gap: commerce surface, phase 2 |
| Series (paywalled content) | ✅ | posts.price_usd + content_purchases wallet flow (019) |

## Recommendation system

| Feature | Status | Notes |
|---|---|---|
| Collaborative filtering baseline | ✅ | Python ML service |
| Watch-time-weighted ranking | ✅ | Shipped: watch events ingestion + ranking |
| Cold-start exploration | 🚧 | Recent+follow fallback; gap: exploration budget |
| Content embeddings | ❌ | Gap: video/audio/text embedding workers |
| User interest vector | ✅ | interest_vector upserted from authored hashtags (019) |
| Diversity/dedup in feed | ❌ | Gap: reranker rules |
| Report/feedback loop into ranking | ❌ | Gap: negative signals |

## Safety

| Feature | Status | Notes |
|---|---|---|
| Comment filters (keyword, auto) | ❌ | Gap: per-user filters |
| Restricted mode | ✅ | users.restricted_mode toggle (019) |
| Screen-time limits / reminders | ❌ | Gap: client feature |
| Family pairing | ✅ | family_links request/accept flow (019) |
| Content levels (mature themes) | ✅ | posts.content_rating everyone|mature (019) |
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
