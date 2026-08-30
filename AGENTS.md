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
