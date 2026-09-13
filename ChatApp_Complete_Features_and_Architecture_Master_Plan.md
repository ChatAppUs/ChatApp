# ChatApp — Complete Facebook + TikTok + Telegram + X Feature & Architecture Master Plan

> **Purpose:** Build ChatApp as a real production platform combining the strongest capabilities of Facebook, TikTok, Telegram, and X/Twitter, while adding unique privacy, AI, creator-economy, wallet, commerce, community, and programmable-platform capabilities.
>
> **Important requirement:** Reels and Video are first-class systems. ChatApp must cover the full practical feature set and functionality expected from both Facebook-style video/social experiences and TikTok-style short-video discovery and creation.

---

## Implementation status — audited 2026-09-13

The repository implementation is tracked by `feature-registry.json`, validated by `scripts/validate-feature-registry.py`, and enforced in `.github/workflows/validate.yml`. The registry covers the documented P0/P1/P2 platform surfaces across Web, Android, iOS, Desktop, Extension, Backend, and Database. Repository parity, web/admin production builds, ML compilation, extension syntax, migration ordering, API readiness wiring, and backup-script syntax are validated in this checkout. Provider-backed, real-device, load, disaster-recovery, and production deployment tests require their configured environments and are not claimed as executed here.

| Status area | Repository evidence | Current result |
|---|---|---|
| Feature registry and cross-platform parity | `feature-registry.json`, `scripts/validate-feature-registry.py`, `tests/parity_check.py` | Implemented and statically validated |
| CI and production safeguards | `.github/workflows/validate.yml`, `/health`, `/ready`, `docker-compose.yml`, `scripts/backup-restore.sh` | Implemented and syntax-validated |
| Client and service feature surface | `apps/`, `services/`, `infra/db/` | Implemented source coverage; runtime environments required for final certification |
| Creator analytics integration | `services/api/handlers_gap9.go`, `infra/db/025_gap_pack9.sql`, web `creator/page.tsx`, Android `MonetizeScreen.kt`, iOS `FeatureClient.swift`/`FeatureViews.swift` | Implemented: Web, Android, and iOS Creator Studio surfaces consume daily reach, impressions, watch time, follower growth, and top-sound insights |
| LuckyDraw | `infra/db/030_luckydraw.sql`, `services/api/handlers_luckydraw.go`, `services/api/main.go`, web `apps/web/src/app/luckydraw/page.tsx`, admin `apps/admin/src/components/LuckyDrawTab.tsx`, `tests/luckydraw_test.py` | Implemented: draws, ticket purchases on the double-entry ledger, audited winner selection with the unique-user rule, prize settlement, and admin lifecycle |
| Professional analytics dashboard | `services/api/handlers_gap4.go`, `services/api/main.go`, `apps/web/src/app/analytics/page.tsx`, `apps/web/src/components/Nav.tsx` | Implemented: authenticated web dashboard consumes account posts, audience, engagement, seven-day shares, and earnings metrics |
| Anonymous guest session | `services/api/handlers_guest.go`, `services/api/main.go` (`POST /api/auth/guest`), web `apps/web/src/lib/api.ts` (`startGuestSession`), login/register pages, `Nav.tsx` | Implemented: device-local ephemeral guest token (no account row) with a web `Continue without account` surface |
| Offline multi-hop mesh (store-and-forward) | `infra/db/031_mesh.sql`, `services/api/handlers_mesh.go`, `services/api/main.go` (`/api/mesh/*`) | Implemented (backend): device registration, encrypted store-and-forward enqueue/dedup, poll delivery, one-hop relay, relay policy, status; native device transport pending |


## No stubs · no mocks · no fake data — audit 2026-09-13

The repository is audited against the requirement that **no hardcoded values, no mock data, no fake implementations, and no stubs are permitted** — everything is fully dynamic, real logic, and operationally complete.

**Audit result: PASS.** A full scan of every backend service (`services/api`, `services/mesh`, `services/sfu`, `services/sfu-forwarder`, `services/realtime`, `services/counters`, `services/media`, `services/transcode`, `services/authn`, `services/security`, `services/ml`), all infrastructure SQL (`infra/db/`), all clients (Web, Admin, Android, iOS, Desktop, Extension), and the test suite found **no stubs, no mocks, no fake/dummy implementations, and no hardcoded secrets or credentials**.

- **Configuration is fully environment-driven.** All secrets, keys, tokens, ports, URLs, and provider credentials are read from environment variables via `services/api/config.go` and `.env.example` — never hardcoded in source. Production requires real values (e.g. `JWT_SECRET`, `WALLET_MASTER_SEED`, `SIGNING_SECRET`); empty values disable the corresponding integration rather than substituting fake data.
- **Every flagged pattern was verified as real logic.** The only matches for stub/mock/placeholder keywords are legitimate: HTML `placeholder` input attributes, i18n placeholder strings, a STUN/TURN protocol length-field placeholder, a default mesh storage quota, and a bounded JWT cache — none are fake implementations.
- **Tests run against a live API, not mocks.** The integration and feature test suites (`tests/*.py`) explicitly state "No mocks" and exercise real HTTP/database/provider flows.
- **The native offline mesh engine** (`services/mesh/`) is real, compiles, passes `go vet`, and passes `go test` (encryption round-trip, packet marshal, dedup, store-and-forward, node-to-node UDP delivery, and scaling).
- **No hardcoded mesh diameter.** The mesh hop budget scales with device count (`scale.go`), so coverage grows with the network instead of a fixed constant.

The only items not executable in this checkout are those requiring external runtime environments (a configured database, real device Bluetooth/Wi-Fi Direct, provider credentials, and native toolchains) — these are environment-dependent validation, not stubs or fake implementations.


---

# Table of Contents

1. Product Vision
2. Core Product Principles
3. Unified Platform Model
4. Facebook-Complete Feature Coverage
5. TikTok-Complete Reels and Video Coverage
6. Unified Content System
7. Feed and Recommendation System
8. Messaging and Telegram-Class Communication
9. X/Twitter-Class Public Conversation
10. Communities, Groups, Pages and Channels
11. Stories
12. Reels
13. Long Video
14. Live Streaming
15. Creator Studio
16. AI Features
17. Search and Discovery
18. Social Graph and Privacy
19. Calls and Live Rooms
20. Marketplace and Commerce
21. Wallet, P2P, Staking, Crypto Card and Conversion
22. Lucky Draw / Rewards
23. Mini Apps, Bots and Developer Platform
24. Cross-Platform Feature Parity
25. Architecture
26. Go Usage
27. Rust Usage
28. C++ Usage
29. Next.js and TypeScript Usage
30. Kotlin and Swift
31. Python and ML
32. Data Architecture
33. Event-Driven Architecture
34. Media Pipeline
35. Security
36. Moderation
37. Reliability and Observability
38. Feature Priorities
39. Production Definition of Done
40. Final Product Roadmap

---

# 1. Product Vision

ChatApp should not be a collection of copied applications.

The target product is:

```text
CHATAPP

Communication
+
Social Network
+
Video Discovery
+
Creator Economy
+
Communities
+
Privacy
+
AI
+
Commerce
+
Wallet
+
Programmable Platform
```

## Strategic Position

ChatApp should become:

> **An AI-powered, privacy-aware, cross-platform communication and social ecosystem combining messaging, social relationships, communities, public conversation, short-form video, long-form video, live interaction, creator tools, commerce and digital financial services.**

---

# 2. Core Product Principles

Every major feature must follow these principles.

## 2.1 Real implementation only

No:

- Demo implementation
- Fake UI
- Placeholder functionality
- Mock production data
- Fake API response
- Empty buttons
- Skeleton presented as completed functionality
- Hidden broken features
- Simulated payment
- Simulated security
- Fake encryption

A production feature must have:

```text
UI
+
API
+
Database
+
Authorization
+
Validation
+
Error handling
+
Tests
+
Monitoring
+
Documentation
```

## 2.2 Cross-platform parity

Required application surfaces:

```text
Web
Android
iOS
Desktop
Browser Extension
Admin
```

The same core capability must work consistently across supported platforms.

---

# 3. Unified Platform Model

ChatApp should operate with multiple connected graphs.

```text
                     CHATAPP IDENTITY
                            |
       -----------------------------------------
       |                |                |
   SOCIAL GRAPH   COMMUNICATION GRAPH   CONTENT GRAPH
       |                |                |
    Friends            Messages          Posts
    Followers          Calls             Videos
    Communities        Groups            Reels
    Relationships      Channels          Stories
       |                |                |
       -----------------------------------------
                            |
                      INTEREST GRAPH
                            |
                    DISCOVERY ENGINE
                            |
              ----------------------------
              |            |             |
             AI          ECONOMY       PRIVACY
              |            |             |
        Assistant       Wallet         E2EE
        Search          Commerce       Identity Modes
        Ranking         Creator        Privacy Controls
```

---

# 4. Facebook-Complete Feature Coverage

## 4.1 Profiles

Each user needs:

- Profile photo
- Cover image
- Name
- Username
- Bio
- Pronouns where supported
- Website links
- Location controls
- Work information
- Education information
- Relationship privacy controls
- Featured content
- Pinned posts
- Profile verification
- Creator profile
- Professional profile
- Business profile

## 4.2 Relationship system

Do not limit relationships to `follow` and `friend`.

```text
FOLLOWER
FOLLOWING
FRIEND
CLOSE_FRIEND
FAMILY
COLLEAGUE
CLASSMATE
COMMUNITY_MEMBER
CREATOR_SUPPORTER
SUBSCRIBER
BUSINESS_CUSTOMER
RESTRICTED
MUTED
BLOCKED
```

## 4.3 Posts

Support:

- Text
- Image
- Multiple images
- Video
- Reels
- Links
- Polls
- GIFs
- Location
- Feeling/activity
- Tagged people
- Tagged pages
- Tagged communities
- Product tags
- Event tags
- Hashtags
- Scheduled publishing
- Drafts
- Editing
- Deletion
- Archive
- Pinning

## 4.4 Reactions

Support configurable reactions:

```text
LIKE
LOVE
LAUGH
WOW
SAD
ANGRY
SUPPORT
CUSTOM_REACTION
```

Also support:

- Reaction counts
- Reaction privacy
- Who reacted
- Animated reactions where appropriate
- Creator analytics

## 4.5 Comments

Full comment functionality:

- Nested replies
- Mentions
- Reactions
- GIF comments
- Image comments
- Video comments where appropriate
- Edit
- Delete
- Pin
- Sort
- Filter
- Moderation
- Translation
- AI summary for very large discussions

## 4.6 Shares

Support:

- Share to feed
- Share to story
- Share to reel where appropriate
- Share to group
- Share to community
- Share to message
- External share
- Quote share
- Private share

---

# 5. TikTok-Complete Reels and Video Coverage

# Reels and Video are mandatory first-class systems

ChatApp must not treat video as an attachment.

Video requires its own:

```text
Creation System
Editing System
Media Pipeline
Discovery System
Recommendation System
Creator Analytics
Moderation System
Monetization System
Copyright System
```

---

# 6. Full Short-Video / Reels Feature Set

## 6.1 Video recording

Support:

- Front camera
- Rear camera
- Camera switching
- Multiple clips
- Pause and resume
- Countdown timer
- Hands-free recording
- Recording speed
- Slow motion
- Fast motion
- Teleprompter
- Flash
- Beauty controls
- Background blur
- Green screen
- Background replacement

## 6.2 Video creation workflow

```text
OPEN CAMERA
      |
RECORD / IMPORT
      |
EDIT
      |
AUDIO
      |
CAPTIONS
      |
EFFECTS
      |
COVER
      |
DESCRIPTION
      |
HASHTAGS
      |
MENTIONS
      |
PRIVACY
      |
PUBLISH
```

## 6.3 Full video editor

```text
VIDEO EDITOR
|
|-- Trim
|-- Split
|-- Cut
|-- Crop
|-- Rotate
|-- Resize
|-- Timeline
|-- Multiple clips
|-- Rearrange clips
|-- Speed
|-- Reverse
|-- Freeze frame
|-- Filters
|-- Effects
|-- Transitions
|-- Text
|-- Stickers
|-- Emojis
|-- Captions
|-- Voiceover
|-- Sound effects
|-- Background music
|-- Audio volume
|-- Audio mixing
|-- Noise reduction
|-- Color adjustment
|-- Brightness
|-- Contrast
|-- Saturation
|-- Cover selection
```

---

# 7. Full Audio and Music System

Support:

- Original sound
- Music library
- Sound effects
- Voiceover
- Extract audio from video
- Save sound
- Favorite sound
- Reuse sound
- Trending sounds
- Sound pages
- Sound analytics
- Audio attribution
- Audio copyright controls

Sound page:

```text
SOUND
|
|-- Sound name
|-- Creator
|-- Original source
|-- Videos using sound
|-- Save sound
|-- Use sound
|-- Trending analytics
```

Licensing must be handled correctly for commercial music.

---

# 8. Duet, Stitch and Remix

ChatApp should support:

## Duet

```text
ORIGINAL VIDEO | USER VIDEO
```

Layouts:

- Side by side
- Top/bottom
- Picture in picture
- Green screen

## Stitch

```text
ORIGINAL CLIP
       +
NEW USER CLIP
```

## Remix

A universal remix system:

```text
ORIGINAL CONTENT
        |
        +-- Video Remix
        +-- Audio Remix
        +-- Post Remix
        +-- Clip Remix
```

The system must preserve:

- Original attribution
- Content lineage
- Permissions
- Revenue attribution where applicable

---

# 9. Video Replies

Users should be able to reply with:

- Text
- Image
- GIF
- Video
- Short video reply

Example:

```text
VIDEO
  |
COMMENT
  |
VIDEO REPLY
```

Video replies should connect to the original discussion.

---

# 10. Full Screen Video Experience

The Reels/FYP player should support:

```text
VIDEO
|
|-- Follow
|-- Like
|-- Comment
|-- Share
|-- Save
|-- Remix
|-- Duet
|-- Stitch
|-- Not Interested
|-- Report
|-- Block
|-- Creator Profile
|-- Sound
|-- Product
|-- Location
|-- Community
```

---

# 11. Multiple Video Feeds

ChatApp must not use one feed for all content.

Required feeds:

```text
FOR YOU
FOLLOWING
FRIENDS
TRENDING
LOCAL
COMMUNITY
LIVE
NEW
CREATOR
TOPIC
SHOPPING
```

---

# 12. Advanced FYP Recommendation System

Architecture:

```text
USER EVENT
     |
EVENT COLLECTION
     |
FEATURE PROCESSING
     |
CANDIDATE GENERATION
     |
FILTERING
     |
RANKING
     |
RE-RANKING
     |
FOR YOU PAGE
```

## 12.1 User signals

Track privacy-respectfully:

```text
VIEW
WATCH_DURATION
COMPLETION
REWATCH
LIKE
COMMENT
SHARE
SAVE
FOLLOW
PROFILE_VIEW
SKIP
NOT_INTERESTED
HIDE
REPORT
```

## 12.2 Candidate sources

```text
FOLLOW GRAPH
FRIEND GRAPH
INTEREST GRAPH
SIMILAR USERS
SIMILAR CONTENT
TRENDING
FRESH CONTENT
LOCAL CONTENT
COMMUNITY CONTENT
CREATOR CONTENT
```

## 12.3 Ranking goals

Do not optimize only for watch time.

Balance:

```text
RELEVANCE
+
USER SATISFACTION
+
DIVERSITY
+
NOVELTY
+
SAFETY
+
FRESHNESS
```

---

# 13. Explainable Recommendations

Users should have:

> Why am I seeing this?

Possible explanations:

- You watched similar videos.
- You follow related creators.
- Your friends interacted with similar content.
- This topic is trending.
- This content is relevant to a community you joined.

Controls:

```text
MORE LIKE THIS
LESS LIKE THIS
REMOVE TOPIC
HIDE CREATOR
NOT INTERESTED
RESET RECOMMENDATIONS
```

---

# 14. Full Long-Form Video System

ChatApp must support long-form video in addition to Reels.

Features:

- Upload long video
- Video chapters
- Timestamps
- Description
- Thumbnail
- Subtitles
- Multiple audio tracks
- Resolution selection
- Playback speed
- Picture-in-picture
- Continue watching
- Watch history
- Playlists
- Series
- Episodes
- Premieres
- Scheduled publishing
- Video analytics
- Comments
- Clips
- Share timestamp

## AI features

- Automatic chapters
- Automatic summary
- Highlight detection
- Clip generation
- Subtitle generation
- Translation
- Dubbing

---

# 15. Universal Video Object

All video content should use a shared model.

```text
VIDEO
|
|-- id
|-- creator
|-- source
|-- duration
|-- visibility
|-- metadata
|-- captions
|-- audio
|-- thumbnail
|-- moderation
|-- analytics
|-- monetization
```

Video types:

```text
REEL
LONG_VIDEO
LIVE_RECORDING
CLIP
STORY_VIDEO
VIDEO_REPLY
AD
```

---

# 16. Facebook-Style Video Coverage

ChatApp video should also support:

- Video posts
- Feed video
- Recommended video
- Saved videos
- Watch history
- Continue watching
- Video collections
- Creator pages
- Livestream video
- Video comments
- Sharing to communities
- Group video
- Cross-posting
- Video playlists
- Family/age controls where applicable

---

# 17. Stories

Support:

- Photo story
- Video story
- Text story
- Music story
- Poll story
- Question story
- Emoji reaction
- Mention
- Link
- Location
- Countdown
- Close friends
- Story privacy
- Story archive
- Highlights
- Story analytics

Story lifecycle:

```text
CREATE
  |
PUBLISH
  |
VIEW
  |
REPLY / REACT
  |
EXPIRE
  |
ARCHIVE
```

---

# 18. Full Live Streaming System

Support:

```text
MOBILE LIVE
WEB LIVE
DESKTOP LIVE
CREATOR LIVE
COMMUNITY LIVE
EVENT LIVE
SHOPPING LIVE
```

## Live features

- Camera live
- Screen live
- Multiple hosts
- Guests
- Co-hosts
- Moderators
- Live chat
- Reactions
- Polls
- Q&A
- Gifts/tips
- Product pins
- Subscriptions
- Recording
- Replay
- Clips

---

# 19. Live Moderation

Required:

- Chat moderation
- Keyword filtering
- Slow mode
- Mute
- Timeout
- Ban
- Moderator controls
- Report
- AI risk detection
- Human review workflow

---

# 20. Live Shopping

```text
LIVE STREAM
      |
PRODUCT PIN
      |
PRODUCT PAGE
      |
CHECKOUT
      |
PAYMENT
      |
ORDER
```

Features:

- Product carousel
- Product pin
- Inventory status
- Discount
- Coupon
- Live purchase analytics

---

# 21. Creator Studio

Create a full professional dashboard.

```text
CREATOR STUDIO
|
|-- Content
|-- Reels
|-- Videos
|-- Live
|-- Stories
|-- Analytics
|-- Audience
|-- Revenue
|-- Comments
|-- Copyright
|-- Monetization
|-- AI Tools
```

---

# 22. Creator Analytics

Required metrics:

```text
VIEWS
UNIQUE_VIEWERS
WATCH_TIME
AVERAGE_WATCH_TIME
COMPLETION_RATE
REWATCH_RATE
LIKES
COMMENTS
SHARES
SAVES
FOLLOWER_GROWTH
PROFILE_VISITS
FOLLOW_CONVERSION
REVENUE
```

Video retention:

```text
100% |\
     | \
 75% |  \
     |   \
 50% |    \
     |     \
 25% |      \
     +--------------
       VIDEO TIME
```

---

# 23. AI Creator Tools

## AI captions

- Speech-to-text
- Subtitle generation
- Caption editing
- Multiple languages

## AI translation

```text
ORIGINAL VIDEO
      |
      +-- English captions
      +-- Bangla captions
      +-- Arabic captions
      +-- Spanish captions
```

## AI dubbing

Optional translated audio with explicit labeling.

## AI clips

```text
LONG VIDEO
     |
AI ANALYSIS
     |
BEST MOMENTS
     |
SHORT CLIPS
```

## AI highlights

Detect:

- High-interest moments
- Questions
- Key statements
- Topic changes

---

# 24. AI Content Creation

Creators may request:

- Post draft
- Video script
- Short-video script
- Hook
- Title
- Description
- Captions
- Hashtag suggestions
- Thumbnail suggestions

AI must not silently publish content.

Human approval is required before publication.

---

# 25. Social Graph

ChatApp needs:

```text
USER
 |
 +-- Friends
 +-- Followers
 +-- Following
 +-- Communities
 +-- Interests
 +-- Creators
 +-- Pages
```

Relationship controls:

- Accept
- Reject
- Remove
- Restrict
- Mute
- Block
- Close friend
- Custom list

---

# 26. Privacy System

Visibility:

```text
PUBLIC
FOLLOWERS
FRIENDS
CLOSE_FRIENDS
CUSTOM
PRIVATE
```

Every content type should support appropriate privacy.

---

# 27. Groups and Communities

A community should be a complete environment.

```text
COMMUNITY
|
+-- Feed
+-- Chat
+-- Topics
+-- Voice
+-- Video
+-- Events
+-- Files
+-- Polls
+-- Q&A
+-- Marketplace
+-- Moderation
+-- Analytics
+-- AI Assistant
```

Community types:

```text
PUBLIC
PRIVATE
SECRET
PAID
LOCAL
PROFESSIONAL
EDUCATIONAL
CREATOR
BUSINESS
```

---

# 28. Pages

Support:

- Public identity
- Business identity
- Creator identity
- Organization identity

Features:

- Followers
- Posts
- Video
- Reels
- Live
- Shop
- Events
- Analytics
- Admin roles
- Verification

---

# 29. Events

Support:

```text
PHYSICAL
VIRTUAL
HYBRID
```

Features:

- RSVP
- Tickets
- Waitlist
- Calendar
- Reminders
- Event chat
- Livestream
- Speakers
- Attendee networking
- QR check-in
- Recording
- Replay

Unique flow:

```text
EVENT
  |
TEMPORARY COMMUNITY
  |
CHAT + LIVE + NETWORKING
  |
ARCHIVE OR CONTINUE
```

---

# 30. Messaging

Conversation types:

```text
DIRECT
GROUP
CHANNEL
COMMUNITY
BUSINESS
BOT
AI
ANONYMOUS
TEMPORARY
SECRET
```

Required messaging features:

- Text
- Emoji
- Reactions
- Stickers
- GIFs
- Images
- Video
- Files
- Voice messages
- Location
- Contacts
- Polls
- Reply
- Forward
- Quote
- Edit
- Delete
- Pin
- Search
- Schedule
- Draft
- Translation

---

# 31. Advanced Messaging

Add:

- Message reminders
- Silent messages
- Message threads
- Topics
- Saved messages
- Message bookmarks
- Media gallery
- Link gallery
- File gallery
- Shared locations
- Voice transcription
- Message translation

---

# 32. X/Twitter-Class Public Conversation

Create:

# CHATAPP PULSE

```text
PULSE
|
+-- Trending
+-- Breaking
+-- Topics
+-- Conversations
+-- Communities
+-- Local
+-- Global
```

Support:

- Short posts
- Threads
- Quotes
- Replies
- Reposts
- Lists
- Topics
- Trends
- Spaces-style audio

---

# 33. Thread System

```text
ROOT POST
    |
    +-- REPLY A
    |
    +-- REPLY B
    |     |
    |     +-- REPLY
    |
    +-- REPLY C
```

Add:

- Branch view
- Chronological view
- Relevant view
- AI summary
- Context display

---

# 34. Community Context

Create a context system for disputed or potentially misleading content.

```text
CONTENT
 |
 +-- Context
 +-- Sources
 +-- Corrections
 +-- Status
```

Use:

```text
AUTOMATION
+
TRUSTED CONTRIBUTORS
+
HUMAN REVIEW
+
APPEALS
```

Do not let AI alone become the final authority.

---

# 35. Voice and Video Calls

Support:

```text
1:1
GROUP
MEETING
COMMUNITY
WEBINAR
BROADCAST
```

Features:

- Voice
- Video
- Screen sharing
- Active speaker
- Grid
- Reactions
- Raise hand
- Polls
- Recording
- Breakout rooms
- Captions
- Translation
- Noise suppression
- Echo cancellation

---

# 36. Universal Live Room

Unify:

- Audio spaces
- Video meeting
- Webinar
- Podcast
- Broadcast

```text
LIVE ROOM
|
+-- AUDIO
+-- VIDEO
+-- MEETING
+-- WEBINAR
+-- PODCAST
+-- BROADCAST
```

Roles:

```text
HOST
CO_HOST
MODERATOR
SPEAKER
PANELIST
VIEWER
```

---

# 37. Search

Unified search:

```text
SEARCH
|
+-- People
+-- Posts
+-- Videos
+-- Reels
+-- Stories
+-- Communities
+-- Messages
+-- Events
+-- Products
+-- Creators
+-- Topics
```

## Semantic search

Support natural language:

> Find the video John sent me about blockchain last month.

This requires privacy-aware indexing.

---

# 38. AI Assistant

A personal assistant can help:

- Search
- Summarize
- Translate
- Remind
- Organize
- Draft content
- Manage events

Example:

```text
"Summarize this group discussion."
```

For private or end-to-end encrypted data, plaintext must not be silently exposed to server-side AI.

---

# 39. Wallet and Financial Platform

Based on the planned ChatApp ecosystem, financial systems should be modular.

```text
ECONOMY
|
+-- Wallet
+-- Ledger
+-- Conversion
+-- P2P
+-- Staking
+-- Crypto Card
+-- Creator Payments
+-- Commerce
+-- Rewards
```

## Critical rule

Never treat a simple mutable balance as the complete financial source of truth.

Use:

```text
TRANSACTION
 |
 +-- Idempotency
 +-- Validation
 +-- Ledger Entries
 +-- Status
 +-- Audit Trail
```

---

# 40. P2P System

Features:

- Send
- Receive
- Request
- Transaction history
- Receipts
- Limits
- Risk controls
- Fraud monitoring
- Dispute workflow where applicable

---

# 41. Conversion System

Required architecture:

```text
QUOTE
 |
VALIDATION
 |
LOCK / CONFIRM
 |
EXECUTION
 |
LEDGER
 |
RECEIPT
```

Never display a quote as guaranteed unless the underlying provider and transaction flow guarantee it.

---

# 42. Staking System

If legally and technically supported:

- Stake
- Unstake
- Lock period
- Reward calculation
- Reward history
- Risk disclosure
- Transaction history

Do not fabricate returns or guarantee profit.

---

# 43. Crypto Card System

Potential components:

```text
CARD
|
+-- Issuance
+-- Authorization
+-- Transaction
+-- Ledger
+-- Limits
+-- Security
+-- Dispute
```

This requires licensed/payment partners and regulatory review where required.

---

# 44. Lucky Draw and Rewards

For daily/monthly Lucky Draw functionality:

```text
CAMPAIGN
 |
+-- Eligibility
+-- Entry
+-- Rules
+-- Schedule
+-- Selection
+-- Verification
+-- Winner
+-- Reward
+-- Audit
```

Important:

The draw mechanism must be:

- Transparent
- Auditable
- Secure
- Legally reviewed

Do not implement a hidden or manipulable winner selection system.

---

# 45. Creator Economy

Monetization:

```text
SUBSCRIPTIONS
TIPS
GIFTS
PAID CONTENT
PAID COMMUNITIES
PAID EVENTS
PAID LIVE
DIGITAL PRODUCTS
AFFILIATE REVENUE
COMMERCE
ADVERTISING
BRAND DEALS
```

---

# 46. Marketplace

Support:

- Products
- Services
- Digital goods
- Shops
- Seller profiles
- Reviews
- Orders
- Returns
- Disputes
- Affiliate links

Connect products with:

```text
POST
VIDEO
REEL
LIVE
CREATOR
COMMUNITY
```

---

# 47. Commerce Graph

```text
PRODUCT
 |
 +-- SELLER
 +-- SHOP
 +-- POST
 +-- VIDEO
 +-- REEL
 +-- LIVE
 +-- CREATOR
 +-- AFFILIATE
```

---

# 48. Bots

Bot capabilities:

```text
BOT
|
+-- Commands
+-- Events
+-- Webhooks
+-- Messages
+-- Inline Actions
+-- AI
+-- Payments
+-- Mini Apps
+```
```

---

# 49. Mini Apps

Create a programmable application platform.

```text
MINI APP PLATFORM
|
+-- SDK
+-- Identity API
+-- Permission API
+-- Payment API
+-- Wallet API
+-- Notification API
+-- Storage API
```

Use cases:

- Games
- Shops
- Booking
- Education
- Productivity
- AI tools

---

# 50. Developer Platform

Provide:

- API keys
- OAuth
- Webhooks
- SDKs
- Sandbox
- Documentation
- Rate limits
- Permissions

SDK languages:

```text
TypeScript
Python
Go
Rust
Java
Kotlin
Swift
```

---

# 51. Cross-Platform Feature Parity

Create a feature registry.

Example:

```json
{
  "feature": "video_remix",
  "web": true,
  "android": true,
  "ios": true,
  "desktop": true,
  "extension": false
}
```

CI should validate required parity.

Feature status:

```text
IMPLEMENTED
TESTED
SUPPORTED
DEPRECATED
PARTIAL
```

---

# 52. Recommended Technology Architecture

```text
CLIENTS
|
+-- Next.js Web
+-- Kotlin Android
+-- Swift iOS
+-- Desktop
+-- Extension
|
API / EDGE
|
+-- Go Gateway
|
CORE SERVICES
|
+-- Go
+-- Rust
+-- C++
+-- Python
|
DATA
|
+-- PostgreSQL
+-- Redis
+-- Object Storage
+-- Search
+-- Event Stream
```

---

# 53. Where to Use Go

Go should be the primary backend orchestration language.

Use Go for:

- API Gateway
- REST APIs
- gRPC
- Identity orchestration
- Profiles
- Social graph services
- Posts
- Comments
- Communities
- Groups
- Events
- Notifications
- Business workflows
- Service-to-service APIs

Suggested services:

```text
api-gateway
identity
profiles
social-graph
content
community
events
notifications
commerce
creator
search-api
```

---

# 54. Where to Use Rust

Use Rust for security-sensitive and shared core systems.

Recommended:

```text
crypto-core
identity-keys
secure-storage
wallet-core
ledger-validation
transaction-validation
protocol-core
```

Rust can also support:

- Shared mobile core
- Desktop core
- WebAssembly modules

Do not invent cryptographic algorithms.

---

# 55. Where to Use C++

Use C++ only where profiling proves that native high-performance processing is needed.

Best candidates:

- Media processing
- Video pipeline
- Audio pipeline
- Specialized realtime processing
- FFmpeg integration
- High-performance codecs

Avoid C++ for ordinary CRUD APIs.

---

# 56. Next.js + TypeScript

Use Next.js for:

```text
WEB SOCIAL
WEB MESSAGING
PUBLIC PROFILES
PUBLIC POSTS
CREATOR STUDIO
COMMUNITIES
MARKETPLACE
WALLET UI
```

Use shared TypeScript packages:

```text
design-system
api-types
api-client
feature-flags
analytics
sdk
```

---

# 57. Kotlin

Native Android:

- Jetpack Compose
- Coroutines
- Offline database
- Camera
- Notifications
- Media
- Background upload
- Calls

---

# 58. Swift

Native iOS:

- SwiftUI
- AVFoundation
- CallKit
- Push notifications
- Background transfers
- Camera
- Media

---

# 59. Python

Use primarily for:

```text
RECOMMENDATION
ML
EMBEDDINGS
MODERATION
OCR
SPEECH
ANALYTICS
MODEL PIPELINES
```

Do not make Python the primary high-scale realtime messaging data plane without a clear performance reason.

---

# 60. Media Architecture

```text
CLIENT
  |
UPLOAD
  |
MEDIA GATEWAY
  |
VALIDATION
  |
OBJECT STORAGE
  |
MEDIA EVENT
  |
TRANSCODING QUEUE
  |
TRANSCODING WORKERS
  |
+-- HLS
+-- DASH
+-- Multiple resolutions
+-- Thumbnails
+-- Preview
+-- Captions
  |
CDN
```

---

# 61. Video Processing Requirements

Generate:

```text
240p
360p
480p
720p
1080p
Higher tiers where appropriate
```

Adaptive bitrate streaming should be supported for appropriate video classes.

Also:

- Thumbnail generation
- Preview generation
- Metadata extraction
- Audio extraction
- Caption pipeline

---

# 62. Resumable Uploads

Large videos require:

```text
VIDEO
 |
CHUNK 1
CHUNK 2
CHUNK 3
 |
UPLOAD STATE
 |
RESUME
```

Requirements:

- Resume after network failure
- Retry failed chunks
- Integrity verification
- Idempotency

---

# 63. Event-Driven Architecture

Example:

```text
VideoViewed
     |
     +-- Analytics
     +-- Recommendation
     +-- Creator Analytics
     +-- Trending
```

Avoid excessive synchronous coupling.

Core events:

```text
UserCreated
PostCreated
PostViewed
VideoCreated
VideoViewed
VideoCompleted
MessageSent
CallStarted
PaymentCompleted
OrderCreated
```

---

# 64. Recommendation Data Pipeline

```text
CLIENT EVENTS
      |
EVENT STREAM
      |
PROCESSING
      |
FEATURE STORE
      |
CANDIDATE GENERATION
      |
RANKING
      |
FEED
```

Privacy controls must limit collection and use according to product policy and applicable law.

---

# 65. Database Architecture

Use the right storage for the workload.

## PostgreSQL

Good for:

- Users
- Relationships
- Transactions
- Orders
- Communities
- Permissions

## Redis

Good for:

- Cache
- Presence
- Rate limiting
- Ephemeral state

## Object storage

Good for:

- Images
- Videos
- Audio
- Files

## Search engine

Good for:

- Full-text search
- Discovery

## Vector index

Good for:

- Semantic search
- Similarity

---

# 66. Security

Mandatory:

```text
PASSKEYS
2FA
TOTP
RECOVERY CODES
DEVICE MANAGEMENT
SESSION MANAGEMENT
RATE LIMITING
ABUSE DETECTION
AUDIT LOGGING
```

---

# 67. Device Management

```text
DEVICE
|
+-- Name
+-- Platform
+-- Last active
+-- Session
+-- Revoke
```

Users must be able to revoke sessions.

---

# 68. End-to-End Encryption

For protected conversations:

- Use established protocols
- Forward secrecy
- Device verification
- Key rotation
- Secure group messaging

Never advertise security properties that have not been technically verified.

---

# 69. Content Moderation

Architecture:

```text
CONTENT
 |
AUTOMATED ANALYSIS
 |
RISK SCORE
 |
POLICY ENGINE
 |
+-- ALLOW
+-- LIMIT
+-- REVIEW
+-- REMOVE
```

For significant enforcement actions:

- Human review where appropriate
- Appeals
- Audit trail
- Policy explanation

---

# 70. Copyright and Content Rights

Video systems should include:

- Ownership metadata
- Original creator attribution
- Remix permissions
- Audio rights handling
- Reporting workflow
- Takedown process
- Appeals

Do not copy copyrighted media without authorization.

---

# 71. Observability

Three pillars:

```text
LOGS
METRICS
TRACES
```

Monitor:

- API latency
- Error rate
- Message delivery
- Upload failures
- Transcoding
- Playback
- Call quality
- Database health

---

# 72. Video QoE Monitoring

Track:

```text
STARTUP_TIME
BUFFERING
BUFFER_DURATION
PLAYBACK_FAILURE
RESOLUTION
BITRATE
COMPLETION
```

---

# 73. Call Quality Monitoring

Track:

```text
PACKET_LOSS
JITTER
RTT
BITRATE
FRAME_RATE
RESOLUTION
```

---

# 74. Feature Flags

Required for safe rollout.

Examples:

```text
new_fyp
video_editor_v2
ai_dubbing
community_context
wallet_v2
```

Rollouts:

- Internal
- Beta
- Percentage
- Region
- Platform

---

# 75. Experimentation

Use controlled experiments for:

- Recommendation ranking
- Video UI
- Creator tools
- Notifications

Measure:

```text
SATISFACTION
RETENTION
WATCH_TIME
REPORTS
FAILURES
```

Do not optimize only engagement.

---

# 76. Offline-First Messaging

```text
LOCAL OUTBOX
     |
NETWORK
     |
SERVER
     |
ACK
```

Message states:

```text
PENDING
SENT
DELIVERED
READ
FAILED
```

---

# 77. Production Definition of Done

A feature is not complete when the UI exists.

Required:

```text
[ ] Backend implemented
[ ] Frontend implemented
[ ] Mobile implemented
[ ] Desktop evaluated
[ ] Authorization
[ ] Validation
[ ] Error handling
[ ] Loading states
[ ] Empty states
[ ] Offline behavior
[ ] Tests
[ ] Monitoring
[ ] Logging
[ ] Analytics
[ ] Security review
[ ] Documentation
```

---

# 78. Priority P0

Build first:

1. Unified identity
2. Authentication and device management
3. Social graph
4. Reliable messaging
5. Groups and communities
6. Posts and comments
7. Full Reels/video foundation
8. Media upload and transcoding
9. Feed architecture
10. Basic recommendation system
11. Notifications
12. Search
13. Cross-platform parity
14. Security and observability

---

# 79. Priority P1

1. Advanced FYP
2. Full video editor
3. Duet/stitch/remix
4. Long-form video
5. Creator Studio
6. Advanced analytics
7. Live streaming
8. Events
9. AI captions
10. Translation
11. Semantic search
12. Public Pulse system

---

# 80. Priority P2

1. AI agents
2. Mini Apps
3. Advanced commerce
4. Creator affiliate system
5. AI dubbing
6. AI video clipping
7. Advanced live shopping
8. Programmable developer ecosystem

---

# 81. Final Feature Matrix

| Domain | Facebook-Level | TikTok-Level | ChatApp Unique |
|---|---|---|---|
| Profiles | Yes | Yes | Multiple privacy modes |
| Friends/Follow | Yes | Yes | Unified relationship graph |
| Feed | Yes | Yes | Multi-feed architecture |
| Posts | Yes | Yes | Universal content |
| Reels | Yes | Yes | Full professional editor |
| Long video | Yes | Yes | AI clips and chapters |
| Stories | Yes | Yes | Community stories |
| Live | Yes | Yes | Universal Live Rooms |
| Communities | Yes | Partial | Community operating system |
| Messaging | Messenger | Limited | Telegram-class messaging |
| Public conversation | Limited | Limited | X-style Pulse |
| AI | Growing | Growing | Unified AI layer |
| Wallet | Partial | Partial | Integrated economy |
| Commerce | Yes | Yes | Content-to-commerce |
| Privacy | Partial | Partial | Public/private/ephemeral modes |
| Mini Apps | No | No | Programmable platform |

---

# 82. Final Architecture Decision

Recommended language distribution:

```text
Next.js + TypeScript
    |
    Web Platform

Kotlin
    |
    Android

Swift
    |
    iOS

Go
    |
    Backend APIs
    Business Services
    Distributed Orchestration

Rust
    |
    Crypto
    Security
    Wallet Core
    Shared Protocol Core

C++
    |
    Specialized Media
    High Performance Processing

Python
    |
    ML
    Recommendation
    AI Pipelines
    Data Science
```

---

# 83. Final Product Goal

ChatApp should become:

```text
FACEBOOK
Social Graph
Groups
Pages
Events
Marketplace

+

TIKTOK
For You
Reels
Video Creation
Discovery
Creator Growth

+

TELEGRAM
Messaging
Channels
Bots
Mini Apps

+

X
Real-Time Conversation
Trends
Topics
Live Audio

+

CHATAPP
AI
Privacy
Unified Identity
Communities
Wallet
P2P
Conversion
Staking
Crypto Card
Lucky Draw
Commerce
Cross-Platform Experience
```

# Final Principle

Do not build the largest number of features.

Build the **best integrated feature system**.

Every feature should connect:

```text
IDENTITY
   |
SOCIAL
   |
CONTENT
   |
DISCOVERY
   |
CONVERSATION
   |
COMMUNITY
   |
CREATOR
   |
ECONOMY
```

That integration is what can make ChatApp more powerful than a simple combination of existing platforms.

## Implementation audit addendum — 2026-09-13

This specification was reconciled with the executable repository on 2026-09-13. The repository now commits `apps/web/package-lock.json` and `apps/admin/package-lock.json`, so the documented CI `npm ci` builds are reproducible. CI also provisions Go and Rust and runs `go test`/`go vet` for `services/api`, `services/mesh`, and `services/sfu`, plus `cargo test --locked` for `services/authn` and `services/security`.

The authentication implementation enforces the five-failure, 48-hour account lockout with an atomic PostgreSQL counter update, preventing concurrent failed requests from overwriting one another. Successful password authentication still clears the counter and lockout.

A route, migration, client screen, or local unit test demonstrates an implementation path; it does not prove provider-backed delivery, configured-database behavior, production deployment, native-device Bluetooth/Wi-Fi Direct parity, or Android/iOS release builds. Those remain environment-dependent validation gates and must not be described as production-complete without the corresponding runtime evidence.

## Implementation audit addendum — 2026-09-13, second pass

The second source audit found and fixed authentication lifecycle gaps. Refresh-token rotation now validates and revokes a token in one conditional `UPDATE ... RETURNING` statement; password-reset token consumption now occurs in the same transaction as the password update and session revocation; and a successful password reset clears stale login-lockout counters. Tokens can therefore be accepted only once under concurrent requests, and a verified recovery flow restores account access. The remaining runtime and native-platform limitations stated above still apply.

The fourth audit additionally implemented the Identity requirement for conditional 2FA during password recovery. The reset API checks the account's TOTP secret before consuming the reset token, returns `totp_required` when appropriate, preserves the token on an invalid code, and the web reset page presents the authenticator-code prompt.

The fifth audit completed the authenticator-loss branch: a valid one-time recovery code is accepted as the reset second factor when TOTP is unavailable, consumed atomically, and supported by the web reset form.
