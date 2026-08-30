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
