# ChatApp — Agent Memory

## Repo facts
- Repo: github.com/ChatAppUs/ChatApp — push directly to `main` (explicit user requirement).
- Monorepo: `services/api` (Go core API), `services/security` (Rust), `services/media-edge` (C++),
  `services/ml` (Python/FastAPI), `apps/web` (Next.js 14), `apps/android` (Kotlin+Java/Compose),
  `apps/ios` (SwiftUI, XcodeGen `project.yml`), `apps/desktop` (Tauri 2/Rust).
- DB migrations: `infra/db/001..009_*.sql` (apply in order). SFU: `services/sfu` (Go/Pion, self-built, embedded STUN/TURN). Admin plane: standalone `apps/admin` (port 3100), admin JWTs rejected on user routes.

## API contracts worth remembering
- Poll creation: `poll_options: [...]` array inside POST /api/posts; votes use `option_id` (upsert = changeable vote).
- Message reactions serialize as `map[emoji]count` in message listing.
- E2E key publish field is `identity_key` (base64 SPKI).
- Most POST endpoints return 201, not 200.
- TOTP 2FA: login returns 401 `totp_required` when enabled; client retries with `totp_code`.

## Test environment
- Local Postgres on :5432; use `sudo -u postgres psql -d chatapp < file.sql` (postgres user can't read workspace paths).
- No docker daemon in sandbox. Python has `websockets` but not `requests` (use urllib).
- Integration suite: `tests/integration_test.py` — 153 end-to-end checks; run with API on :8080 (`CLUSTER_SECRET=test-cluster-secret`, `SFU_SECRET`/`TURN_SECRET=test-*-secret`) and the SFU on :8095 with matching secrets.
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
