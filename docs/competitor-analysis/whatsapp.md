# WhatsApp — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

WhatsApp's core differentiators: phone-number identity, default E2E encryption,
status updates, high-quality calls, and massive global reach with minimal UI.

| Area | WhatsApp | ChatApp | Status |
|------|----------|---------|--------|
| Identity | phone-number signup, OTP | phone verification with 200+ country codes + flags, email/password, Google, passkeys, QR login | ✅ |
| Messaging | E2E by default, ticks (sent/delivered/read), replies, forwards, star | E2E opt-in per conversation, ✓/✓✓ read receipts, reply, forward with attribution, pins | ✅ |
| Disappearing messages | per-chat timer (24h/7d/90d) | per-conversation TTL 1m/1h/24h/7d with live WS removal | ✅ |
| Voice/video | 1:1 and group calls | WebRTC 1:1 mesh calls plus meetings/group calls on our self-built SFU (services/sfu) with embedded STUN/TURN — no external media kit | ✅ |
| Status | 24h photo/video status with privacy | stories with view tracking, viewers list, reactions, replies | ✅ |
| Groups | 1024 members, admin roles, mentions | groups with owner/admin/member roles, member management UI | ✅ |
| Media | photos, video, voice notes, docs | media service uploads, voice messages with inline player | ✅ |
| Payments | WhatsApp Pay (limited countries) | multi-chain crypto wallet, P2P with KYC, 200+ countries | ✅ (broader) |
| Business | catalogs, business API | ads platform with global targeting + creator monetization | ✅ (different model) |
| Multi-device | linked devices, QR pairing | QR-code login (Telegram-style approve flow) | ✅ |

## Implemented parity — batch 5 (disappearing messages, member management)
- **Disappearing messages**: per-conversation timer (`PUT /api/conversations/{id}/ttl`,
  0/1m/1h/24h/7d). New messages get `expires_at`; list and search exclude expired
  messages; a 30s sweeper permanently deletes them and pushes `messages_expired`
  over WS so they vanish live from every client. DMs: either member can toggle;
  groups/channels: owner/admin only (`ttl_changed` broadcast).
- **Member management**: `GET /api/conversations/{id}/members` with roles;
  chat UI members panel lists everyone, owner/admin can remove members and add
  new ones from user search.
