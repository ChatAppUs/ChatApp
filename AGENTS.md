# ChatApp — Agent Memory

## Repo facts
- Repo: github.com/ChatAppUs/ChatApp — push directly to `main` (explicit user requirement).
- Monorepo: `services/api` (Go core API), `services/security` (Rust), `services/media` (C++),
  `services/realtime` (C++ epoll WS fanout, :8300 WS / :8301 control), `services/transcode`
  (C++ ffmpeg HLS worker), `services/ml` (Python/FastAPI), `services/sfu` (Go/Pion),
  `apps/web` (Next.js 14), `apps/admin` (Next.js, port 3100), `apps/android`
  (Kotlin/Compose), `apps/ios` (SwiftUI, XcodeGen `project.yml`), `apps/desktop`
  (Tauri 2/Rust), `apps/extension` (Chrome/Firefox MV3).
- DB migrations: `infra/db/001..014_*.sql` (apply in order). SFU: `services/sfu` (Go/Pion, self-built, embedded STUN/TURN). Admin plane: standalone `apps/admin` (port 3100), admin JWTs rejected on user routes.
- Light/dark theme exists on EVERY client: web + admin share `localStorage["chatapp.theme"]`
  + `html[data-theme]`; desktop inherits web; extension uses chrome.storage key `theme`;
  Android persists `Session.darkTheme` (SharedPreferences) driving `ChatAppTheme`;
  iOS persists `@AppStorage("chatapp.theme")` driving `.preferredColorScheme`.

## API contracts worth remembering
- Poll creation: `poll_options: [...]` array inside POST /api/posts; votes use `option_id` (upsert = changeable vote).
- Message reactions serialize as `map[emoji]count` in message listing.
- E2E key publish field is `identity_key` (base64 SPKI).
- Most POST endpoints return 201, not 200.
- TOTP 2FA: login returns 401 `totp_required` when enabled; client retries with `totp_code`.

## Test environment
- Local Postgres on :5432; use `sudo -u postgres psql -d chatapp < file.sql` (postgres user can't read workspace paths).
- No docker daemon in sandbox. Python has `websockets` but not `requests` (use urllib).
- Integration suites: `tests/integration_test.py` — 153 end-to-end checks, and
  `tests/feature_test.py` — 72 checks (watch signals, transcode, groups, pages,
  monetization, bots, push, contacts, 2FA). Run with API on :8080
  (`CLUSTER_SECRET=test-cluster-secret`, `SFU_SECRET`/`TURN_SECRET=test-*-secret`),
  SFU on :8095, realtime relay on :8300/8301 with matching secrets.
  NOTE: re-running suites back-to-back trips the 10/min register rate limit —
  expect spurious 429/401 cascades; wait a minute between runs.
- Go unit tests: `cd services/api && go test ./...`.

## Security config (2026-08 audit)
- `APP_ENV=production` requires `JWT_SECRET` ≥32 bytes (Go API) and `SIGNING_SECRET` ≥32 chars (Rust security svc); both refuse to boot otherwise.
- `ALLOWED_ORIGINS` (comma-separated) drives CORS + WebSocket Origin allowlist; empty = dev wildcard.
- Rate limits (per-IP token bucket): login 15/min, register 10/min, password reset 5/min, SMS send 5/min, passkey/OAuth 60/min.
- Media uploads require a signed grant: `POST /api/media/upload-token` (Go → Rust `/sign`), media edge verifies when `SECURITY_SERVICE_URL` set. Downloads = unguessable 128-bit CSPRNG IDs, no auth.
- Analysis docs: `docs/SECURITY_AUDIT.md`, `docs/RUST_CONVERSION_PLAN.md`, `docs/CPP_CONVERSION_PLAN.md`, `docs/GAP_ANALYSIS.md`.

## Tooling quirks
- Terminal tool rejects heredoc combined with a second command; chain with `&&` only.
- `file_editor` create fails if parent dirs don't exist — `mkdir -p` first.
## Contracts discovered while testing (2026-08-30)
- Message requests: a DM conversation alone creates NO message_request. The request row is
  created when the first MESSAGE is sent (WS) and the recipient neither follows nor is
  followed by the sender. Recipient accepts via POST /api/me/message-requests/{convId}/accept.
- FYP (/api/fyp) only returns posts that HAVE watch signals (completion/rewatch > 0 ranked
  first, then not-interested/down-weighted). posts = [] with no signals is correct.
- post_media ordering column is `position` (not sort_order).
- Direct conversation validation counts the OTHER member (1) plus creator.
- integration_test.py shells out to psql as postgres://chatapp:chatapp@... — the chatapp
  role must exist (CREATE USER chatapp PASSWORD 'chatapp' SUPERUSER).
- SFU occupies :8095 (WS signaling) + udp/3478 (STUN/TURN); only one instance can run.
- Desktop app (apps/desktop) is a Tauri shell loading the web app — feature parity is
  inherited from apps/web; no per-feature desktop code needed.

## Contracts discovered while testing (2026-08-30, session 2)
- Extension (`apps/extension`, MV3): popup badge reads `GET /api/me` (username) +
  `GET /api/notifications` (`{notifications: [...]}` with nullable `read_at`) using a
  token pasted in Options; full-page view iframes the web app (parity inherited).
- Realtime relay: Go API publishes fanout via `POST {REALTIME_RELAY_URL}/publish`
  (Authorization: Bearer CLUSTER_SECRET, `{"user_ids":[...],"event":{...}}`); clients
  connect `ws://host:8300/ws?token=<access JWT>`. WS push works with or without the
  relay (Go hub fallback when REALTIME_RELAY_URL unset).
- Transcode pipeline: `POST /api/media/{id}/transcode` enqueues; worker polls
  `POST /internal/transcode/claim` (Bearer CLUSTER_SECRET) and reports to
  `/internal/transcode/complete`; done jobs rewrite `post_media.url` to the HLS master.
- docker-compose now wires api → security, api → realtime (REALTIME_RELAY_URL),
  transcode → api (shared mediauploads volume), and parameterizes all secrets via .env.

## Contracts discovered while testing (2026-08-30, session 3 — finance plane)
- Finance plane: `infra/db/015_finance.sql` adds feature flags to
  platform_tokens, convert_rates, withdrawal_requests, p2p_offers/p2p_trades,
  local_payment_methods (881 rows, one row per country × its rails),
  admin_role_defs (dynamic roles: superadmin can create/delete; permissions
  include p2p.resolve, convert.manage, withdrawals.review, tokens.manage).
- Deposit addresses are deterministic: HKDF-SHA256(seed=WALLET_MASTER_SEED
  (env, default `dev-wallet-seed-change-me`), info="addr|chain|uid") →
  per-chain encoding (bech32 SegWit, base58check legacy/Tron, keccak EVM,
  base58 Solana). No private keys are stored — deposit address = ID only.
- Withdrawal signature: HMAC-SHA256(signing key = HMAC(seed, "withdraw-signing"),
  message "withdraw|id|uid|asset|chain|to|amount|fee"). Auto-approve when
  risk_score < WITHDRAW_AUTO_THRESHOLD (default 100); the score comes from
  account age, new address, velocity, and USD value vs
  WITHDRAW_AUTO_LIMIT_USD (default 10000). Approved+signed →
  executeWithdrawal → `signed` status (broadcast hot-wallet hook left at
  execution point). Reject refund is a compensating `withdrawal_refund`
  ledger entry.
- Withdrawal requests debit-hold amount+fee immediately (`withdrawal_hold`);
  balances = SUM(ledger_entries.amount) per account (no balance column).
- POST /api/wallet/withdraw requires kyc_status='verified' and per-chain
  address format validation; P2P trade open locks seller crypto via
  `p2p_escrow_lock`; release/refund are idempotent via locked-status checks.
- Admin endpoints: /api/admin/withdrawals(?status=), /{id}/review
  (decision=approve|reject), /api/admin/wallet/tokens (POST add, DELETE remove,
  /{id}/features toggles), /api/admin/convert/rates (POST upsert),
  /api/admin/p2p/disputes + /api/admin/p2p/trades/{id}/resolve (to=buyer|seller),
  /api/admin/role-defs (GET/POST/DELETE).
- tests/finance_test.py = 44 checks; run AFTER integration/features suites with
  ≥60s gaps (register rate limit). It bootstraps funds via psql
  (ledger_entries insert) and grants superadmin via grant_superadmin().
- Frontend parity: web /wallet /convert /p2p (+QRScanModal/QRDisplay
  components), admin FinanceTabs.tsx (tokens/withdrawals/roles/rates/disputes),
  Android ui/WalletScreen.kt (ZXing QR + ML Kit scan), iOS Views/WalletView.swift
  (CoreImage QR + AVFoundation scan). Desktop/extension inherit web.

## Contracts discovered while testing (2026-08-30, session 4 — gap closure)
- Migration 016_merchants_cards.sql: p2p_merchants + p2p_merchant_tiers (3 levels),
  virtual cards (Luhn PAN shown ONCE at issue, only last4 stored), card_transactions,
  post_reactions (6 types; like endpoint = alias), post_tags, post_edits, albums +
  album_items, chat_nicknames, conversations.theme, posts.feeling/location/publish_at,
  users.pinned_post_id, event reminder notifications. Numeric limits serialized with
  trim_scale() to avoid exponent notation.
- Merchant flow: POST /api/p2p/merchant/apply → admin review (approve+tier/reject) at
  /api/admin/p2p/merchants*; badge on offers via owner_is_merchant/owner_merchant_tier;
  tier caps enforced at trade open (per-trade + daily volume).
- Cards: /api/cards (issue/list), /charge, /topup (crypto→USD at admin rate), /refund,
  /status (POST), /limits (PUT), /{id}/transactions. Admin: /api/admin/cards + status.
  handleCardCharge decline row MUST insert in the same tx as the FOR UPDATE card lock
  (second pool conn = self-deadlock, 300s hang).
- Admin transfer oversight: GET /api/admin/transfers, POST .../reverse (compensating
  double-entry). User transfers already at /api/wallet/transfer.
- Social: PUT/DELETE /api/posts/{id}/react, GET /reactions, PUT/DELETE
  /api/me/pinned-post, GET /api/posts/{id}/edits, GET /api/me/scheduled-posts,
  DELETE /api/scheduled-posts/{id}, comment sort ?sort=top|newest|oldest.
- Chat: PUT /api/conversations/{id}/theme (1-40 chars a-z0-9_-), PUT .../nicknames/{uid}.
  Web presets: sunset/ocean/forest/candy/mono gradients in chat page.
- Web routes: /cards, /albums; profile shows pinned post first + albums strip +
  scheduled posts (own profile); Composer has Extras (feeling/location/tags/schedule).
  Admin dashboard tabs: merchants, cards, transfers.
- tests/gaps_test.py = 92 checks. Full sweep 2026-08-30: gaps 92/92, integration
  153/153 (needs SFU on :8095 else 5 media-ticket fails), features 72/72, finance 44/44.
  features_test.py exits 0 with no summary line. integration_test.py needs python
  'cryptography' pkg for the passkey flow.

## Contracts discovered while testing (2026-08-30, session 4b — native parity)
- Card lifecycle statuses are `active | frozen | terminated` (NOT "closed") —
  all clients must use "terminated". Card issue response: `card_number` + `cvv`
  (shown once). Top-up request: `{asset, chain, amount}` (crypto amount, server
  converts to USD at platform rate). Merchant status: `{merchant|null,
  max_trade_usd, daily_volume_usd}` — limits are TOP-LEVEL, not inside merchant.
- P2P offer JSON includes `owner_is_merchant` + `owner_merchant_tier` (badge).
- Post JSON includes `my_reaction`, `feeling`, `location`, `edited_at`,
  `publish_at`. Reactions: PUT/DELETE /api/posts/{id}/react {reaction:
  like|love|haha|wow|sad|angry}. Pin: PUT /api/me/pinned-post {post_id}.
- Chat theme: PUT /api/conversations/{id}/theme {theme: ""|sunset|ocean|forest|candy};
  conversation JSON includes `theme`. Nicknames: PUT /api/conversations/{id}/nicknames/{userId}.
- Native parity (Android WalletScreen/FeedScreen/ChatScreen, iOS WalletView/
  FeedView/ChatListView) shipped for all of the above; sandbox has no JVM/Swift
  toolchain so native builds are not compiled here — verify in CI/Android Studio/Xcode.

## Contracts discovered while testing (2026-08-30, session 5 — gap pack 2)
- Migration 017_gap_pack2.sql: user_profiles (multiple), blocked_media_hashes,
  media_verdicts, sanctions_entries + sanctions_hits, trusted_contacts (max 4)
  + account_recoveries, users.legacy_contact_id/memorialized, posts.remix_of,
  reel_watch aggregates, call_screen_shares + call_recordings, live_locations,
  chat_polls + chat_poll_votes, posts.story_background/story_stickers/story_music
  (jsonb), platform_tokens.rpc_url.
- postSelect story_stickers/story_music are jsonb columns: SELECT with ::text
  and unmarshal in postOut, else feed scan 500s.
- tests/gaps2_test.py covers: multiple profiles (/api/me/profiles + /activate),
  live location start/stop/view (haversine fallback without PostGIS), chat polls,
  video notes (uploaded media_id), own media moderation (blocked sha256/dhash;
  ML svc POST /moderate/media), chain watcher (own-node deposit credits from
  platform_tokens.rpc_url), trusted-contacts recovery (TOTP-gated claim),
  legacy contact + memorialize, reel remix + analytics, call screenshare +
  recordings, story composer fields, sanctions screening (admin CSV import of
  OFAC/EU/UN lists, trigram name match), P2P-order-book-derived convert rates.
- i18n: new UI strings go in the extras block of apps/web/src/lib/i18n.tsx
  (all 8 locales); t() falls back to en.
- Community notes API (all clients): GET /api/posts/{id}/notes, POST note
  {body}, POST /api/notes/{id}/vote {helpful}, DELETE /api/notes/{id}.
  Wired in web CommunityNotes.tsx (PostCard + reels), Android FeedScreen,
  iOS FeedView.
- Call recording: MediaRecorder webm via POST /api/media/upload-token grant,
  then POST /api/calls/rooms/{id}/recordings {media_id, duration_s}; list +
  delete on the same path. Screenshare is client getDisplayMedia + signaling
  via /api/calls/rooms/{id}/screenshare.

## Contracts discovered while testing (2026-08-31, session 6 — gap pack 3)
- Migration 018_gap_pack3.sql: users.last_seen_privacy/phone_privacy/
  data_saver/safety_mode/account_ttl_days, conversations.handle (unique
  public @handle), conversation_members.perms (granular admin flags),
  sticker_packs + stickers, chat_folders + chat_folder_conversations,
  lists + list_members, bookmark_folders + bookmarks.folder_id,
  profile_views, playlists + playlist_items, verification_requests,
  posts.reply_policy/content_warning/sensitive/pinned_comment_id,
  post_media.alt_text, comments.hidden_at.
- Reply policy enforced in handleAddComment via checkReplyPolicy (403 on
  violation); author is always allowed; "mentioned" checks post_tags +
  @mention of the author's username in the comment body.
- Hidden replies (comments.hidden_at): invisible to everyone except the
  comment author and the post author; hide/unhide = POST /api/comments/{id}/
  hide|unhide (post author or comment author only).
- Public handles: PUT /api/conversations/{id}/handle (owner only,
  3-32 [a-z0-9_]), GET /api/handles/{handle} preview, POST .../join.
  Member roles: PUT /api/conversations/{id}/members/{uid}/role (owner only).
- startAccountTTLWorker (main.go) flips status='deleted' after inactivity
  past account_ttl_days. Sessions: GET/DELETE /api/me/sessions{,/{id}}.
- tests/gaps3_test.py = 82 checks. Full sweep 2026-08-31: gaps3 82/82,
  integration 153/153, gaps 92/92, gaps2 70/70, features 72/72, finance
  44/44 (SFU on :8095, fresh Postgres 17 in sandbox, Go 1.23 toolchain
  under ~/tools/go, binaries built to /tmp).

## Contracts discovered while testing (2026-08-31, session 6 — gap pack 4)
- Migration 019_gap_pack4.sql: post_drafts, interest_topics + topic_follows +
  interest_vector, organizations + organization_members (+users.affiliated_org_id),
  audio_rooms + audio_room_participants (host|cohost|speaker|listener, hand_raised),
  premium_plans (2 seeded) + premium_subscriptions (+users.is_premium),
  gif_catalog, messages.kind/entities (jsonb), conversations.discussion_group_id/
  anonymous_admin/category, sounds, share_ledger (+posts.share_count/price_usd/
  content_rating), content_purchases, marketplace_listings, fundraisers,
  users.restricted_mode/discoverable/xp, family_links, bot_invoices, live_gifts
  (room-scoped, reuses 012 gift_catalog), brand_deals. Platform treasury user
  = uuid all-zeros (username 'platform', status 'suspended') — premium revenue
  accrues there; required because wallet_accounts.user_id FK references users.
- pgx gotcha: `WHERE uuid_col = $1` with a string param errors (uuid=text);
  use `col::text=$1` when the same param also matches a text column.
- requireAdmin("superadmin") middleware; admin endpoints need POST
  /api/admin/login {identifier(email), password} → admin-scoped token.
- XP/levels: awardXP on WS message (+1) & post (+5); level=floor(sqrt(xp/100))+1.
- Live gifts: POST /api/live/{room}/gifts {gift_id,to_user} debits wallet via
  moveUSD; leaderboard = GET /api/live/{room}/leaderboard.
- Bot payments: POST /api/bot/{token}/createInvoice {user_id,title,amount_usd}
  (bot token auth), pay via POST /api/bots/invoices/{id}/pay (user JWT, 409 on
  double pay). Inline: GET /api/bots/inline?bot=<username>&q=.
- Message entities: WS send accepts `entities` ([{type,offset,length,url?}]),
  sanitized server-side (bold/italic/mono/spoiler/link only, bounds-checked);
  messages.entities jsonb returned in list. Contact cards: kind='contact'
  with entities {user_id,username}; GIF: kind='gif'.
- handlePurchasePost: check content_purchases BEFORE moveUSD (409 already
  purchased) — moveUSD first would double-charge.
- tests/gaps4_test.py = 96 checks (needs migration 019 + SFU not required).
  Full sweep 2026-08-31 (session 6): gaps4 96/96, integration 153/153,
  features 72/72, finance 44/44, gaps 92/92, gaps2 70/70.

## Contracts discovered while testing (2026-08-31, session 7 — gap pack 5)
- Migration 020_gap_pack5.sql: drop_in_rooms (slug 128-bit CSPRNG, link
  /room/<slug>, ended_at), kyc_submissions.auto_score/auto_checks columns,
  ad_campaigns + ad_creatives (review→active→fund lifecycle).
- Drop-in rooms: POST /api/rooms {title} → {slug, link}; GET /api/rooms/{slug}
  (410 when ended); POST .../join registers SFU room `dropin-<slug>` and
  returns a full SfuSession ({room_id, mode:"meeting", role:"publisher",
  ticket, sfu_url, ice_servers}); POST .../end is host-only.
- KYC auto-verify: ML svc POST /kyc/verify {doc_image_url, selfie_url,
  doc_type, doc_number, full_name} → {score, checks} (checks = flat dict of
  bool/str/dict). handleKYCSubmit (handlers_wallet.go) scores SYNCHRONOUSLY
  in the submit tx: score ≥0.75 AND sanctions-clean (screenName hits==0)
  auto-verifies; ML unreachable fails open to manual review. Admin KYC queue
  rows include auto_score/auto_checks; review decision value is "verified"
  (not "approve").
- Ads: POST /api/ads/campaigns, /{id}/creatives, /{id}/submit, admin
  /api/admin/ads/review {decision}, /{id}/activate, /{id}/fund (moveUSD into
  platform treasury via moveUSDToTreasury — wallet tx rows need BOTH a debit
  and credit row, treasury user 00000000-…). POST /api/ads/serve
  {country, placement_post_id?} → {ad: {...}, share_usd}; target_countries
  must match exactly or be empty ("ALL" does NOT match); unattributed serves
  record spend, attributed ones also ledger 55% rev-share to the creator.
- Redis cache: services/api/cache.go — *cache wraps go-redis, newCache
  returns nil when REDIS_URL unset/invalid (caching simply off, no in-process
  stand-in); all calls fail open with 300ms ctx timeout. Only /api/fyp is
  cached: key `fyp:<uid>:<limit>:<offset>`, 15s TTL, X-Cache: hit header.
- Web VideoFilter (lib/webrtc.ts): canvas ctx.filter presets (warm/cool/bw/
  vivid/soft) publish the filtered outgoing track via replaceVideoTrack —
  SfuCall now has replaceVideoTrack too. Room page: /room/[slug].
- Web chat page has NO error state — don't call setError there.
- Android ChatScreen: material3.TextButton must be imported explicitly.
- tests/gaps5_test.py = 39 checks. Full sweep 2026-08-31 (session 7):
  integration 153/153, features 72/72, finance 44/44, gaps 92/92,
  gaps2 70/70, gaps3 82/82, gaps4 96/96, gaps5 39/39, go test OK,
  web next build OK, admin tsc OK.

## Contracts discovered while testing (2026-08-31, session 8 — gap pack 6)
- Migration 021_gap_pack6.sql: posts.article (jsonb {title,subtitle,body<=100k,cover_url}),
  users.bio_links (jsonb, max 5, https-only via handleUpdateProfile), messages.waveform
  (jsonb peaks, client-computed, server-clamped 0..100), custom_emoji (admin-managed:
  POST/DELETE /api/admin/custom-emoji, GET /api/custom-emoji; reactions accept :shortcode:
  via customEmojiExists in WS react), message_translations (cache; built-in lexicon
  translateLocal -> 'local-dict-v1', cache hits tagged provider='cache'), live_rooms +
  live_room_viewers (discoverable live rooms: create/list/get/join/leave/like/end,
  one live room per host, peak_viewers tracked),
  visibility CHECK extended with 'list' (audience_list_id -> user_lists).
- GET /api/posts/{id} single-post permalink added (same visibility matrix as feed,
  blocks both directions); response is {"post": {...}}.
- Post edit window: PATCH /api/posts/{id} rejects after POST_EDIT_WINDOW_MINUTES
  (default 48h, 0 = unlimited). Tested by back-dating created_at in psql.
- Typing actions: WS typing accepts action typing|recording_voice|recording_video|
  uploading_photo|uploading_video|uploading_voice|uploading_document|choosing_sticker
  (invalid -> 400 echo {ok:false}).
- Safety mode auto-blocks stranger DM senders when: account <7d old OR 3+ reports —
  user_reports counts BOTH user-targeted AND post-targeted reports whose post author
  is the target (posts.author_id subquery).
  FYP); creator share = 25% impression ($0.005 CPM floor), 2% click ($0.05 floor) paid
  from treasury (all-zeros uuid) to context author, skipped when author == advertiser.
- Ads rev share: adopted the session-7 implementation (ad_events.placement_post_id,
  55% of impression cost from treasury, single-tx). Do NOT re-add context_post_id.
- Creator earnings: GET /api/creator/earnings counts qualified reel_watch_events only
  (completed OR rewatched) x CreatorRPM/1000. pgx GOTCHA: `COUNT(*) * $float / 1000.0`
  infers pgtype wrongly and silently returns 0 — must cast: `COUNT(*)::numeric * $2 / 1000.0`.
- FYP: viewer's reports (user/post) exclude reported reels (viewer-scoped), then one
  'explore' slot (explore=true flag) injected every ~10th position.
- tests/gaps6_test.py (ads block dropped; covered by gaps5). Full sweep 2026-08-31:
  integration 153/153 (needs `pip install cryptography` for passkey flow), gaps4 96/96,
  gaps3/gaps2/gaps/features/finance all green (SFU on :8095).
- Reaction route is POST /api/messages/{id}/reactions (NOT /react); profile update is
  PATCH /api/me (NOT PUT); post edit is PATCH /api/posts/{id}.

## Contracts discovered while testing (2026-08-31, session 9 — staking + live prices)
- Migration 022_staking.sql: staking_assets (APY + durations jsonb + min/max
  NUMERIC(38,18)), staking_rate_audits (every admin upsert logged with admin
  username + old/new APY), staking_positions (status active|unlock_requested|
  closed, APY frozen at open, simple-interest rewards), staking_treasury_moves
  (in/out of the platform treasury with purpose), token_prices (unique
  asset+chain: usd + source live-coingecko|override|orderbook + fetched_at),
  platform_tokens.coingecko_id. Platform treasury user = all-zeros uuid with
  wallet_accounts rows created on demand per (asset, chain).
- Simple interest reward = amount*apy*duration_days/365, computed with float64
  then decimal-quantized to 18dp in settlePosition. Rewards pay out from the
  treasury in the SAME tx as the position close. handleStakingUnlock: mature
  → settle in one tx; early → 202 + status unlock_requested (admin
  POST /api/admin/staking/settle {position_id} credits and closes). Admin
  scope: superadmin only for assets/rates/settle/treasury; audit GET is
  'staking.manage' permission.
- pgx scan trap: SELECT must project the same column count as rows.Scan —
  stakePosSelect lists 13 fields including p.closed_at. Use $N::uuid casts
  when the same param is also compared to text columns.
- pricefeed.go: startPriceFeed worker (ticker COINGECKO_INTERVAL_S, default
  300s; ≤20 ids/call to respect free-tier rate limits; USD cached in Redis
  60s under key price:<asset>:<chain>). priceUSD(ctx, a, asset, chain, include)
  order: token_prices override → Redis cache → P2P order book mid → CoinGecko
  live (fetch+cache+upsert with 10s ctx). COINGECKO_IDS overrides the
  symbol→id map (CSV). Admin override: PUT /api/admin/prices/{asset}/{chain}
  {usd:""} clears override; {usd:"<num>"} sets source=override.
- Staking routes: GET /api/staking/assets (enriched with price_usd),
  GET/POST /api/staking/positions{,/{id}/unlock}; admin under
  /api/admin/staking/* + /api/admin/prices*.
- frontend parity: web /staking + wallet PricesTable, admin dashboard tabs
  staking + prices (FinanceTabs.tsx), Android ui/StakingScreen.kt (new menu
  item) + WalletScreen prices tab, iOS Views/StakingView.swift + WalletView
  Prices tab. i18n keys added for all 8 locales in lib/i18n.tsx extras block.
- test environment note: gaps5_test.py needs `pip install pillow` + the ML
  service on :8200 (`uvicorn main:app --port 8200`) or the KYC auto-verify
  checks fail-open to manual review. All ten suites green: integration 153,
  features 72, finance 44, gaps 92, gaps2 70, gaps3 82, gaps4 96, gaps5 39,
  gaps6 91, staking 48.
## Contracts discovered while testing (2026-08-31, session 9b — gap pack 7)
- Migration 023_gap_pack7.sql (renamed: remote main took 022 for staking):
  upload_sessions (chunked resumable uploads),
  chat_polls.is_quiz + correct_option_id + explanation (quizzes), moments
  (publish/re-publish/unpublish/admin review), audio_room_recordings
  (host-only), e2e_keys + sas_verifications, related-reel embeddings
  (posts.embedding 256-dim jsonb), word_filters (creator-side comment auto-
  hide), comments.hidden_at usage + GET /api/u/{username}.
- FYP pipeline order is rerank → diversify → inject exploration slots (the
  deterministic slots survive only in this order; previously diversify moved
  the explore card). Explore candidate query must exclude
  already-seen ids (p.id <> ALL($seenIDs)) or empty for watchers-with-history.
- diversified2 reranker: spread = max 2 consecutive per author, remix-chain
  dedup on remix_of. Related reels: cosine rerank over embeddings via ML
  svc POST /embed (256-dim), similarity threshold clamps out unrelated;
  poll for async embedding indexing in tests (retry helpers).
- Chat quiz polls: votes reply {poll_results} includes correct_option_id
  only when is_quiz AND my vote exists; quizzes can't run without
  correct_option (400). Bots: /api/bot/{token}/getMe {ok:true,result:{id,
  username}}; getChat {id,title,type,member_count}; editMessageText idempotent.
- Upload sessions: POST /api/uploads {filename,size,mime} → signed chunk
  grants via Rust /sign (SECURITY_SERVICE_URL); completion requires exact
  byte count (400), abort deletes; C++ edge also rejects non-/uploads-URL
  uploads and URL-injected byte mismatches.
- E2E keys: POST /api/me/e2e/keys {identity_key base64}; SAS: GET
  /api/conversations/{id}/sas {fingerprint, phrase}; fingerprint is
  symmetric under user ordering AND stable across re-publishes; self-
  verify is rejected (400).
- Web multi-account: archive tokens to localStorage chatapp.accounts on
  saveTokens(tokens,username); switchAccount swaps active keys; Nav renders
  a switcher when >1 archived. Reels speed chip cycles 0.5/1/1.5/2x;
  prefetch = <link rel=preload as=video> for next 3 video urls.
- tests/gaps7_test.py = 92 checks. Full sweep 2026-08-31 (session 9):
  integration 153/153, features OK, finance 44/44, gaps, gaps2-gaps7 all
  green, go test OK,   web next build OK (SFU :8095, ML svc + Pillow for
  KYC image checks).

## Contracts discovered while testing (2026-08-31, session 10 — gap pack 8)
- Migration 024_gap_pack8.sql: profile_questions (profile Q&A),
  live-rooms ingest endpoints, screen_time_usage, app_lock flag, marketplace
  orders, feature-vector rollup, group scale probe tables/columns.
- Compositor queue (TikTok duet/stitch + trim/mix): POST
  /api/reels/{id}/duet|stitch and /api/media/{id}/trim|/mix enqueue kind into
  transcode_jobs via enqueueCompositorJob; C++ worker draws 720p HLS +
  thumb + master playlist, ladder flagged master:true. handleDuet/Stitch
  also creates the remix post (media_url clip, type=reel, remix_of).
- Live ingest: POST /api/live-rooms/{id}/ingest mints a 128-bit stream key
  and enqueues a `live` transcode job; worker runs ffmpeg `-listen 1` as
  the RTMP endpoint itself (RTMP_LISTEN_PORT default 1935). End → 404
  idempotent. GET .../stream returns {stream:{play_url,status,stream_id}}.
- Live co-hosts: action invite|accept|remove via speaker join check;
  host (owner|host role) only for invite/remove.
- Screen time: PUT limit (1..1440), POST /ping {minutes<=30} accumulates
  day row, returns exceeded; GET for Extras panel.
- App lock: PUT /api/me/app-lock {enabled}; POST /api/auth/verify-password
  {password} → {ok:true} (401 verifyPassword mismatch).
- Marketplace checkout: POST /api/marketplace/{id}/buy — wallet hold,
  seller notify, affiliate cut = 40% of 5% platform fee when purchased
  via_post_id of a reel. handleListOrders GET /api/me/orders?as=seller.
- FYP rollup: GET /api/me/feature-vector → {interest_vector, watch.events}.
- Group scale: GET /api/admin/groups/scale (admin): largest groups +
  fanout note (C++ realtime relay flood, falls back to Go hub shards).
- Admin login response 401 for non-admin user tokens on requireAdmin routes
  (NOT 403); GAP: GET /api/admin/groups/scale needs admin-scoped JWT.
- getUser/publicUser now carries pinned_post_id/kyc_status/is_premium._
  handlers_social/gap6/gap7 scans must keep column-count parity with their
  publicUser scan lists or SQL scans fail (gotcha from session 4).
- psql e2e compositor job gotcha: transcode_jobs.params.source_url is the
  string the C++ worker reads (not the row source_url column) when kind is
  a compositor kind; claim attempts <5 so avoid churning manual polls.
- tests/gaps8_test.py = 54 checks; verified: gaps8 54/54, go test OK,
  features exit 0, web next build OK, C++ worker build OK, full compositor
  e2e proven with ffmpeg (claim→ffmpeg→ladder→master→complete→
  post_media.url rewrite). ffmpeg installed via apt for the sandbox.



## Contracts discovered while testing (2026-08-31, session 10 — gap pack 8)
- Migration 024_gap_pack8.sql: posts.remix_mode (""|duet|stitch). createPost
  accepts remix_mode only with remix_of and type='reel'; postSelect/postOut/
  scanPosts expose remix_of + remix_mode; handleReelRemixes rows include
  remix_mode; FYP main + exploration projections carry both fields.
- FYP response shape is a CUSTOM projection (not postOut): id/username/body/
  media_url/like_count/watch metrics — native clients (Android FypScreen,
  iOS FypView) decode this shape, not the feed shape.
- Web reels: ReelExtras.RemixModal has a layout picker (Remix/Duet/Stitch);
  RemixPlayer renders duet (side-by-side muted source + response) and stitch
  (source once → response loops); reels/page.tsx routes layout remixes to
  RemixPlayer. React ref gotcha: prop type React.RefObject<HTMLVideoElement>
  (not | null) to satisfy <video ref>.
- Android: FypScreen RemixDialog (layout FilterChips + video picker via
  GetContent) and RemixVideo (two VideoViews for duet, completion-handover
  for stitch; source fetched via /api/posts/{id}); ApiClient.uploadMedia
  (signed grant → media edge POST, BuildConfig.MEDIA_BASE_URL :8100).
- iOS: FeatureViews FypView + RemixSheet (segmented layout Picker +
  PhotosPicker) + RemixPlayerView (HStack VideoPlayers for duet,
  AVQueuePlayer for stitch); APIClient.uploadMedia + mediaBaseURL
  (CHATAPP_MEDIA_BASE env/Info.plist, default :8100).
- Docs corrected: GAP_ANALYSIS rows 6/9/10, facebook.md Remix + Polls rows.
- Test env: ML svc (uvicorn :8200, needs Pillow) for gaps5/gaps7; no Rust
  toolchain in sandbox → security svc unsigned dev mode (SECURITY_SERVICE_URL
  unset); SFU /tmp/sfu on :8095.

## Gap-pack-8 merged with remote (2026-08-31, session 10)
- Remote gap-8 (255ba8d) = remix_mode (duet/stitch) client render; my pack = server
  compositor (C++ ffmpeg duet/stitch/trim/mix), RTMP live ingest, Q&A, app-lock,
  screen-time, marketplace checkout, feature-vector rollup, group scale probe.
  Migration 024 contains BOTH blocks; tests/gaps8_test.py merged 32/32 PASS.
- Web reels page merges both: RemixModal (layout) + RemixPlayer (client render)
  + DuetStitchModal (server compositor).
- Gotcha: sandbox had a stale /tmp/chatapp-api PID from an earlier session
  holding :8080 — always pkill by exact path AND check remaining PID holders.
