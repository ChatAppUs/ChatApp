# imo — feature inventory vs ChatApp

Legend: ✅ implemented · 🚧 partial · ❌ missing

imo is strongest in low-bandwidth markets (South Asia, Middle East, Africa):
free international calls over 2G/3G, lightweight client, massive group voice
rooms. These are the differentiators to match.

## Calls (imo's core strength)

| Feature | Status | Notes |
|---|---|---|
| Free international audio calls | ✅ | WebRTC; gap: PSTN is not imo's model either |
| Video calls on 2G/3G (adaptive quality) | 🚧 | WebRTC adapts; gap: explicit low-bitrate profile + simulcast |
| Group video calls | ✅ | Mesh ≤8 |
| Large group voice chat rooms (drop-in) | ❌ | Gap: SFU audio rooms with speaker/listener roles |
| Call quality indicator | ❌ | Gap: client RTCP stats UI |
| Data-usage saver mode | ❌ | Gap: client bitrate caps |
| Call on slow networks auto-audio-only | ❌ | Gap: adaptive downgrade logic |

## Messaging

| Feature | Status | Notes |
|---|---|---|
| Text/photo/video messages | ✅ | |
| Voice messages | ✅ | MediaRecorder capture, inline audio player |
| Stickers (big catalog) | ❌ | Gap: sticker packs |
| Group chats (up to 100k) | 🚧 | Groups exist; gap: scale validation |
| Stories | ✅ | |
| Chat backup/restore | ❌ | Gap: export/import |
| Message translation | ❌ | Gap: translation hook |
| Disappearing messages | ✅ | Per-conversation TTL + sweeper with live WS removal |

## Discovery & social (imo's engagement layer)

| Feature | Status | Notes |
|---|---|---|
| People nearby | ❌ | Gap: geo discovery with privacy controls |
| Voice club / audio rooms discovery | ❌ | Gap: room directory |
| Levels & gamification (active-user levels) | ❌ | Gap: XP/levels entity |
| Virtual gifts in rooms | ❌ | Gap: wallet-backed gifts |
| Big groups directory by interest | ❌ | Gap: public group discovery |
| Profile visitors ("who viewed me") | ❌ | Gap: profile_views table |

## International reach

| Feature | Status | Notes |
|---|---|---|
| Phone-number-first registration | ✅ | 245 countries, flags, verification codes |
| Low-end Android support | 🚧 | Native app exists; gap: APK size/memory budget |
| Offline message delivery | ✅ | Server-persisted, sync on connect |
| Multi-language UI | 🚧 | 8 locales (EN/ES/FR/DE/PT/AR/HI/ZH, RTL); gap: imo ships 30+ |

## Privacy

| Feature | Status | Notes |
|---|---|---|
| Block contacts | ✅ | Block/unblock enforced in messaging |
| Last-seen / online privacy | ❌ | Gap: privacy settings |
| Screenshot alert (secret chats) | ❌ | Gap: best-effort client signal |
| Encrypted chats | ✅ | E2EE key relay |

## Implementation priority for ChatApp

1. Low-bandwidth call profile (simulcast + adaptive audio-only downgrade)
2. Group voice rooms (SFU) — imo's stickiest feature
3. Presence privacy granularity
4. Locale expansion to 30+ languages
5. People-nearby with strict privacy defaults
6. Profile visitors + gamification
