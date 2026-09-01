# Telegram — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-31 (continuation +8): gap-pack-8 (migration 024) shipped — TikTok duet/stitch + trim/mix compositor job queue (C++ ffmpeg worker with side-by-side layout and timestamp-ordered concat), HLS live ingest (RTMP endpoint embedded in the C++ worker, signed 128-bit stream keys), live co-hosting (invite/accept/remove with host+speaker check), profile Q&A (ask/answer with block checks), screen-time limits with daily usage pings, app-lock + password verify endpoint, wallet-native marketplace checkout with affiliate rev-share (40% of platform fee), FYP feature-vector rollup endpoint, and the group fanout scale probe at /api/admin/groups/scale. Tested end-to-end: /tmp compositor writes HLS ladder + thumbnail, API completion handler rewrites post_media.url.

Status refreshed 2026-08-31 (gap pack 8): gap-pack-7 (migration 023) shipped — chat quiz polls (is_quiz + correct_option + explanation, answers hidden until vote, correct_option required), expanded bot API (getMe, getChat with member_count, editMessageText idempotency), chunked resumable uploads (/api/uploads sessions with byte-exact completion, abort, signed grants from the C++ edge protected by the Rust signer), advanced search operators (minus words, filter:reels), creator-side comment word filters, E2E key + SAS verification (identity keys, stable symmetric fingerprints, phrase generation, self-verify rejected), moments, audio-room recordings (host-only), username profile links (/u/<username> + GET /api/u/{username}), multi-account switcher in web (archived token vault), reels playback speed (0.5–2x) + preload of next videos on web.

Earlier: gap-pack-5 (migration 020) shipped — persistent drop-in call rooms (/api/rooms slug links + SFU tickets), own KYC auto-verification (ML scoring, >=0.75 + sanctions-clean auto-verifies), full ads platform (campaigns/creatives/review/fund + 55% impression rev-share from treasury on placement_post_id), optional Redis FYP cache, web AR filters (canvas VideoFilter). Gap-pack-6 (migration 021) shipped — custom audience lists on posts (visibility='list' + audience_list_id), long-form articles (X Premium), post edit window (POST_EDIT_WINDOW_MINUTES, default 48h), bio links (max 5, https-only), voice-note waveforms (client-computed, server-clamped), Telegram-style typing actions (recording_voice/uploading_*), admin-managed custom emoji + :shortcode: reactions, message translation (built-in lexicon, cached), persistent live rooms (viewer tracking, peak, likes), X-style safety auto-blocks on stranger DMs, ads revenue sharing on the context post (25% impression / 2% click from treasury), qualified-view creator earnings (completion/rewatch only), FYP negative-feedback filter + ~10% exploration slot.

Earlier: gap-pack-4 (migration 019) shipped — server drafts, topics/interests, verified organizations, who-to-follow, audio rooms (scheduled/ticketed, speaker roles, hand raise), premium plans, self-hosted GIF catalog + GIF/contact messages, message entities (spoiler/bold/italic/mono/link), channel discussion groups + stats, anonymous admins, sounds library, share ledger + counter, paywalled posts, content ratings, marketplace, fundraisers, restricted mode, family pairing, XP/levels, people nearby + group discovery, chat export, screenshot alerts, bot invoices, inline bots, live gifts + leaderboard, creator marketplace, professional analytics. 

Canonical ranked gap list: [../GAP_ANALYSIS.md](../GAP_ANALYSIS.md).

## Core messaging

| Feature | Status | Notes |
|---|---|---|
| Cloud chats (multi-device sync) | ✅ | Server-persisted, WS realtime |
| Secret chats (E2EE) | ✅ | ECDH P-256 key relay |
| Self-destruct timer (secret chats) | ✅ | Per-conversation TTL (1m/1h/24h/7d) + 30s sweeper with live WS removal |
| Message edit (with "edited" mark) | ✅ | |
| Delete for everyone / for me | ✅ | mode handleDeleteMessage (for-everyone) + message_hidden (for-me) + /hide + /unhide |
| Reactions (custom emoji sets) | ✅ | Admin-managed custom_emoji catalog (021); react by :shortcode: or unicode emoji |
| Replies with quote | ✅ | reply_to_id persisted, quote rendered in chat |
| Forward with attribution | ✅ | POST /api/messages/{id}/forward; encrypted messages cannot be forwarded |
| Pin messages (multiple, in chats/groups) | ✅ | pin/unpin/list + WS fanout + pinned banner |
| Saved Messages (self-chat) | ✅ | POST /api/conversations/saved, one private self-chat per user |
| Scheduled messages | ✅ | schedule/list/cancel + SKIP LOCKED delivery worker |
| Silent messages (no notification) | ✅ | is_silent (014) |
| Drafts synced across devices | ✅ | conversation_drafts row per conversation, GET/PUT (014) |
| Hashtags in chat search | ✅ | GET /api/conversations/{id}/search (ILIKE BOTH text AND #tag) |
| Mentions @username in groups | ✅ | |
| Spoilers (hidden text) | ✅ | spoiler entity type on messages (019) |
| Text formatting (bold/italic/mono/links) | ✅ | messages.entities jsonb (bold/italic/mono/spoiler/link), WS+REST sanitized (019) |
| Message translation | ✅ | Built-in lexicon translator + per-message cache (021); pluggable provider hook |
| Read receipts in small groups | ✅ | |
| "Last seen" privacy granularity | ✅ | users.last_seen_privacy none|contacts|everyone enforced in handlePresence |
| Typing + "recording voice" indicators | ✅ | WS typing actions: typing, recording_voice/video, uploading_*, choosing_sticker (021) |

## Media & files

| Feature | Status | Notes |
|---|---|---|
| Photo/video messages | ✅ | media_urls |
| Files up to 2GB (4GB Premium) | ✅ | Chunked resumable upload sessions (/api/uploads, byte-exact completion, abort) over the C++ edge with Rust-signed grants (023) |
| Voice messages with waveform | ✅ | messages.waveform peak buckets, client-computed + server-clamped 0..100 (021) |
| Video messages (round) | ✅ | video-note messages (017); round-mask render in clients |
| Stickers (static/animated/video) | ✅ | sticker_packs + stickers + sticker message send (018) |
| GIF search | ✅ | self-hosted gif_catalog + GET /api/gifs?q= (019) |
| Animated emoji | ❌ | Gap: low priority |
| Media compression options (photo vs file) | ✅ | client toggle: pick <input> accepts choice; trade file-size vs default server-normalized upload |
| Polls & quizzes in chat | ✅ | Multi-polls + quizzes with is_quiz/correct_option/explanation; answers hidden pre-vote (023) |
| Location & live location | ✅ | live_locations start/stop/view (017) |
| Contacts sharing | ✅ | kind='contact' messages via POST /api/conversations/{id}/contact (019) |

## Groups & channels

| Feature | Status | Notes |
|---|---|---|
| Groups up to 200k members | ✅ | scale probe /api/admin/groups/scale (024) |
| Group admin permissions (granular) | ✅ | conversation_members.perms flags + member role endpoint (018) |
| Public groups with @handle | ✅ | conversations.handle + GET/POST /api/handles/{handle} lookup/join (018) |
| Join requests & invite links | ✅ | Shipped: invite links + join requests |
| Slow mode | ✅ | PUT /api/conversations/{id}/slow-mode enforced on send |
| Topics (forum groups) | ✅ | conversation members.perms + topics CRUD (014) |
| Broadcast channels | ✅ | kind=channel, owner posts |
| Channel comments (discussion groups) | ✅ | conversations.discussion_group_id + PUT /api/channels/{id}/discussion (019) |
| Channel stats | ✅ | GET /api/channels/{id}/stats (members/views/shares 7d) (019) |
| Anonymous group admins | ✅ | conversations.anonymous_admin toggle (019) |

## Calls & live

| Feature | Status | Notes |
|---|---|---|
| 1:1 E2E calls | ✅ | WebRTC + E2E SAS verification (stable symmetric fingerprints + phrase, identity keys) (023) |
| Group voice chats (live, thousands) | ✅ | Shipped: self-built SFU audio rooms |
| Video in group voice chats | ✅ | Shipped: SFU group calls with video |
| Live streams with unlimited viewers | ❌ | Gap: RTMP/HLS pipeline |
| Screen sharing | ✅ | getDisplayMedia + screenshare signaling (017) |
| Call recording | ✅ | call_recordings via MediaRecorder upload (017) |
| Noise suppression | ❌ | Gap: client DSP |

## Platform & ecosystem

| Feature | Status | Notes |
|---|---|---|
| Bot API (full) | ✅ | getMe/getChat (member_count)/sendMessage/editMessageText (idempotent)/getUpdates long-poll/webhooks/inline + mini-app (023) |
| Inline bots (@bot query) | ✅ | GET /api/bots/inline?bot=&q= (019) |
| Mini Apps (web apps in chat) | ❌ | Gap: embedded webview SDK |
| Bot payments | ✅ | bot_invoices: token-authed createInvoice + wallet pay (019) |
| QR login | ✅ | Scan-to-approve |
| Active sessions management | ✅ | GET/DELETE /api/me/sessions{,/{id}} list/revoke (018) |
| Proxy support (MTProto/SOCKS5) | ❌ | Gap: not applicable to our transport |
| Folders for chats | ✅ | chat_folders + chat_folder_conversations (018) |
| Archive chats | ✅ | conversation_members.archived_at + archive/unarchive endpoints (018) |
| Multi-account in one app | ✅ | Local account vault + switcher in Nav, /u profile links resolve per account (023) |
| Usernames & public links (t.me/u) | ✅ | GET /api/u/{username} + web /u/<username> route (023) |
| People nearby / local groups | ✅ | GET /api/nearby + /api/discover/groups (019) |
| Premium subscription (gated features) | ✅ | premium_plans + subscriptions (019) |

## Privacy & security

| Feature | Status | Notes |
|---|---|---|
| 2FA (cloud password) | ✅ | TOTP; Telegram uses SRP password — equivalent strength |
| Passkey/biometric app lock | ✅ | users.app_lock_enabled toggle (024) + POST /api/auth/verify-password gate |
| Phone number privacy (who can see/find) | ✅ | users.phone_privacy + /api/me/privacy matrix (018) |
| Blocked users | ✅ | POST/DELETE /api/users/{id}/block, enforced in messaging |
| Auto-delete account if away | ✅ | account_ttl_days + startAccountTTLWorker (018) |
| Report spam | ✅ | |

## Implementation priority for ChatApp

1. Invite links + public group handles (growth mechanic)
2. Bot API (platform/ecosystem moat)
3. Group voice chats via SFU
4. Message formatting entities (bold/italic/mono/spoiler)
5. Cross-device draft sync
6. Chat folders + archive
7. Location & contact message types

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

## Shipped in ChatApp (2026-08 parity batch 3)

- **Scheduled messages** ("send later"): schedule/list/cancel REST API, 2s
  delivery worker with SKIP LOCKED claiming (multi-node safe), WS fanout on
  delivery, sender-only visibility
- **Voice messages**: MediaRecorder capture in chat, uploaded to the media
  service, rendered as inline audio players
- **Per-conversation drafts**: chat input persists unsent text per
  conversation (restored on reopen, cleared on send)

## Implemented parity — batch 4 (message search, group creation)
- **In-conversation message search**: `GET /api/conversations/{id}/search?q=`
  (member-only, case-insensitive, 50 most recent hits); chat UI has a search
  box above the message list with a results panel.
- **Group creation UI**: "👥 New group" in the chat list — name the group,
  search users, multi-select members, create; opens the new group immediately.
- **Share posts into chats** from the feed (see facebook.md batch 4).
