
## Implemented parity — batch 5 (disappearing messages, member management)
- **Disappearing messages**: per-conversation timer (`PUT /api/conversations/{id}/ttl`,
  0/1m/1h/24h/7d). New messages get `expires_at`; list and search exclude expired
  messages; a 30s sweeper permanently deletes them and pushes `messages_expired`
  over WS so they vanish live from every client. DMs: either member can toggle;
  groups/channels: owner/admin only (`ttl_changed` broadcast).
- **Member management**: `GET /api/conversations/{id}/members` with roles;
  chat UI members panel lists everyone, owner/admin can remove members and add
  new ones from user search.
