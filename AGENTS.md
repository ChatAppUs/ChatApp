# ChatApp — Agent Memory

## Repo facts
- Repo: github.com/ChatAppUs/ChatApp — push directly to `main` (explicit user requirement).
- Monorepo: `services/api` (Go core API), `services/security` (Rust), `services/media-edge` (C++),
  `services/ml` (Python/FastAPI), `apps/web` (Next.js 14), `apps/android` (Kotlin+Java/Compose),
  `apps/ios` (SwiftUI, XcodeGen `project.yml`), `apps/desktop` (Tauri 2/Rust).
- DB migrations: `infra/db/001_schema.sql`, `infra/db/002_features.sql` (apply in order).

## API contracts worth remembering
- Poll creation: `poll_options: [...]` array inside POST /api/posts; votes use `option_id` (upsert = changeable vote).
- Message reactions serialize as `map[emoji]count` in message listing.
- E2E key publish field is `identity_key` (base64 SPKI).
- Most POST endpoints return 201, not 200.
- TOTP 2FA: login returns 401 `totp_required` when enabled; client retries with `totp_code`.

## Test environment
- Local Postgres on :5432; use `sudo -u postgres psql -d chatapp < file.sql` (postgres user can't read workspace paths).
- No docker daemon in sandbox. Python has `websockets` but not `requests` (use urllib).
- Integration suite: `tests/integration_test.py` — 45 end-to-end checks, run with the API live on :8080.
- Go unit tests: `cd services/api && go test ./...`.

## Tooling quirks
- Terminal tool rejects heredoc combined with a second command; chain with `&&` only.
- `file_editor` create fails if parent dirs don't exist — `mkdir -p` first.
