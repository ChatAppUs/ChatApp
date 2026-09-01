# Rust Conversion Plan — Safety & Security-Critical Code

Date: 2026-08-29; updated 2026-08-30. Purpose: identify which code files must
move to Rust for memory safety and cryptographic correctness, and which should
stay in Go.

**Status**: `services/security` (Rust, HMAC signing + media upload grants,
release build + unit tests green) is shipped and wired into the API and the
C++ media edge. The 11-file Go→Rust `services/authn` conversion below is the
approved roadmap — the Go implementations it replaces are argon2id/HMAC/
constant-time and pass the full integration suites today, so the conversion is
a hardening step, not a correctness fix.

## Principle

Rust earns its cost where a memory bug or a cryptographic misuse is
catastrophic: authentication, tokens, signatures, money, key material.
Go is already memory-safe and is the better tool for high-fanout I/O
orchestration (JSON REST, SQL, WebSocket control plane) — converting those
paths to Rust would add months of work with no safety gain. So the plan is
**surgical**: convert the trust-critical core, keep the I/O shell in Go.

Current Rust footprint: `services/security` (HMAC-SHA256 signing service),
plus — new — `services/authn` (the P0 auth surface: argon2id/PHC, JWT HMAC,
TOTP, OTP engine, CSPRNG) shipped 2026-09-01, reachable through
`AUTHN_SERVICE_URL`.
The API's `authn_client.go` delegates password/JWT/TOTP/OTP/CSPRNG to it with
a fail-open local fallback. Equivalence proven by a Go-generated argon2
cross-test vector + full-register/login/TOTP/OTP integration pass
(`tests/authn_test.py`).

## Files to convert to Rust: 11 (≈2,600 LOC of ~7,500 in the Go API)

| # | Go file | LOC | Why Rust (safety rationale) | Priority |
|---|---------|-----|------------------------------|----------|
| 1 | `services/api/auth.go` | 197 | argon2id hashing, JWT HMAC sign/verify, constant-time compares. Timing leaks and malleability bugs live here; Rust crypto crates (argon2, hmac, subtle) are audited and constant-time by construction. | P0 |
| 2 | `services/api/handlers_security.go` | 177 | TOTP (RFC 6238) secret generation and verification; touches `users.totp_secret`. | P0 |
| 3 | `services/api/webauthn.go` | 298 | WebAuthn attestation/assertion verification, EC/RSA signature checks over raw bytes. Parser + signature code = classic memory-safety risk class. | P0 |
| 4 | `services/api/handlers_webauthn.go` | 428 | Challenge lifecycle, CBOR parsing of authenticator data — untrusted binary input parsing, the exact place Rust prevents RCE-class bugs. | P0 |
| 5 | `services/api/handlers_oauth.go` | 331 | Google JWKS fetch/parse + RSA verification of untrusted tokens. | P0 |
| 6 | `services/api/handlers_wallet.go` | 319 | Custody ledger: double-entry money movement, KYC gating, `CryptoProvider` boundary to Fireblocks/BitGo. Money code deserves Rust's type discipline (newtype `Amount`, no negative amounts by construction). | P1 |
| 7 | `services/api/handlers_ads.go` | 261 | Advertiser billing: budget debits against the same ledger. Same money-safety rationale as wallet. | P1 |
| 8 | `services/api/handlers_qrlogin.go` | 100 | Login-token minting/approval — session-forgery surface. | P1 |
| 9 | `services/api/providers.go` | ~120 | Holds SMTP/Sumsub credentials and the upload-signing client; isolating secret-handling HTTP into the Rust security service keeps keys out of the Go process entirely. | P2 |
| 10 | `services/api/otp.go` | 126 | Self-built OTP engine: crypto/rand codes, salted hashes, attempt limits, resend throttling. Code generation/verification is a timing-sensitive trust boundary. | P0 |
| 11 | `services/api/handlers_calls.go` | 194 | Mints HMAC SFU room tickets + TURN credentials — a forged ticket means hijacking calls/meetings/live rooms. Belongs with the other HMAC code in Rust. | P1 |

### Target shape after conversion

A single `services/authn` Rust service (axum or std-only like
`services/security`) owning: password hashing/verify, JWT mint/verify,
TOTP, WebAuthn, OAuth token verification, QR tokens, and the ledger
mutation endpoints. The Go API keeps the HTTP routes and delegates the
trust-critical operation over an internal, HMAC-signed channel (the
existing security-service pattern) — or links Rust as a staticlib via cgo
if per-call latency matters more than isolation.

### Verification per conversion

- Port the existing `api_test.go` crypto vectors (argon2 round-trip, JWT
  forgery rejection, TOTP drift window, WebAuthn forged-signature
  rejection) into Rust `#[test]`s first — the tests define the contract.
- The 129-check integration suite must pass unchanged against the Go
  façade.

## Files that must STAY in Go (do not convert): 14

| Go file | Why Go stays |
|---|---|
| `main.go`, `config.go`, `db.go`, `middleware.go` | Routing/config/pool plumbing; no untrusted parsing beyond stdlib HTTP/JSON. |
| `handlers_auth.go` | Orchestrates DB + token issuance; the crypto moves to Rust, the flow stays. |
| `handlers_chat.go` / `handlers_chat2.go` | Conversation/message SQL + WS control plane; I/O-bound, memory-safe already. (The hot fanout path moves to C++ — see CPP_CONVERSION_PLAN.md.) |
| `handlers_social.go` / `_social2` / `_social3` | Feed/posts/stories/reels SQL composition; Go + pgx is the right tool. |
| `handlers_features.go` | Polls/hashtags/bookmarks/blocks — pure SQL orchestration. |
| `handlers_scheduled.go`, `handlers_ttl.go` | Workers with SKIP LOCKED; cron-style I/O. |
| `cluster.go` | Fleet heartbeats/routing; already fail-closed, low complexity. |
| `handlers_admin.go` | RBAC-gated CRUD + audit log; low risk, high change churn — keep it where iteration is fastest. |

## Bottom line

**11 of 25 Go files (~2,600 LOC) convert to Rust** — everything that touches
passwords, tokens, signatures, biometrics, credentials, or money.
The other 14 files are I/O orchestration that Go already serves safely and
at lower maintenance cost. Combined with the existing Rust signing service,
that puts 100% of the cryptographic and financial attack surface in Rust.
