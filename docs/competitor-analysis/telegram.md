# Telegram — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

## Core messaging

| Feature | Status | Notes |
|---|---|---|
| Cloud chats (multi-device sync) | ✅ | Server-persisted, WS realtime |
| Secret chats (E2EE) | ✅ | ECDH P-256 key relay |
| Self-destruct timer (secret chats) | ❌ | Gap: ttl_seconds on messages + sweeper |
| Message edit (with "edited" mark) | ✅ | |
| Delete for everyone / for me | 🚧 | delete-for-everyone exists; gap: per-user deletion |
| Reactions (custom emoji sets) | 🚧 | Basic reactions; gap: custom emoji |
| Replies with quote | ❌ | Gap: reply_to_id + quote rendering |
| Forward with attribution | ❌ | Gap: forwarded_from on messages |
| Pin messages (multiple, in chats/groups) | ❌ | Gap: pinned_messages table |
| Saved Messages (self-chat) | ❌ | Gap: conversation kind=saved |
| Scheduled messages | ❌ | Gap: send_at + scheduler |
| Silent messages (no notification) | ❌ | Gap: silent flag |
| Drafts synced across devices | ❌ | Gap: drafts table + WS sync |
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
| Files up to 2GB (4GB Premium) | ❌ | Gap: chunked upload + 16MB cap removal |
| Voice messages with waveform | ❌ | Gap: audio recorder + waveform data |
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
| Join requests & invite links | ❌ | Gap: invite_links table |
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
| Group voice chats (live, thousands) | ❌ | Gap: SFU audio rooms |
| Video in group voice chats | ❌ | Gap: SFU |
| Live streams with unlimited viewers | ❌ | Gap: RTMP/HLS pipeline |
| Screen sharing | ❌ | Gap: getDisplayMedia |
| Call recording | ❌ | Gap: SFU recording |
| Noise suppression | ❌ | Gap: client DSP |

## Platform & ecosystem

| Feature | Status | Notes |
|---|---|---|
| Bot API (full) | ❌ | Gap: bot accounts + long-poll/webhook API |
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
| Blocked users | ❌ | Gap: blocks table |
| Auto-delete account if away | ❌ | Gap: account TTL worker |
| Report spam | ✅ | |

## Implementation priority for ChatApp

1. Saved Messages + drafts sync (daily-use retention)
2. Reply-with-quote + forward with attribution (messaging basics)
3. Pin messages + message formatting entities
4. Voice messages (recorder + waveform)
5. Invite links + public group handles (growth mechanic)
6. Bot API (platform/ecosystem moat)
7. Group voice chats via SFU
