# ChatApp — Full Backend Security & Safety Audit

Date: 2026-08-29. Scope: every backend file in the repository —
`services/api` (Go, 23 files), `services/security` (Rust, 2 files),
`services/media` (C++, 1 file), `services/ml` (Python, 1 file),
`infra/db` (8 migrations), plus client-side crypto in `apps/web/src/lib/e2e.ts`.

Legend: ✅ verified safe · 🔧 fixed in this audit · ⚠️ recommendation.

---

## 1. Executive summary

The backend was audited line-by-line for the OWASP API Top 10 plus
crypto-specific and financial-ledger risks. The core design was already
sound: parameterized SQL everywhere (zero injection surface), argon2id
password hashing, HMAC-SHA256 JWTs verified in constant time, refresh-token
rotation with hashed storage, RBAC on every admin route, KYC-gated wallet
with a double-entry ledger, and fail-closed cluster secrets.

This audit found and **fixed 8 real issues** (section 3), including one
broken build (missing embedded asset excluded by `.gitignore`), one
missing-authentication hole on the media upload endpoint, one silent
fail-open DNS bug in the C++ media edge, and missing rate limiting on all
authentication endpoints.

After fixes: **129/129 integration checks pass**, Go unit tests pass,
Rust and C++ services compile clean and the signed-upload flow was
verified end-to-end (signed upload accepted, unsigned and forged-signature
uploads rejected, forged download signature rejected).

## 2. What was verified safe

### Authentication & sessions (services/api/auth.go, handlers_auth.go)
- ✅ Passwords hashed with argon2id (m=64MiB, t=3, p=2) in PHC format;
  verification is constant-time (`subtle.ConstantTimeCompare`).
- ✅ JWT HS256: signature verified **before** claims parsing, algorithm is
  fixed in the header (no alg-confusion), expiry enforced, `typ` claim
  separates access vs refresh so refresh tokens can't be used as access
  tokens.
- ✅ Refresh tokens stored as SHA-256 hashes, rotated on every use, old
  session revoked atomically; logout revokes by hash.
- ✅ Password reset: 32-byte random token, stored hashed, 1h expiry,
  single-use, and **all sessions revoked** on reset; forgot-password never
  reveals account existence.
- ✅ Phone verification: self-built OTP engine (no external provider) —
  crypto/rand codes stored salted+hashed, attempt limits, resend throttling,
  single-use; in-app delivery in sandbox mode.
- ✅ TOTP 2FA: RFC 6238 with ±1 step drift, constant-time compare; pending
  secret can't be used until verified; login returns `totp_required` and
  re-prompts.
- ✅ Passkeys (WebAuthn): challenge stored server-side, single-use,
  origin/RP-ID allowlist, signature counter enforced; forged-signature
  rejection is covered by an integration test.
- ✅ Google OAuth: ID tokens verified locally against Google's JWKS with
  issuer + audience checks, 6h key cache with stale fallback.
- ✅ QR login: one-shot tokens, approve/reject from an authenticated
  session, re-approve rejected (tested).

### Authorization & admin separation
- ✅ **Completely separate admin plane**: admin sessions are minted only via
  `/api/admin/login` with an admin-scoped JWT audience that every user route
  rejects; the admin console is a standalone app (`apps/admin`, port 3100),
  fully removed from the user web app. Users can never reach admin
  functionality.
- ✅ Every `/api/admin/*` route is behind `requireRole(...)` which checks
  the `admin_roles` table per request; roles (superadmin, moderator,
  support, finance, ads_reviewer) are least-privilege per route.
- ✅ Platform tokens in the wallet are admin-managed (`platform_tokens`):
  only superadmin/finance can add/enable/disable assets.
- ✅ SFU access control: meeting/live rooms require HMAC-signed tickets
  minted by the API; TURN credentials are short-lived HMAC users — the SFU
  accepts nothing unsigned.
- ✅ First-admin bootstrap only fires when `admin_roles` is empty;
  afterwards only superadmins grant roles.
- ✅ Admin actions are written to `audit_log` (actor, action, target, meta).
- ✅ Object-level checks: post edit/delete author-only, story viewers
  author-only, conversation actions member-only, group admin ops
  owner/admin-only.

### Data layer
- ✅ 100% parameterized SQL via pgx — no string-built queries anywhere
  (verified by grepping every `Sprintf` in the service).
- ✅ JSON request bodies capped at 4 MiB (`http.MaxBytesReader`).
- ✅ Wallet: double-entry ledger in one transaction; sender row locked
  `FOR UPDATE`; balance check is atomic with the insert; P2P is KYC-gated;
  amounts parsed as `NUMERIC` (no float money); self-transfer rejected.
- ✅ Ads: campaign funding debits the same ledger; only `active` creatives
  served; clicks recorded per-creative.
- ✅ Cluster heartbeat endpoint fails closed when `CLUSTER_SECRET` is unset.

### Media edge (services/media/main.cpp)
- ✅ Path traversal blocked (`safeName` whitelist + `..` rejection).
- ✅ 2 GiB upload cap, incomplete uploads deleted.
- ✅ MIME types from a fixed whitelist (no content sniffing of user bytes
  as HTML — no stored-XSS via the media domain).
- ✅ HTTP Range streaming with bounds clamping.

### ML service (services/ml/main.py)
- ✅ Pydantic-validated inputs; no auth surface exposed beyond internal
  network assumption (bind to private interface in production).
- ✅ Moderation model loading is optional and offline-safe.

## 3. Findings fixed in this audit

| # | Severity | Finding | Fix |
|---|----------|---------|-----|
| 1 | **High** | `POST /upload` on the media edge had **no authentication** — anyone could push 2 GiB files | API now mints 5-min HMAC-signed upload grants via the Rust security service (`POST /api/media/upload-token`); the C++ edge rejects unsigned/forged uploads whenever `SECURITY_SERVICE_URL` is set. Web client fetches a grant before upload. |
| 2 | **High** | No rate limiting on login/register/reset/SMS endpoints → credential stuffing, SMS bombing, reset-token brute force | Per-IP token-bucket limiter (`middleware.go`) on all auth endpoints: login 15/min, register 10/min, password reset 5/min, SMS send 5/min, OAuth/passkey 60/min. |
| 3 | **High** | CORS `Access-Control-Allow-Origin: *` unconditional | `ALLOWED_ORIGINS` env allowlist, echoed origin + `Vary: Origin`; wildcard only when unset (local dev). |
| 4 | **High** | API would not build from a fresh clone: `//go:embed data/countries.json` but `data/` was gitignored and the file uncommitted | Committed `services/api/data/countries.json` (238 countries, dial codes, flags); narrowed `.gitignore`. |
| 5 | **Medium** | C++ media edge `httpPost` used `inet_pton` only — hostname security URLs (e.g. `http://security:8090` in Compose) silently failed closed, breaking all uploads | Replaced with `getaddrinfo` (IPv4/IPv6, DNS) connect loop. |
| 6 | **Medium** | Default JWT/signing secrets usable in production | `APP_ENV=production` now refuses to boot without a 32+ byte `JWT_SECRET` (Go) and `SIGNING_SECRET` (Rust). |
| 7 | **Medium** | Media file IDs generated from a seeded `mt19937_64` PRNG — IDs double as download tokens | Switched to raw `std::random_device` output (128-bit CSPRNG IDs). Downloads remain unguessable-ID based (Discord/Telegram CDN model); uploads are the authenticated operation. |
| 8 | **Medium** | WebSocket `CheckOrigin` accepted all origins (CSWSH exposure for browser clients) | Origin allowlist from `ALLOWED_ORIGINS`; native clients (no Origin header) unaffected; token auth remains primary. |

Also added: `withSecurityHeaders` middleware (nosniff, frame-deny,
no-referrer, no-store, CSP `default-src 'none'` for the JSON API), and
docker-compose wiring so api → security → media signing works out of the box.

## 4. Remaining recommendations (ops-level, not code bugs)

- ⚠️ **TLS termination**: put the API and media edge behind TLS (Caddy/nginx
  or cloud LB). HTTP is fine inside a private cluster mesh only.
- ⚠️ **Redis-backed rate limiting**: the in-memory limiter is per-node; with
  N API replicas the effective limit is N× — move buckets to Redis when
  running multi-node (cluster engine already tracks nodes).
- ⚠️ **Secrets management**: load `JWT_SECRET`/`SIGNING_SECRET`/provider keys
  from a secrets manager (Vault, AWS SM) rather than env files in production.
- ⚠️ **TOTP secret at rest**: consider envelope-encrypting `users.totp_secret`
  with a KMS key; today it is plaintext in the DB (same as most platforms).
- ⚠️ **DB user least privilege**: the app role owns the schema; for prod, run
  migrations as a separate role and give the app role DML-only grants.
- ⚠️ **ML + security services**: bind to private interfaces / restrict with
  network policy; they have no auth of their own (except the signing HMAC).
- ⚠️ **C++ service hardening**: add a read timeout per connection (slow-loris
  protection) before exposing the media edge directly; today it sits behind
  LB timeouts.
- ⚠️ **Wallet custody**: `CryptoProvider` is the deliberate, only external
  dependency — wire Fireblocks/BitGo/Coinbase WaaS for on-chain moves. The
  internal ledger/P2P needs no provider. Never store private keys in the DB.
- ⚠️ **Audit log alerting**: ship `audit_log` rows to alerting so role grants
  and user suspensions page the on-call.

## 5. Compliance posture

- Passwords/tokens: hashed at rest (argon2id / SHA-256). No plaintext secrets
  in the repo (`.env` gitignored; `.env.example` holds placeholders only).
- PII: emails/phones only in `users`; KYC documents referenced by URL;
  deletes should cascade per GDPR erasure (roadmap item, see GAP_ANALYSIS).
- Money movement: KYC gate before any P2P send; ledger is append-only
  (no UPDATE/DELETE paths on `ledger_entries`).
