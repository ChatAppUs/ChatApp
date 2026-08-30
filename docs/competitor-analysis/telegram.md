# Telegram — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

Status refreshed 2026-08-30: features shipped in parity batches 4/5 and the
realtime/transcode work are marked accordingly; the canonical ranked gap list is
[../GAP_ANALYSIS.md](../GAP_ANALYSIS.md).

## Core messaging

| Feature | Status | Notes |
|---|---|---|
| Cloud chats (multi-device sync) | ✅ | Server-persisted, WS realtime |
| Secret chats (E2EE) | ✅ | ECDH P-256 key relay |
| Self-destruct timer (secret chats) | ✅ | Per-conversation TTL (1m/1h/24h/7d) + 30s sweeper with live WS removal |
| Message edit (with "edited" mark) | ✅ | |
| Delete for everyone / for me | 🚧 | delete-for-everyone exists; gap: per-user deletion |
| Reactions (custom emoji sets) | 🚧 | Basic reactions; gap: custom emoji |
| Replies with quote | ✅ | reply_to_id persisted, quote rendered in chat |
| Forward with attribution | ✅ | POST /api/messages/{id}/forward; encrypted messages cannot be forwarded |
| Pin messages (multiple, in chats/groups) | ✅ | pin/unpin/list + WS fanout + pinned banner |
| Saved Messages (self-chat) | ✅ | POST /api/conversations/saved, one private self-chat per user |
| Scheduled messages | ✅ | schedule/list/cancel + SKIP LOCKED delivery worker |
| Silent messages (no notification) | ❌ | Gap: silent flag |
| Drafts synced across devices | 🚧 | Per-conversation drafts persist on the client; gap: cross-device server sync |
| Hashtags in chat search | 🚧 | Global hashtags exist; gap: per-chat filter |
| Mentions @username in groups | ✅ | |
| Spoilers (hidden text) | ❌ | Gap: formatting marker |
| Text formatting (bold/italic/mono/links) | ❌ | Gap: entities array on messages |
| Message translation | ❌ | Gap: translation API hook |
| Read receipts in small groups | ✅ | |
| "Last seen" privacy granularity | 🚧 | Presence exists; gap: privacy rules |
| Typing + "recording voice" indicators | 🚧 | Typing exists; gap: action types |

## Media & files

| Feature | Status | Notes |
|---|---|---|
| Photo/video messages | ✅ | media_urls |
| Files up to 2GB (4GB Premium) | 🚧 | 2 GiB cap at media edge; gap: chunked resumable upload |
| Voice messages with waveform | 🚧 | Voice messages shipped (recorder + inline player); gap: waveform data |
| Video messages (round) | ❌ | Gap: recorder UI |
| Stickers (static/animated/video) | ❌ | Gap: sticker packs entity |
| GIF search | ❌ | Gap: Tenor/GIPHY integration |
| Animated emoji | ❌ | Gap: low priority |
| Media compression options (photo vs file) | ❌ | Gap: upload pipeline choice |
| Polls & quizzes in chat | 🚧 | Post polls exist; gap: chat-embedded |
| Location & live location | ❌ | Gap: location message type |
| Contacts sharing | ❌ | Gap: contact card message type |

## Groups & channels

| Feature | Status | Notes |
|---|---|---|
| Groups up to 200k members | 🚧 | Group chat exists; gap: scale testing, admin tools |
| Group admin permissions (granular) | 🚧 | role owner/admin/member; gap: per-permission flags |
| Public groups with @handle | ❌ | Gap: public handles + join-by-link |
| Join requests & invite links | ✅ | Shipped: invite links + join requests |
| Slow mode | ❌ | Gap: per-group rate limit |
| Topics (forum groups) | ❌ | Gap: topic threads in groups |
| Broadcast channels | ✅ | kind=channel, owner posts |
| Channel comments (discussion groups) | ❌ | Gap: link channel→discussion group |
| Channel stats | ❌ | Gap: view/share analytics |
| Anonymous group admins | ❌ | Gap: low priority |

## Calls & live

| Feature | Status | Notes |
|---|---|---|
| 1:1 E2E calls | 🚧 | WebRTC; gap: E2E key verification UI (emoji) |
| Group voice chats (live, thousands) | ✅ | Shipped: self-built SFU audio rooms |
| Video in group voice chats | ✅ | Shipped: SFU group calls with video |
| Live streams with unlimited viewers | ❌ | Gap: RTMP/HLS pipeline |
| Screen sharing | ❌ | Gap: getDisplayMedia |
| Call recording | ❌ | Gap: SFU recording |
| Noise suppression | ❌ | Gap: client DSP |

## Platform & ecosystem

| Feature | Status | Notes |
|---|---|---|
| Bot API (full) | 🚧 | Core shipped (bot accounts, getUpdates long-poll, webhooks, sendMessage); inline bots phase 2 |
| Inline bots (@bot query) | ❌ | Gap: bot platform |
| Mini Apps (web apps in chat) | ❌ | Gap: embedded webview SDK |
| Bot payments | ❌ | Gap: wallet hook for bots |
| QR login | ✅ | Scan-to-approve |
| Active sessions management | 🚧 | Sessions stored; gap: UI to list/revoke |
| Proxy support (MTProto/SOCKS5) | ❌ | Gap: not applicable to our transport |
| Folders for chats | ❌ | Gap: chat folders |
| Archive chats | ❌ | Gap: archived flag |
| Multi-account in one app | ❌ | Gap: account switcher |
| Usernames & public links (t.me/u) | 🚧 | Usernames exist; gap: public URL routing |
| People nearby / local groups | ❌ | Gap: geo discovery |
| Premium subscription (gated features) | ❌ | Gap: plans entity |

## Privacy & security

| Feature | Status | Notes |
|---|---|---|
| 2FA (cloud password) | ✅ | TOTP; Telegram uses SRP password — equivalent strength |
| Passkey/biometric app lock | 🚧 | Passkey login exists; gap: local app-lock screen |
| Phone number privacy (who can see/find) | ❌ | Gap: privacy settings matrix |
| Blocked users | ✅ | POST/DELETE /api/users/{id}/block, enforced in messaging |
| Auto-delete account if away | ❌ | Gap: account TTL worker |
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
