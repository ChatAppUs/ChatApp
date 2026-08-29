# Facebook — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

## Accounts & identity

| Feature | Status | Notes |
|---|---|---|
| Email/phone registration | ✅ | + username, 245 dial codes with flags |
| Google/SSO login | ✅ | Google ID-token sign-in (JWKS verified) |
| 2FA (TOTP/SMS) | ✅ | RFC 6238 TOTP; SMS codes via Twilio Verify |
| Passkey / biometric login | ✅ | WebAuthn platform authenticators |
| Trusted contacts / account recovery | ❌ | Gap: nominate friends to help recover account |
| Profile lock (non-friends see limited info) | ❌ | Gap: privacy setting |
| Legacy contact / memorialization | ❌ | Gap: low priority |
| Multiple profiles per account | ❌ | Gap: FB allows up to 4 extra profiles |

## Feed & posting

| Feature | Status | Notes |
|---|---|---|
| Text/image/video posts | ✅ | |
| Post audience selector (public/friends/only me/custom) | 🚧 | Gap: per-post audience enum + feed filtering |
| Feeling/activity stickers on posts | ❌ | Gap: metadata enum, low effort |
| Location check-in on posts | ❌ | Gap: lat/lng + place name on posts |
| Tag people in posts (not just @mention text) | 🚧 | Mentions exist; formal post_tags table missing |
| Reactions beyond like (love/haha/wow/sad/angry) | 🚧 | Message reactions exist; post reactions are like-only |
| Comment threads (nested replies) | 🚧 | Comments flat; gap: parent_id threading |
| Comment ranking (top/newest) | ❌ | Gap: sort options |
| Share/repost with quote | ❌ | Gap: shares table + counter |
| Pinned post on profile | ❌ | Gap: users.pinned_post_id |
| Post editing with history | ❌ | Gap: posts.edited_at |
| Scheduled posts | ❌ | Gap: publish_at column + worker |
| Albums / multi-photo grid | 🚧 | media_urls JSON array exists; no album entity |
| 3D photos / avatars | ❌ | Gap: out of scope, low value |

## Groups & Pages

| Feature | Status | Notes |
|---|---|---|
| Public/private groups with posts | ❌ | Gap: our groups are chat-only; need content groups with membership, roles (admin/mod), join requests, group feed |
| Pages (business/creator) with followers | ❌ | Gap: pages entity, page roles, page feed |
| Events (RSVP, reminders) | ❌ | Gap: events entity |
| Marketplace | ❌ | Gap: listings entity — large surface, phase 2 |
| Fundraisers | ❌ | Gap: low priority |

## Stories

| Feature | Status | Notes |
|---|---|---|
| 24h photo/video stories | ✅ | |
| Story replies (DM from story) | ❌ | Gap: story_id on message |
| Story reactions | ❌ | Gap: story_reactions table |
| Story highlights (permanent collections) | ❌ | Gap: highlights entity |
| Story privacy (close friends) | ❌ | Gap: audience flag |
| Story viewer list | ❌ | Gap: story_views tracking |
| Text stories / stickers / music | ❌ | Gap: composer tools |

## Reels / video

| Feature | Status | Notes |
|---|---|---|
| Short video feed | ✅ | |
| Watch-time tracking | ❌ | Gap: reel_watch_events for ranking |
| Reel comments/likes/shares | 🚧 | Likes exist; gap: comments & shares on reels |
| Remix/duet | ❌ | Gap: needs editor |
| Sounds/music library | ❌ | Gap: licensed audio is a legal, not tech, problem |
| Creator monetization on reels | 🚧 | RPM-based earnings exist; gap: per-reel analytics |

## Messaging (Messenger parity)

| Feature | Status | Notes |
|---|---|---|
| 1:1 & group chat, realtime | ✅ | |
| Message edit/delete/reactions | ✅ | |
| Typing indicators, read receipts, presence | ✅ | |
| E2EE | ✅ | ECDH key relay; client-side cipher |
| Voice messages | ❌ | Gap: audio message type + recorder UI |
| Video notes | ❌ | Gap: recorder UI |
| Message requests (non-friends) | ❌ | Gap: request inbox before chat opens |
| Nicknames per chat | ❌ | Gap: low effort |
| Chat themes/colors | ❌ | Gap: cosmetic |
| Polls in chat | ✅ | Post polls exist; gap: attach to chat message |
| Location sharing (live) | ❌ | Gap: location message type |
| Payments in chat | 🚧 | Wallet P2P exists; gap: pay-in-chat UI hook |

## Calls

| Feature | Status | Notes |
|---|---|---|
| 1:1 audio/video | ✅ | WebRTC mesh |
| Group calls | ✅ | Mesh ≤8; gap: SFU for larger |
| Messenger Rooms (drop-in video rooms) | ❌ | Gap: persistent room links |
| Screen sharing | ❌ | Gap: getDisplayMedia in client |
| Call recording | ❌ | Gap: needs SFU/compositor |
| AR effects/filters | ❌ | Gap: low priority |

## Monetization

| Feature | Status | Notes |
|---|---|---|
| In-stream ads revenue share | 🚧 | Flat RPM earnings exist; gap: true ad-share accounting |
| Stars (virtual gifts) | ❌ | Gap: wallet-backed gift ledger |
| Fan subscriptions | ❌ | Gap: recurring subscription entity |
| Professional dashboard (analytics) | ❌ | Gap: per-content analytics rollup |

## Safety & moderation

| Feature | Status | Notes |
|---|---|---|
| Report content/users | ✅ | reports table + admin queue |
| Block users | ❌ | Gap: blocks table + feed/chat filtering |
| Comment filters / hidden words | ❌ | Gap: per-user keyword filters |
| Community Notes equivalent | ❌ | Gap: low priority |
| AI moderation queue | 🚧 | Rust security service has heuristic scorer; gap: media moderation |

## Notifications

| Feature | Status | Notes |
|---|---|---|
| In-app notifications | ✅ | |
| Push (FCM/APNs/Web Push) | ❌ | Gap: device tokens + push worker |
| Email notifications | ❌ | Gap: digest worker (SMTP exists) |

## Implementation priority for ChatApp

1. Post audience selector + block list (privacy fundamentals)
2. Story replies/reactions/viewer list (engagement loop)
3. Nested comments + post reactions beyond like
4. Groups with content feeds (biggest structural gap)
5. Push notifications (retention)
6. Voice messages (messaging parity)

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
