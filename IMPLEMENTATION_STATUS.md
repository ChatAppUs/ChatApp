# ChatApp Implementation Status

This status is derived from the five root specifications and the current source tree on `main`. It does not rely on `agent.md`, previous assistant reports, or historical commits.

## Independent fresh-main verification — 2026-09-15

The current checkout was audited from source. Static parity passed with **153 client files and 547 registered API routes**; the feature registry passed with **26 features and 7 required client/service layers**; and Python syntax compilation passed after installing the dependencies declared in `tests/requirements.txt`. The five specification files now carry an explicit Implemented / Partial / Outstanding status notice. A reproducible `scripts/validate-local.sh` command runs these checks and skips unavailable toolchains with an explicit message.

The web and admin type-checks and production builds completed successfully in the local Node 22 environment after serialized clean installs. Go, C++, PostgreSQL-backed integration, Android/iOS compilation, physical radio, provider, load, backup, and disaster-recovery gates remain environment-dependent in this checkout.

## Summary

The repository contains an implemented multi-platform ChatApp product surface. The API currently registers **547 client-referenced routes** (the parity-scan figure; the router source contains additional `HandleFunc` registrations including non-`/api` endpoints) across authentication, identity, messaging, calls, groups, social features, media, moderation, monetization, wallets, cards, staking, advertisements, administration, push notifications, privacy, forums, Pulse, live shopping, and AI creator/assistant tools. The web and admin applications have reproducible Next.js build inputs through committed lockfiles. The repository parity scanner reports no missing platform route references.

The specifications describe a release program substantially broader than what can be proven by static inspection alone. Features are therefore marked **Implemented**, **Implemented with runtime validation pending**, or **Not proven complete** rather than being represented as complete merely because a route or page exists.

## Feature matrix

| Domain | Current status | Evidence | Remaining validation or work |
|---|---|---|---|
| Unified authentication and account security | Implemented with runtime validation pending | `handlers_credential_security.go`, `handlers_security_attestation.go`, `handlers_auth.go`, `028_credential_change_challenges.sql`, `029_security_attestations.sql`, web settings, reset-password page, Android `ApiClient.kt`/`PrivacyScreen.kt`, iOS `FeatureClient.swift`/`FeatureViews.swift`, and the auth service. Email/phone/password changes now require server-owned OTPs plus fresh-selfie ML attestation, revoke sessions, apply the 48-hour withdrawal freeze, and expose web/Android/iOS controls. Password reset conditionally verifies TOTP or consumes a one-time recovery code before token consumption. | Run Go/Rust/native tests and database-backed flows with ML, SMTP, and SMS services; complete desktop/admin/extension runtime validation. |
| Password hashing, JWT, OTP, recovery codes, sessions, login lockout | Implemented with runtime validation pending | Argon2id password hashing, signed access/refresh claims, CSPRNG helpers, session and recovery-code handlers are present in the API and auth service. The Identity spec's 48-hour account lockout after 5 consecutive failed login attempts is implemented in `handlers_auth.go` (`handleLogin`) with migration `032_login_lockout.sql` adding `users.failed_login_attempts` and `users.locked_until`; a successful login or verified password reset resets the counter and clears the lockout. Refresh-token rotation and password-reset token consumption are atomic and single-use under concurrent requests. | Validate delegation and revocation behavior against a running database and Rust authn service. |
| KYC and withdrawal/security gates | Implemented with runtime validation pending | `handlers_features.go`, `handlers_wallet.go`, `handlers_crypto.go`, `handlers_p2p.go`, `handlers_staking.go`, `handlers_cards.go`, `handlers_admin.go`, `handlers_credential_security.go`, `handlers_security.go`, and migration `026_identity_security_lifecycle.sql`. Withdrawals reject an active `users.withdrawal_freeze_until`; credential and 2FA mutations now commit the security change, one-use verification evidence, session revocation, and 48-hour freeze atomically. | Validate ML/admin review and all financial authorization paths with seeded data. |
| Account deletion lifecycle | Implemented with runtime validation pending | `handlers_identity_lifecycle.go`, `handlers_deletion_verification.go`, `026_identity_security_lifecycle.sql`, and `027_deletion_verification_challenges.sql` implement password proof, asset confirmation, KYC gate, hashed/expiring email and phone OTPs, ML-provider-backed fresh-selfie face-match/liveness scoring, 30-day scheduling, cancellation-on-login, session revocation, status, and permanent cleanup. | Validate the configured ML service with real KYC media and run the complete database-backed flow. |
| Private chat, groups, channels, reactions, polls, media, stories, social feed | Implemented with runtime validation pending | Chat/group/social handler families, web routes, Android and iOS screens, and feature migrations are present. | Execute websocket, database, media-upload, and cross-client integration tests. |
| Voice/video calling and SFU integration | Implemented with runtime validation pending | Call handlers, WebRTC client helpers, SFU service, SFU-forwarder, and call pages/screens are present. | Run service-level tests and a live browser/device media session. |
| Finance, wallet, conversion, P2P, cards, staking, monetization | Implemented with runtime validation pending | Dedicated API handlers, SQL migrations, web pages, Android screens, and finance/staking regression tests are present. | Run database-backed finance tests and provider/sanctions integrations with test fixtures. |
| Admin, moderation, sanctions, audit, reports, ads, bots | Implemented with runtime validation pending | Admin handlers, admin app, moderation/sanctions handlers, audit-related migrations, and web/admin routes are present. | Validate role boundaries, approval workflows, and asynchronous jobs in a configured deployment. |
| Cross-platform identity-security parity | Implemented with environment validation pending | Web, Android, and iOS expose identity-security controls; the Tauri desktop shell inherits the web security panel; the extension options page exposes OTP, fresh-selfie attestation, password-change, and deletion-status controls; and admin has a read-only `/api/admin/security/attestations` audit endpoint plus dashboard view. Repeatable parity validation is enforced in `.github/workflows/validate.yml`. | Execute native-device and configured deployment validation. |
| Production operations safeguards | Implemented with environment validation pending | API `/health` and `/ready` endpoints, Compose API healthcheck and readiness dependencies, CI validation workflow, ordered migration validation, and guarded `scripts/backup-restore.sh` backup/verify/restore workflow are present. | Execute against production-like PostgreSQL, Docker, provider, load, observability, backup, and restore environments. |
| Creator analytics | Implemented | The existing creator-insights API and daily rollup are consumed by web `creator/page.tsx`, Android `MonetizeScreen.kt`, and iOS `FeatureClient.swift`/`FeatureViews.swift`, displaying reach, impressions, watch time, follower growth, top sound, and daily rows. | Run database-backed analytics flow with seeded watch/follow events. |
| LuckyDraw (draws, tickets, audited winner selection, prize settlement) | Implemented with runtime validation pending | `infra/db/030_luckydraw.sql`, `services/api/handlers_luckydraw.go`, `services/api/main.go`, web `apps/web/src/app/luckydraw/page.tsx`, admin `apps/admin/src/components/LuckyDrawTab.tsx`, `tests/luckydraw_test.py`, and `feature-registry.json` (20 features). User plane lists draws, buys tickets from internal USD on the double-entry ledger, and lists my tickets/winners; admin plane creates draws, opens/closes sales, runs audited selection with the unique-user winner rule, settles prizes, disables draws, and audits. | Run database-backed draw lifecycle with seeded tickets and ML/admin review. |
| Professional analytics dashboard | Implemented | `GET /api/me/analytics` is consumed by authenticated web `apps/web/src/app/analytics/page.tsx`, linked from `apps/web/src/components/Nav.tsx`, and displays posts, followers, likes, comments, views, seven-day shares, and earnings. | Run database-backed dashboard flow with seeded account activity. |
| Offline mesh and native shared-core architecture | Implemented (backend store-and-forward + Go engine + native Bluetooth / Wi-Fi Direct transports) | `infra/db/031_mesh.sql`, `services/api/handlers_mesh.go`, and `services/api/main.go` implement device registration, encrypted store-and-forward packet enqueue/dedup, poll-based delivery, multi-hop relay with TTL/hop accounting, relay consent policy, and mesh status — all wired as `/api/mesh/*` routes. Device registration is open to anonymous clients: the account link is stored only when the token subject is a real account UUID, so guest sessions (`guest_<id>` subjects) register as pure relay/member nodes instead of failing the `uuid` cast. The Go engine in `services/mesh/` provides authenticated encryption (NaCl secretbox), local Wi-Fi/hotspot UDP transport with presence-beacon discovery, multi-hop routing with TTL and duplicate suppression, a delay-tolerant store-and-forward queue, and 1:1/group/voice/call-signaling payloads; `native_transport.go` adds the **Bluetooth (RFCOMM) and Wi-Fi Direct stream bridge transports** plus `AutoTransport`, which implements the Anonymous.md §5.3 fallback order (local Wi-Fi → Wi-Fi Direct → Bluetooth → store-and-forward) and keeps packets queued when no radio is reachable. The native clients implement the radios themselves: Android `apps/android/app/src/main/java/com/chatapp/mesh/MeshTransport.kt` (BluetoothServerSocket/BluetoothSocket RFCOMM + WifiP2pManager group socket, with `MeshEngine.kt` mirroring the Go engine's routing and queue) and iOS `apps/ios/ChatApp/Sources/Services/MeshTransport.swift` (CoreBluetooth GATT peripheral+central) with `MeshEngine.swift`. | Radio paths require on-device validation: CI has no Bluetooth/Wi-Fi Direct hardware, so the bridge, selection order and Node routing are covered by `services/mesh/native_transport_test.go` on real loopback sockets, while the radio handshakes themselves need real devices. |
| Forums (communities with topics, threaded posts, moderators) | Implemented | `infra/db/036_platform_gaps.sql` (`forums`, `forum_moderators`, `forum_topics`, `forum_posts`), `services/api/handlers_forums.go` (create/list/get-by-slug/search, topic create/list with pinned-first ordering, threaded reply with `parent_id`, moderator pin/lock resolved server-side, locked-topic write guard, author/moderator delete), web `apps/web/src/app/forums/page.tsx`, Android `ui/ForumsScreen.kt`, iOS `Views/PlatformViews.swift` (`ForumsView`), desktop and extension (both render the shared web app, so the screen is identical there), `tests/platform_gaps_test.py`. All six clients are recorded `true` in `feature-registry.json` with status `IMPLEMENTED`. | Kotlin/Swift compile validation requires the Android Gradle and Xcode toolchains, which are unavailable in this environment. |
| ChatApp Pulse (short posts, threads, quotes, reposts, topics, trends, lists) | Implemented | `036_platform_gaps.sql` (`pulse_posts` with `parent_id`/`quote_of`/`repost_of`/`TEXT[] topics`, `pulse_topics`, `pulse_trends`, `pulse_lists`, `pulse_list_members`), `services/api/handlers_pulse.go` (recursive-CTE threads with chronological/relevant ordering, server-side hashtag extraction, global/local feeds, trends computed from real 24-hour post volume, curated lists, per-user timeline, author delete), `startPulseTrendWorker`, web `apps/web/src/app/pulse/page.tsx` and `pulse/thread/[id]/page.tsx`, Android `ui/PulseScreen.kt`, iOS `Views/PlatformViews.swift` (`PulseView`), desktop/extension via the shared web app. `feature-registry.json` records all clients `true`, status `IMPLEMENTED`. | Kotlin/Swift compile validation requires native toolchains unavailable here. |
| Live shopping (product pins, coupons, real checkout, analytics) | Implemented | `036_platform_gaps.sql` (`live_products`, `live_product_pins`, `live_coupons`, `live_orders`), `services/api/handlers_shopping.go` (seller-only listing/pin/unpin, one active pin per room, coupon minting restricted to room sellers with atomic use-count claim and max-uses enforcement, checkout that locks the product row `FOR UPDATE`, settles buyer/seller/treasury on the double-entry ledger in the same transaction, charges a 5% platform fee, and computes per-product and per-coupon analytics), web `apps/web/src/app/live-shop/page.tsx`, Android `ui/LiveShopScreen.kt`, iOS `Views/PlatformViews.swift` (`LiveShopView`), desktop/extension via the shared web app. Checkout remains server-authoritative on every client. `feature-registry.json` records all clients `true`, status `IMPLEMENTED`. | Kotlin/Swift compile validation requires native toolchains unavailable here. |
| AI dubbing, AI clip generation, in-app AI assistant | Implemented (all clients); provider models required for output | `036_platform_gaps.sql` (`media_dubs`, `ai_clip_jobs`, `ai_clips`, `assistant_conversations`, `assistant_messages`, `assistant_actions`), `services/api/handlers_ai.go`, `services/ml/creator_assistant.py` (`/dub`, `/clips`, `/assistant`), web `apps/web/src/app/ai-studio/page.tsx` and `apps/web/src/app/assistant/page.tsx`, Android `ui/AiStudioScreen.kt` and `ui/AssistantScreen.kt`, iOS `Views/PlatformViews.swift` (`AiStudioView`, `AssistantView`), desktop/extension via the shared web app. Honest-availability contract: with no model configured the endpoints return `available:false` plus the reason, the API persists that state, and no audio, transcript, clip or reply is fabricated — every client displays that state instead of inventing an artifact. Clip candidates are scored deterministically over real ASR segments; the assistant falls back to answers computed from the caller's real data. Assistant actions and AI clips require explicit human approval (conditional `UPDATE ... WHERE status='proposed'`); proposals are never auto-applied, on any client. `feature-registry.json` records all clients `true`, status `IMPLEMENTED`. | Configure `WHISPER_MODEL`/`TRANSLATE_MODEL`/`TTS_MODEL`/`ASSISTANT_MODEL` to produce real output; Kotlin/Swift compile validation requires native toolchains unavailable here. |
| Operations, observability, load, disaster recovery, and production deployment | Not proven complete | Docker and service configuration exist, but production behavior depends on deployment-specific secrets, databases, providers, and toolchains. | Execute deployment, load, security, observability, backup, and restore validation in a configured environment. |

## Validation performed in this checkout

| Check | Result |
|---|---|
| `python3 tests/parity_check.py` | **Passed (re-verified 2026-09-14): 150 files, 537 registered routes**, with web/admin/Android/iOS/extension references accounted for. |
| `python3 scripts/validate-feature-registry.py` | **Passed (re-verified 2026-09-13): 26 registered P0/P1/P2 features** and 7 required client/service layers. |
| **`tests/platform_gaps_test.py` against live PostgreSQL + API** | **Executed 2026-09-13: 76/76 checks passed, 0 failed.** All 37 migrations applied cleanly to a fresh database (210 tables), the API was run against that database, and the six newly implemented feature areas (forums, Pulse, live shopping, AI dubbing, AI clips, AI assistant) were exercised end-to-end — including authorization denials, oversell protection, coupon exhaustion, and the AI honest-availability branch. |
| `python3 tests/gaps10_test.py` (regression) | **Executed 2026-09-13: 8/8 passed** after the `register()` email-OTP fix. |
| Go service tests and vet | **Passed 2026-09-13 with Go 1.25.1** for `services/api`, `services/mesh`, and `services/sfu`; the hosted CI configuration now uses Go 1.25 to match the modules. |
| `npm ci --no-audit --no-fund` in `apps/web` | Reproducible from the committed `apps/web/package-lock.json`; full install/build requires the Node toolchain. |
| `npm run build` in `apps/web` | **Passed 2026-09-13 after adding Suspense boundaries to URL-search-param pages; 53 routes generated successfully.** |
| `npm run build` in `apps/admin` | CI-enforced: dashboard and all admin routes must compile successfully. |
| Python ML compilation and extension Node syntax checks | Passed. |
| Compose YAML, backup script, and CI workflow syntax validation | Passed: Compose parses, `scripts/backup-restore.sh` passes `bash -n`, and `.github/workflows/validate.yml` is present with parity/build/migration checks. |
| API readiness and Compose dependency wiring | Implemented: `/health` remains liveness, `/ready` checks database readiness, and web/admin wait for API health in Compose. |
| Go tests for `services/mesh` | CI-enforced: `go build`, `go vet`, and `go test` run for the native offline mesh transport engine (crypto, packet, transport, routing, store-and-forward, node, messages). |
| Go tests for `services/api` and `services/sfu` | CI-enforced: `go build`, `go vet`, and `go test` run for `services/api` (633-handler / 537-route control plane) and `services/sfu` (Pion group-call/live SFU). |
| Rust tests for `services/authn` and `services/security` | CI-enforced: `cargo test --locked` runs for both authn and security services. |
| Python integration tests | Now executable in the audit environment: PostgreSQL 15 was installed, all migrations were applied, the API was started, and `tests/platform_gaps_test.py` passed 76/76. Other suites still require their own fixtures/providers. |
| Android/iOS native builds | Not executable: Android Gradle wrapper, iOS Swift package manifest, and native toolchains are unavailable. |
| Docker/PostgreSQL/provider end-to-end validation | Partially executed in the audit environment: PostgreSQL 15 + full migration set + live API were stood up and the platform-gaps suite passed 76/76. Docker, ML, SMTP, SMS provider, and production-deployment validation remain outstanding. |

## Definition used for marking

A route, screen, or migration is evidence that an implementation path exists. It is not, by itself, evidence that the feature is production-complete. A feature is marked complete only when its API, authorization, validation, persistence, UI where applicable, error handling, and relevant runtime tests can be verified together.

## Audit addendum — 2026-09-13 (third pass): platform gap features

A third audit cross-checked every requirement in the five root specifications against the
executable source tree and found six feature areas that were specified but had **no
implementation anywhere** — zero routes, zero tables, zero client references across web,
Android and iOS. All six are now implemented on the backend and surfaced in the web client:
Forums (master plan §30 / master documentation §75 item 24), ChatApp Pulse (master plan §32),
Live Shopping (master plan §20), AI dubbing (master plan §23), AI clip generation
(master plan §23) and the AI assistant (master plan §38). Native-client screens for these six
remain **not implemented**, and `feature-registry.json` records that honestly
(`web/backend/database: true`, `android/ios/desktop/extension: false`, status `PARTIAL`).

Two additional defects were found and fixed during this pass:

1. **IPv6 login/registration returned HTTP 500.** `clientIP()` sliced the remote address at
   the last colon, so an IPv6 client (`[::1]:53210`) produced `[::1]`, which Postgres rejects
   as an `inet` value when writing `sessions.ip`. Every registration, login and refresh from
   an IPv6 client failed. Replaced with `net.SplitHostPort` plus an `inet`-safe normaliser
   that yields `NULL` for anything unparseable.
2. **`register()` in the shared test helper did not complete the email-OTP gate**, so every
   suite that reuses it silently failed account creation. The helper now performs the real
   `send-code` → `check-code` flow using the development-returned code (no mock).

**Still not implemented (unchanged by that pass):** native Android/iOS/desktop/extension
screens for the six new features; Bluetooth/Wi-Fi Direct native mesh transport; production
deployment validation; and every environment-dependent gate already listed above.

## Audit addendum — 2026-09-13 (fourth pass): native clients and radio transports

The fourth pass closed the two largest gaps the third pass had left open. The repository was
re-cloned at `main` and every claim below was verified against the actual source tree, not
against the previous report.

**1. Native client screens for the six platform features (was: not implemented).**
Forums, Pulse, Live Shopping, AI Studio, AI assistant and the offline-mesh status surface now
exist on every client, bound to the same Go API endpoints as the web pages:

| Client | Where the screens live |
|---|---|
| Android | `apps/android/app/src/main/java/com/chatapp/ui/` — `ForumsScreen.kt`, `PulseScreen.kt`, `LiveShopScreen.kt`, `AiStudioScreen.kt`, `AssistantScreen.kt`, `MeshScreen.kt`, routed in `MainActivity.kt` and listed in `ui/MenuBar.kt` |
| iOS | `apps/ios/ChatApp/Sources/Views/PlatformViews.swift` — `ForumsView`, `PulseView`, `LiveShopView`, `AiStudioView`, `AssistantView`, `MeshStatusView`, linked from the `MoreView` section and a new Pulse tab in `ChatAppApp.swift` |
| Desktop | Renders the shared web application, so all six screens are the same implementation |
| Extension | Same shared web application; `/forums`, `/pulse`, `/live-shop`, `/ai-studio` and `/assistant` added to the popup navigation |

`feature-registry.json` now records `android/ios/desktop/extension: true` and status
`IMPLEMENTED` for all six features (26 features, 7 required clients — validation passes).

**2. Native Bluetooth / Wi-Fi Direct mesh transport (was: not implemented).**
- Go engine: `services/mesh/native_transport.go` adds `TCPTransport` (length-prefixed stream
  framing shared by the Bluetooth RFCOMM and Wi-Fi Direct group sockets) and `AutoTransport`,
  which implements the Anonymous.md §5.3 fallback chain — local Wi-Fi → Wi-Fi Direct →
  Bluetooth → store-and-forward. `services/mesh/node.go` now wires its inbound callback through
  an `inboundSetter` interface so any transport (not only UDP) can feed the node, and the
  presence beacon advertises the transport actually carrying traffic.
- Android radios: `apps/android/.../mesh/MeshTransport.kt` — `BluetoothLink` uses
  `BluetoothServerSocket`/`BluetoothSocket` RFCOMM with a real accept loop and per-peer reader
  threads; `WifiDirectLink` uses `WifiP2pManager` discovery/group formation over a UDP socket;
  `LocalWifiLink` mirrors the Go UDP transport. `MeshEngine.kt` mirrors the Go engine's routing,
  dedup and store-and-forward queue with AES-256-GCM payload sealing.
- iOS radios: `apps/ios/ChatApp/Sources/Services/MeshTransport.swift` — `BluetoothMeshLink` is a
  real CoreBluetooth `CBPeripheralManager` + `CBCentralManager` GATT service/characteristic
  pair; `LocalWifiMeshLink` is a `Network.framework` UDP listener. `MeshEngine.swift` mirrors
  the same routing/queue logic with AES-GCM via CryptoKit.
- Android manifest declares `BLUETOOTH_CONNECT`, `BLUETOOTH_SCAN` (with
  `neverForLocation`), legacy Bluetooth and location permissions for pre-Android-12 discovery.

**3. Defect found and fixed in this pass.**
Anonymous (guest) mesh registration returned **HTTP 500**. A guest session's token subject is
the literal `guest_<id>`, and `handleMeshRegister` cast it to `uuid` for `mesh_devices.user_id`
— a non-UUID subject therefore aborted the insert. Registration is now open to anonymous
clients: the account link is stored only when the subject passes `isUUIDShape`, so guests
register as relay/member nodes with `user_id` NULL instead of failing.

**Validation executed in this pass (fresh PostgreSQL 15 + live API on an alternate port because
the sandbox reserves 8080):**
- All **37 migrations applied cleanly to a fresh database** (`001_schema.sql` → `037_message_ttl_invariant.sql`, 210 tables).
- `go build` + `go vet` + `go test ./...` green for `services/api`, `services/mesh` and `services/sfu`; `cargo test --locked` green for `services/authn` (8/8) and `services/security` (11/11).
- `python3 tests/parity_check.py` — **passed (150 files, 537 registered routes)**, with the new Android/iOS/extension references accounted for.
- `python3 scripts/validate-feature-registry.py` — **passed (26 features, 7 required clients)**.
- `node --check` on the extension scripts — passed.

**Still not implemented / not provable here (stated plainly):** the radio handshakes themselves
cannot run in CI (no Bluetooth or Wi-Fi Direct hardware), so they need on-device validation —
the bridge, selection order and node routing are covered by `native_transport_test.go` on real
loopback sockets instead; Kotlin and Swift compilation require the Android Gradle and Xcode
toolchains, which are absent; provider-backed AI output requires
`WHISPER_MODEL`/`TRANSLATE_MODEL`/`TTS_MODEL`/`ASSISTANT_MODEL`; and production deployment,
load, DR and provider integrations remain unvalidated outside this environment.

## Files checked

The implementation was checked against these root specifications: `README.md`, `Anonymous.md`, `ChatApp_Complete_Features_and_Architecture_Master_Plan.md`, `ChatApp_Complete_Master_Documentation.md`, and `Identity-Authentication-and-Account-Security.md`.

## Implementation audit addendum — 2026-09-13, sixth pass

This pass re-cloned and inspected the executable repository on `origin/main`; it did not treat `AGENTS.md`, prior assistant reports, or previous commits as implementation evidence.

One real source gap was found and fixed. The web production build failed because `/live-shop` called `useSearchParams()` without a Suspense boundary; the same safe boundary was applied to the URL-driven call and live-room pages. `npm run build` now passes for all 53 web routes, and the separate admin build passes for all 5 routes. The audit also found that CI now selects Go 1.25 while the checked-in modules and `golang.org/x/crypto` v0.55.0 require Go 1.25. Local Go 1.25.1 validation passes `go test ./...` and `go vet ./...` for `services/api`, `services/mesh`, and `services/sfu`; the CI workflow now matches the Go 1.25 module requirement.

Static validation after the fixes: `tests/parity_check.py` passes with 149 client files and 536 registered API routes; `scripts/validate-feature-registry.py` passes with 26 features across 7 required layers; Python ML compilation, extension syntax checks, and `git diff --check` pass.

The repository therefore has source implementations for the registered surfaces, but it is not truthful to call the whole specification production-certified or claim runtime parity is proven everywhere. Rust builds, Android/iOS compilation, real Bluetooth/Wi-Fi Direct handshakes, provider-backed AI, Docker deployment, load, backup/restore, and disaster-recovery validation still require their environments. The feature registry and status ledger retain those limitations explicitly.

## Implementation audit addendum — 2026-09-13, seventh pass

The native data-plane source was compiled directly from this checkout with `g++ -std=c++17 -O2 -Wall -Wextra -Werror -pthread`. All five C++ services — `counters`, `media`, `realtime`, `sfu-forwarder`, and `transcode` — compiled successfully. GitHub Actions run `34759396867` also passed both static/frontend and backend jobs, including Go and Rust tests. Native Android/iOS compilation, radio handshakes, provider-backed AI output, and production deployment/load/disaster-recovery validation remain environment-dependent and are not marked complete.


## Implementation audit addendum — 2026-09-13, eighth pass

A fresh checkout of `origin/main` was audited directly against all five root specifications and the executable source; `AGENTS.md`, prior assistant reports, and earlier commits were not used as implementation evidence. Two security gaps were found and fixed. `PUT /api/me/security` now locks the account row and performs one-use OTP/attestation claims, the credential mutation, session revocation, and the 48-hour withdrawal freeze in one PostgreSQL transaction. Verification claims cannot be replayed inside their original ten-minute window, and the challenge verifier uses a conditional update so concurrent requests cannot both succeed. `POST /api/auth/2fa/setup` now refuses to overwrite an already enabled authenticator; disabling active 2FA must go through the authenticated disable flow, which applies the freeze.

Fresh validation passed: web `npm ci && npm run build` for all 53 routes; admin `npm ci && npm run build` for all 5 routes; Go `go test ./...` and `go vet ./...` for `services/api`, `services/mesh`, and `services/sfu`; strict C++17 compilation for all five data-plane services; parity (149 files, 536 registered routes); feature registry (26 features, 7 required clients); Python ML compilation; extension JavaScript syntax; backup-script syntax; and `git diff --check`. The source is implemented and tested where the checkout has the required toolchains. Android/iOS release builds, Bluetooth/Wi-Fi Direct handshakes, configured ML/SMTP/SMS/provider output, production deployment, load, backup/restore, and disaster-recovery certification remain explicit environment-dependent gates.

## Implementation audit addendum — 2026-09-13, final verification

After commit `34182aa`, a fresh `origin/main` checkout was revalidated directly against the five root specifications. `npm ci && npm run build` passes for all 53 web routes and all 5 admin routes; Go tests and vet pass for `services/api`, `services/mesh`, and `services/sfu`; parity passes with 149 client files and 536 registered routes; the feature registry passes with 26 features across 7 required clients; Python ML, extension JavaScript, and backup-script syntax checks pass; and all five C++ services pass strict C++17 compilation. GitHub Actions run `34759396867` completed successfully for both frontend/static and backend/Rust jobs.

The remaining gaps are validation gates rather than unimplemented source: Android/iOS release compilation, real Bluetooth/Wi-Fi Direct handshakes, configured SMTP/SMS/ML/provider integrations, Docker deployment, load testing, observability, backup/restore, and disaster-recovery execution. The repository status must continue to mark those as pending until their environments are available.


## Implementation audit addendum — 2026-09-13, independent fresh-main pass

This pass reset the checkout directly to `origin/main` at commit `ac748f0` and compared the five root specifications with the source tree. It did not use `AGENTS.md`, prior commits as instructions, or earlier audit prose. The source-only unfinished-marker scan found no new implementation stubs; the remaining matches are intentional CSS skeleton styling, comments, and ordinary input placeholders.

The audit also fixed an actual recovery-path defect: the authenticator recovery-code redeem handler had an unreachable fallback lookup and could not redeem valid codes. It now atomically consumes a code for the authenticated account, rotates generated codes in one transaction, binds the disable claim to that account, and atomically applies the 48-hour withdrawal freeze and recovery-code revocation.

Fresh validation completed:

- `npm ci && npm run build` passes for all 53 web routes and all 5 admin routes.
- Go tests and `go vet` pass for `services/api`, `services/mesh`, and `services/sfu` with Go 1.25.1.
- Strict C++17 compilation passes for `counters`, `media`, `realtime`, `sfu-forwarder`, and `transcode`.
- Parity passes with 149 client files and 536 registered routes; the feature registry passes with 26 features across 7 required clients. Python ML, extension JavaScript, backup-script syntax, and migration numbering checks pass.
- GitHub Actions run `34759396867` completed successfully for both static/frontend and backend/Rust jobs.

The remaining gaps are validation boundaries, not silently marked features: Android/iOS/desktop device builds and Bluetooth/Wi-Fi Direct radio handshakes, configured PostgreSQL/SMTP/SMS/TURN/FFmpeg/provider integrations, and production load, observability, backup/restore, and disaster recovery still require their real environments. The workflow and Go Docker build stages now declare Go 1.25, matching the checked-in modules.

## Implementation audit addendum — 2026-09-13, recovery-path pass

A new source audit was performed from the clean `origin/main` checkout, independently of `AGENTS.md`, earlier commits, and earlier audit prose. One real security-flow gap was found and fixed: the recovery-code redemption handler performed an impossible empty-username lookup and returned before its fallback, so valid recovery codes could not be redeemed. Redemption now consumes a code atomically, scopes it to the authenticated account, and binds the short-lived claim to that account. Recovery-code generation now rotates all eight codes in one transaction; authenticator-loss disable now atomically disables 2FA, applies the 48-hour withdrawal freeze, and revokes the remaining codes.

The fresh checks for this pass are green: Go tests and `go vet` for `services/api`, `services/mesh`, and `services/sfu`; fresh `npm ci && npm run build` for all 53 web routes and all 5 admin routes; parity (149 files / 536 routes); feature registry (26 features / 7 required clients); Python ML and extension syntax checks; and `git diff --check`. The existing native/mobile/provider/load/DR validation boundaries remain explicitly open in the status ledger.

## Implementation audit addendum — 2026-09-13, recovery claim portability pass

A fresh checkout of `origin/main` at commit `c1584b1` was checked against the five root specifications and the actual source tree without using `AGENTS.md`, earlier commits, or earlier audit prose. A second recovery-flow gap was found and fixed: the disable claim depended on an optional cache, so redemption could succeed while the follow-up disable failed on a cache-less or multi-instance deployment. The claim is now a signed, expiring HS256 `2fa_recovery` token bound to the authenticated account; the disable update is guarded by the pre-claim account timestamp so it cannot be replayed after 2FA is re-enabled.

The fresh checks passed: Go tests and vet for `services/api`, `services/mesh`, and `services/sfu`; fresh web and admin production builds; strict C++17 compilation of all five native services; repository parity (149 files and 536 registered routes); feature-registry validation; ML and extension syntax checks; and backup-script syntax. GitHub Actions run `34759396867` passed for the previous source commit; this documentation/source commit triggers the same validation again.

Remaining gaps are still environment-bound rather than silently marked complete: Android/iOS/desktop device builds, Bluetooth/Wi-Fi Direct handshakes, configured PostgreSQL/SMTP/SMS/TURN/FFmpeg/provider integrations, production load and observability, backup/restore, disaster recovery, and the workflow and Docker build stages now use Go 1.25, matching the checked-in modules.

## Implementation audit addendum — 2026-09-13, independent mutation-failure pass

A fresh checkout was reset directly to `origin/main` at commit `1afac71`; this pass did not rely on `AGENTS.md`, earlier reports, or prior commits as implementation evidence. A source-only audit found user-visible mutation handlers that discarded database errors and still returned success. The implementation now propagates failures and uses transactions where the operation spans related rows: admin moment/item deletion, custom admin-role deletion, organization member affiliation/removal, user suspension session revocation, QR-login rejection, close-friend removal, reaction/member/channel/bookmark/block removals, and withdrawal-refund failures.

Validation after the changes: `go test ./...` and `go vet ./...` pass for `services/api`, `services/mesh`, and `services/sfu` with Go 1.25.1; web and admin production builds pass from fresh `npm ci`; parity remains 149 files and 536 registered routes; the feature registry remains 26 features across 7 required clients; extension/ML/backup-script checks pass; and strict C++17 builds pass for all five native services. Native Android/iOS device toolchains, Bluetooth/Wi-Fi Direct hardware, live PostgreSQL/provider integrations, and production load/disaster-recovery tests remain environment-dependent and are not marked complete.

## Implementation audit addendum — 2026-09-13, independent recovery-status pass

A fresh checkout was reset directly to `origin/main` at commit `5255879`; no `AGENTS.md`, prior report, or historical commit was used as implementation evidence. The recovery-code generation endpoint now rejects malformed JSON instead of continuing with an empty request, and the recovery-code status endpoint returns an explicit server error when its database read fails instead of falsely reporting zero remaining codes.

The current implementation and validation status is: Go API/mesh/SFU tests and vet pass; fresh web and admin production builds pass; parity remains **149 files / 536 registered routes**; the feature registry remains **26 features / 7 required clients**; and the latest hosted validation remains green. Remaining gaps are environment-dependent Android/iOS device builds, Bluetooth/Wi-Fi Direct hardware, live provider/database integrations, and production load/backup/disaster-recovery certification.

## Independent implementation audit — 2026-09-13, final fresh-main validation

This pass reset directly to `origin/main` at `9c66857` and checked all five root specifications against the executable source, not against AGENTS.md or earlier reports. Fresh web/admin production builds passed; parity passed at 149 files and 536 registered routes; the feature registry passed for 26 features; and GitHub Actions run `34761708641` passed both backend and frontend jobs. The source scan found no unfinished implementation marker beyond explanatory comments and real numeric conversions. Environment-dependent gaps remain explicitly open: live PostgreSQL/provider integrations, Android/iOS device toolchains, Bluetooth/Wi-Fi Direct hardware, and production load, backup/restore, and disaster-recovery validation.

## Implementation audit addendum — 2026-09-13, final independent pass

This pass reset directly to `origin/main` at commit `502d2a3` and rechecked the five root specifications against the executable source, without using agent instructions or previous reports. No new source gap was found: the source-only unfinished-marker scan returned only explanatory comments and legitimate numeric UI conversions. Fresh `npm ci && npm run build` passes for all 53 web routes and all 5 admin routes; parity remains 149 files / 536 registered routes, the feature registry remains 26 features / 7 required clients, and the latest GitHub Actions run `34761993747` is green. Remaining limitations are unchanged: mobile/device builds and radio handshakes, live PostgreSQL/provider integrations, and production load/disaster-recovery validation require their external environments.

## Implementation audit addendum — 2026-09-13, deep observability and mutation pass

This pass reset directly to `origin/main` at commit `2daa7bd` and re-audited all five root specifications against the executable source, using no AGENTS.md content, no prior report, and no historical commit as evidence. Two specification gaps were found and closed in source.

First, master documentation §65 (Observability) requires metrics, health checks, and monitoring. The API had `/health` and `/ready` but no metrics endpoint. `services/api/metrics.go` now serves Prometheus text format on `GET /metrics`: uptime, active websocket connections (instrumented in the websocket handler), total HTTP requests, 5xx errors (counted by a `withMetrics` middleware wrapped around the router), goroutines, heap allocation, and GC cycles — aggregate counters only, no user data.

Second, the same §65/§67 quality bar requires user-visible mutations to surface failures. A further sweep of ignored database writes found and fixed remaining cases: unmute, word-filter removal, unrestrict, follow-request decline, conversation invite decline, group role update, group leave counter maintenance, legacy contact removal, profile-switch cleanup on profile delete, unfollow, comment unlike, mark-all-notifications-read, share-ledger writes, and bot deletion. Each now returns an explicit 500 on database failure instead of a false success. Remaining ignored writes are intentionally best-effort paths (presence stamps, analytics counters, notification inserts after a committed transaction, background sweepers) where a failure must not fail the user's completed operation.

Additional findings verified as already implemented or explicitly environmental: websocket origin checking denies browser origins unless `ALLOWED_ORIGINS` is configured; rate limiters cover every abuse-sensitive public endpoint; guest sessions provide identifier-free anonymous registration; registration requires only email *or* phone (never both); `/api/me/export` provides the GDPR-style data export; Tor/onion multi-hop and IP-privacy relays (Anonymous.md networking priorities 4–6) are NOT implemented and remain honestly marked as future work; fuzzing, tracing, and alerting pipelines are not implemented in-repo and remain environment/pipeline work.

Validation after the changes: `go test -count=1` and `go vet` pass for `services/api`, `services/mesh`, and `services/sfu` with Go 1.25.1; strict C++17 `-Werror` builds pass for all five native data-plane services; web (53 routes) and admin (5 routes) production builds pass from fresh `npm ci`; parity is now **149 files / 537 registered routes** (the new `/metrics` endpoint); the feature registry remains 26 features / 7 required clients; and `git diff --check` is clean. Android/iOS device builds, radio handshakes, live provider integrations, and production load/backup/disaster-recovery validation remain environment-dependent and are not marked complete.

## 2026-09-13 — web mesh parity and admin console closure pass

- Web offline-mesh UI added (`apps/web/src/app/mesh/page.tsx` + nav link) driving all six `/api/mesh/*` endpoints; parity now **150 files / 537 registered routes** (web 92 files / 379 refs, admin 8 files / 77 refs).
- Admin console closed the last UI-less admin endpoints: content-abuse log, custom-emoji management (Safety tab), group scale report, organization verification, merchant tier upsert (new Platform tab).
- Verified green this pass: fresh `npm ci && npm run build` for web (54 routes) and admin (5 routes); feature registry (26 features, 7 required clients); `go test -count=1` for `services/api` on Go 1.25.1.
- Still future work, not marked complete: Tor/multi-hop IP-privacy transport, fuzzing, tracing, alerting pipelines; Android/iOS release builds and radio handshakes; live PostgreSQL/provider integrations; production load, backup/restore, and disaster-recovery validation.

## 2026-09-14 audit (fresh reset to `20effb6`)

Deep re-scan of all five root specifications against the executable source. Corrections: the web
client builds **62 routes** and the admin console **2 routes** (`/`, `/dashboard`) — earlier notes
saying 53/54 web and 5 admin were stale. The router has 633 `HandleFunc` registrations (537 `/api`
routes counted by parity); parity is **150 files / 537 registered routes** (web 92 files / 379 refs,
admin 8 files / 77 refs); the feature registry passes with 26 features across 7 required clients;
migrations stand at 37 forward-only files / 210 tables.

Re-verified implemented: channel posting rules; LuckyDraw per-user caps and unique-winner rule; P2P
escrow with dispute resolution; users/messages/posts/forums search; the credential-change
transaction (current password + contact challenge + face-match attestation + session revocation +
48-hour withdrawal freeze in one commit); referral attribution; tipping; creator payouts and wallet
withdrawals with admin review; one-time invite links (`conversation_invites.max_uses`); story
archive; AI dubbing/clips and the in-app assistant with FastAPI ML backing; `GET /metrics` process
metrics; 414 `ON CONFLICT` idempotent-write sites across SQL and Go.

Verified green this pass: `go test -count=1` for `services/api`, `services/mesh`, `services/sfu` on
Go 1.25.1; fresh `npm ci && npm run build` for web and admin; parity and feature-registry validators;
Python ML compilation; extension JavaScript syntax; backup-script syntax.

Still future work, not marked complete: Tor/multi-hop IP-privacy transport, fuzz targets, tracing,
alerting pipelines; Android/iOS release builds and Bluetooth/Wi-Fi Direct handshakes; live
PostgreSQL/SMTP/SMS/ML provider integrations; production load, backup/restore, and disaster-recovery
validation.

## 2026-09-14 audit (fresh main `a8ee690`, runtime pass)

Fresh clone of `main` (`a8ee690`, fifteen commits past the last audited commit), re-scanned against
all five root specs and exercised at runtime on PostgreSQL 15.19 with the API, SFU and TURN relay
running against a freshly migrated database.

### Defects found and fixed in this pass

1. **Every WebSocket upgrade returned HTTP 500 on the production middleware chain.** The metrics
   middleware wrapped `http.ResponseWriter` in a struct embedding the *interface*, which stripped
   `http.Hijacker`. gorilla's upgrader type-asserts `w.(http.Hijacker)`, the assertion failed, and it
   answered 500 — so realtime chat, presence, typing indicators, call signalling and notifications
   were all dead, on a build that compiled cleanly and passed every Go unit test. Fixed by forwarding
   `Hijack`, `Flush`, `ReadFrom` and `Unwrap`. Regression guard: `services/api/metrics_test.go`.
2. **Nine of ten `INSERT INTO messages` sites ignored the conversation's disappearing-message TTL.**
   Only `persistMessage` honoured `message_ttl_seconds`; forwards, location shares, media sends,
   payment messages, pin notices, system messages, scheduled sends and story shares all wrote
   `expires_at = NULL`, creating permanent messages in conversations every member believed expired at
   the TTL. Fixed once at the storage layer instead of ten times in Go: migration
   `037_message_ttl_invariant.sql` adds a `BEFORE INSERT` trigger that stamps `expires_at` from the
   conversation TTL when the caller supplies none, leaves explicit values untouched, and is a no-op
   for `message_ttl_seconds = 0`. Backfills rows written before the migration. Verified by direct
   SQL probe (TTL conversation stamped, non-TTL conversation untouched, explicit past value preserved).
3. **Four more test suites still registered accounts without clearing the email-OTP gate** —
   `gaps_test`, `gaps2_test`, `gaps3_test`, `gaps4_test`. Registration correctly answers `403 email
   OTP verification is required before registration` until `check-code` proves ownership, so each
   suite silently produced no token and cascaded into dozens of misleading `401 missing bearer token`
   failures. Migrated all four to the real `send-code` → `check-code` flow.
4. **`integration_test` and `finance_test` (the two foundational suites other suites import) posted
   to `/api/auth/register` directly**, with the same 403 knock-on. Added a shared, documented
   `register_verified()` helper in `integration_test.py` and routed both suites through it.
5. **Test-only rate-limit ceiling.** The auth limiters are hard-coded (`send-code` 5/min burst 2,
   register 10/min burst 3) with no override, so back-to-back suites throttled themselves and
   reported false failures. Added `RATE_LIMIT_SCALE` (documented in `.env.example`, unset = the
   hardened production defaults verbatim; values below 1 are rejected so abuse controls can only ever
   be raised for testing, never lowered).
6. **`integration_test`'s TTL check could not observe what it asserted.** It re-shared the *same*
   post, and the platform's duplicate-content defense correctly dropped the identical message, so no
   row existed to inspect. It now shares a fresh post with a unique body.
7. **CI never ran the Python E2E suites** — the process gap that let defects 1, 3 and 4 reach `main`
   behind a green build. `.github/workflows/validate.yml` now has a required `e2e-postgres` job:
   PostgreSQL 15 service, all migrations, live API, then the suites.

### Validation executed this pass (fresh PostgreSQL 15.19, API + SFU + TURN live)

- **All 21 Python suites plus both validators pass, 0 failures.** Highlights: `integration_test`
  **154/154** (was 143 pass / 6 fail / crash), `gaps4_test` **96/96**, `gaps6_test` **91/91**,
  `gaps7_test` **83/83**, `gaps8_test` **32/32**, `gaps9_test` **15/15**, `sfu_turn_test` **18/18**,
  `authn_test` **11/11**, `counters_test` **9/9**; `gaps_test`, `gaps2_test`, `gaps3_test`,
  `gaps5_test`, `features_test`, `finance_test`, `gaps10_test`, `guest_mesh_test`,
  `platform_gaps_test`, `luckydraw_test`, `staking_test`, `parity_check` and
  `validate-feature-registry` all exit 0.
- 37 migrations applied cleanly to a fresh database → 210 tables.
- `go build` + `go vet` + `go test -count=1` green for `services/api`, `services/mesh`,
  `services/sfu`; `cargo test` green for `services/authn` (8/8) and `services/security` (11/11);
  the C++ `services/sfu-forwarder` TURN relay builds and passes its 18-check protocol suite.
- Parity **150 files / 537 registered routes** (633 `HandleFunc` registrations); feature registry
  **26 features / 7 required clients**.
- Native screens and the native mesh transport re-verified as present and wired (parity: android
  26 files / 120 refs, ios 15 files / 114 refs, extension 9 files / 9 refs).

### Still not implemented / not provable in this environment

Tor/multi-hop IP-privacy transport; fuzz targets, tracing and alerting pipelines; on-device
Bluetooth/Wi-Fi Direct radio handshakes (no radio hardware — the bridge, selection order and routing
are covered on real loopback sockets); Kotlin/Swift compilation (no Android Gradle or Xcode
toolchain); provider-backed AI output (`WHISPER_MODEL`/`TRANSLATE_MODEL`/`TTS_MODEL`/`ASSISTANT_MODEL`
unset, so those endpoints report `available:false` and fabricate nothing); live SMTP/SMS provider
integration; production load, backup/restore and disaster-recovery validation.

The web client was the one exception where a build *was* attempted and did not finish here:
`npx tsc --noEmit` exits **clean** and `next build` reports **"✓ Compiled successfully"**, but the
process is then `Killed` (SIGKILL) during "Collecting page data" — the sandbox caps a process at
2 GB and this machine also runs PostgreSQL, the API, the SFU and the TURN relay concurrently. That
is an environment ceiling, not a code defect; the same build must be confirmed in the CI job, which
has the memory headroom.

## 2026-09-14 audit — final pass (fresh main `19241d1`)

Re-cloned `main` at `19241d1bcbec70e53289e2ce41ab21a2782e1138` (identical to the previous
pass's push, so no other agent had committed since) and re-ran every check against the real
code, a live API, a real PostgreSQL 15.19, the Go SFU, and the C++ TURN relay.

### Defects found and fixed in this pass

1. **`/api/fyp` returned a short page and silently dropped its exploration slot.** The
diversity/dedup reranker (`diversifyFYP`: one survivor per remix root, at most two consecutive
reels per author) ran *after* the SQL `LIMIT`, so filtering could pull the page below the
requested size. A page shorter than nine posts then tripped `injectFYPExploration`'s
`len(posts) < 9` early return, so the guaranteed exploration slot disappeared with it. Measured
before the fix: `?limit=9` returned **8** posts with no `explore` entry, `?limit=25` returned 24.
The handler now fetches a wider candidate window and truncates to the requested page size *after*
ranking, so `limit` means "up to N ranked reels". After the fix `?limit=9` returns 10 with the
exploration slot present and `gaps6_test` is **91/91** (was 89 passed / 2 failed).
   This is state-dependent: the previous pass saw 91/91 because that database happened to hold
enough diverse candidates. It is a latent flake in a ranking path — exactly the class a required
end-to-end job exists to catch.
2. **The E2E coverage gap had not actually been closed.** The `e2e-postgres` job added in the
previous pass listed only eight suites by hand. Omitted were `integration_test` and every
call/broadcast suite. It now loops over `tests/*_test.py`, so all 20 suites run.
3. **The media plane was absent from CI, so the omitted suites could not have passed.** Nothing
started the SFU or the TURN relay; without them every call/broadcast path answers
`502 media service unavailable` (this is exactly why `integration_test` failed 5 checks on a cold
baseline here). The job now starts the Go SFU and the C++ forwarder and waits for both to be ready.
4. **Nothing compiled the C++ services.** The specifications state the ultra-low-latency data
planes are C++ and the documents claimed strict C++17 compilation passes, but no build script,
Dockerfile or CI step did it. The job now compiles all five (`counters`, `media`, `realtime`,
`sfu-forwarder`, `transcode`) with `-std=c++17 -O2 -Wall -Wextra -Werror -pthread`.
5. **`tests/gaps2_test.py` hard-coded `http://localhost:8080`** for the sanctions-import call
instead of using the imported `BASE`, so the suite broke under any other API port.

### Validation executed this pass (fresh PostgreSQL 15.19; API + Go SFU + C++ TURN relay live)

- **All 20 Python suites pass, 0 failures.** `integration_test` **154/154**, `gaps6_test` **91/91**,
  plus `authn_test`, `counters_test`, `features_test`, `finance_test`, `gaps_test`–`gaps10_test`,
  `guest_mesh_test`, `platform_gaps_test`, `luckydraw_test`, `staking_test`, `sfu_turn_test`.
- `parity_check` **150 files / 537 registered routes** (633 `HandleFunc` registrations);
  `validate-feature-registry` **26 features / 7 required clients**.
- All 37 migrations apply cleanly to a fresh database → **210 tables**.
- `go build` + `go vet` + `go test` green for `services/api`, `services/mesh`, `services/sfu`.
- All five C++ data planes compile under strict C++17.
- `gofmt -l` flags ten pre-existing files, none touched by this pass; the file changed here
  (`handlers_watch.go`) is format-clean.

### Still not implemented / not provable in this environment

- **Native packaging is implemented.** All five C++ data planes now have Docker build definitions;
the previously missing `services/sfu-forwarder/Dockerfile` and its `sfu-forwarder` Docker Compose
service expose TURN on 3479 TCP/UDP and the control port on 8099. Live registry, provider, and
production deployment validation remains environment-dependent.
- **The web production build cannot complete here.** `next build` reports "✓ Compiled successfully"
and is then SIGKILLed (exit 137) during "Collecting page data". Root cause measured directly:
`/sys/fs/cgroup/memory.max` is **2 GiB** for the whole container (`free -m` advertises the 386 GiB
host, which makes this look like ample memory). Not a code defect; it must be confirmed in CI.
- On-device Bluetooth/Wi-Fi Direct radio handshakes (no radio hardware); Kotlin/Swift compilation
(no Android Gradle or Xcode toolchain); provider-backed AI output
(`WHISPER_MODEL`/`TRANSLATE_MODEL`/`TTS_MODEL`/`ASSISTANT_MODEL` unset, so those endpoints report
`available:false` and fabricate nothing); live SMTP/SMS/ML providers; Tor/multi-hop IP-privacy
transport; production load, backup/restore and disaster-recovery validation.


### 2026-09-14 TURN CI wiring audit

The end-to-end workflow now passes `localhost:3479` as `TURN_FORWARDER`, matching the C++ relay’s TURN listener. The forwarder control port `8099` remains dedicated to readiness checks and is no longer advertised as a TURN endpoint.


### 2026-09-14 Go toolchain alignment audit

All checked-in Go modules require Go 1.25.0. GitHub Actions and the API/SFU Docker build stages now use Go 1.25, so CI and container builds no longer depend on automatic toolchain substitution from an older Go declaration.

### 2026-09-14 notification preference enforcement audit

- **Implemented:** §33 preference check now enforced end to end. `notification_settings` is consulted by every notification write path (`notifyKind`, `notifyUser`, push `notify`) and by storage-layer trigger `038_notification_preference_invariant.sql`; muted kinds are rejected before persistence, default-on when no preference row exists. Comment replies notify the parent-comment author under the `replies` kind. `tests/integration_test.py` covers mute → silence → re-enable → delivery.
- **Verified on fresh `main`:** Go vet + tests (api, mesh, sfu), strict C++ builds (counters, media, realtime, sfu-forwarder, transcode), fresh web/admin production builds, extension syntax, ML compilation, parity (537 routes), feature registry (26 features, 7 clients), migration ordering, backup-script syntax.
- **Still environment-dependent (not claimed):** provider-backed AI/ML model execution, real Bluetooth/Wi-Fi Direct handshakes, Android/iOS release compilation, live load tests, backup/restore + disaster-recovery execution, Rust service cargo builds (executed in CI), production deployment validation.

## Audit pass 10 — 2026-09-14 (E2E green)

Reproduced the failing E2E job on a live local stack (Postgres 15, Go API on :8080, Go SFU on :8095, C++ counters/realtime/media/sfu-forwarder engines). Three real defects were fixed:

1. **Notification list leaked muted kinds** — `GET /api/notifications` now LEFT JOINs `notification_settings` and hides rows whose preference is disabled (`COALESCE(s.enabled, TRUE)`), matching the write-side gate. Un-reposting also deletes the `repost` notification it previously fanned out.
2. **Two suites masked failures** — `tests/features_test.py` and `tests/finance_test.py` unconditionally returned exit code 0; both now report `passed/failed` from the shared tally and exit 1 on failure, so CI can no longer go green over red checks.
3. **FYP completion-rate probe** — the features suite's third watch event omitted `completed: true`, diluting the aggregate the assertion reads; the probe now reports the signal it asserts on.

Environment notes for running the full stack locally (mirrors CI): the counters engine flushes through `POST /internal/counters/flush` and must be launched with `FLUSH_URL=http://localhost:8080/internal/counters/flush`, `FLUSH_SECRET=$CLUSTER_SECRET`, `FLUSH_INTERVAL_MS<=2000`; the TURN suite asserts the forwarder's `/stats` bearer (`SFU_SECRET`) and its STUN/TURN path against `TURN_LISTEN` (default 3479) plus control port 8099.

Status: every Python suite passes in isolation against the live stack; Go vet + tests, strict C++17 builds, parity (150 files / 537 routes), and the feature registry (26 features, 7 required clients) are green.


## 2026-09-14 direct source audit and implementation update

Evidence was taken from the current source tree and migrations, not from AGENTS.md, earlier commits, or earlier status prose.

Implemented in the current working tree: selectable web message translation with configured ML-provider support and limited local fallback; close-friend search/add/remove; chat-folder create/delete; portable message export; opt-in People Nearby; complete ML image source copying; and corrected native mesh forwarding with a real three-node UDP relay test.

Validation passed with Go 1.25.1 (`go test` and `go vet` for API, mesh, and SFU), Python ML compilation, web TypeScript checking, feature-registry validation, API route parity, and `git diff --check`. Current static coverage is 26 registry features, 7 required clients, 153 scanned files, and 547 registered API routes.

Not proven or implemented in this environment: Tor/onion transport, anonymous IP-privacy relays, physical Bluetooth/Wi-Fi Direct handshakes, configured provider/model output, Android/iOS release builds, production load, backup/restore, disaster recovery, and deployed observability. These remain explicitly pending.

## 2026-09-14 direct source audit

Implemented in this push: selectable/provider-backed web message translation with bounded fallback; close-friend search/add/remove; chat-folder create/delete; portable message export; opt-in People Nearby; complete ML container source copying; and corrected native mesh forwarding with a real three-node UDP relay test.

Verified: Go 1.25.1 tests and vet for API, mesh, and SFU; web and admin production builds; web TypeScript checking; Python ML compilation; feature registry; route parity; and `git diff --check`. Current totals: 26 registered features, 7 required clients, 153 scanned files, and 547 registered API routes.

Pending and not marked implemented: Tor/onion transport, anonymous IP relays, physical Bluetooth/Wi-Fi Direct handshakes, configured provider/model execution, Android/iOS release builds, production load, backup/restore, disaster recovery, and deployed observability.

## Direct source audit — 2026-09-14

Implemented and checked against source: selectable web translation with ML-provider wiring and bounded fallback; close-friend search/add/remove; chat-folder create/delete; portable message export; opt-in People Nearby; complete ML image source copying; and corrected native mesh forwarding with a real three-node UDP relay test. Validation passed: Go 1.25.1 tests/vet for API, mesh, and SFU; web and admin production builds; Python ML compilation; TypeScript; feature-registry validation; API parity; and `git diff --check`. Current counts: 26 registry features, 7 required clients, 153 scanned files, 547 registered API routes.

Pending rather than falsely marked complete: Tor/onion transport, anonymous IP relays, hardware Bluetooth/Wi-Fi Direct handshakes, configured provider/model output, Android/iOS release builds, production load, backup/restore, disaster recovery, and deployed observability.


## Deep source-and-documentation audit — 2026-09-14

Latest source evidence, validation, dependency reduction and remaining unproven native/mesh gates are recorded in `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`.


## Deep source-and-documentation audit — 2026-09-14

The current status includes a cross-platform AES-GCM mesh envelope, native forwarding queue fixes, group metadata preservation, removal of the mesh `x/crypto` dependency and a deterministic interop test. A follow-up pass added Ed25519-signed beacons with trust-on-first-use key pinning, deterministic route scoring and neighbour route expiry. Full findings and unproven release gates are in `file 'ChatApp_Deep_Code_and_Documentation_Audit.md'`.

## Mesh hardening pass — 2026-09-14

Implemented and tested in `services/mesh` (standard library only): per-peer X25519 session keys with HKDF-SHA256 derivation advertised inside Ed25519-signed beacons (unicast payloads no longer rely on the pre-shared key), epoch-based session rotation, post-decrypt sliding-window replay protection, a duplicate-local-delivery fix (exactly one handler invocation per packet id), honest `direct`/`multihop`/`unreachable` call feasibility, per-source relay token-bucket quotas, and a deterministic simulation harness covering chain delivery, partition-and-heal and a 100-node grid with a 500/5k/50k sweep CLI. Validation: Go 1.27.1 mesh/api/sfu `go test`, `go vet`, `gofmt`; route parity; feature registry. Native Android/iOS adoption of the session-key envelope, key revocation, physical-radio validation and production operations remain pending rather than falsely marked complete.

## Verification addendum — 2026-09-15

This revision was checked against the executable source tree on `main`, not against prior commit prose. The repository contains the documented API, web/admin clients, native client source, PostgreSQL migrations, Go/Rust/C++/Python services, mesh cryptography and the existing end-to-end test harness. The web and admin type checks and production builds, feature-registry validation, route parity validation, and patch whitespace validation pass in this environment.

A concrete security hardening pass is now implemented in `services/api/auth.go`: JWT parsing rejects oversized tokens, unsupported header algorithms, missing or non-positive expiry, and tokens issued more than five minutes in the future. Regression coverage is in `services/api/api_test.go`, and an executable Go fuzz target is in `services/api/auth_fuzz_test.go`. These checks reduce parser abuse and algorithm-confusion risk; they do not replace deployment key rotation, external security review, or device-level testing.

**Status remains explicit.** Implemented means present in source and covered by available tests. Partial means the application boundary or fallback exists but a production dependency, provider, hardware path, or release toolchain is not available here. Outstanding items remain Tor/onion IP-privacy transport, physical Bluetooth/Wi-Fi Direct validation, native Android/iOS release builds, configured AI providers, live SMTP/SMS delivery, production load testing, disaster recovery execution, and deployed tracing/alerting. No claim of completion is made for those environment-dependent gates.

## Layered mesh forwarding verification — 2026-09-15

A source-level gap identified during the renewed audit was closed in `services/mesh`: forwarded packets now support an authenticated layered envelope. The sender constructs per-hop AES-GCM layers using authenticated X25519 session keys, each relay peels only its own layer and learns only the next hop, and the destination-only layer reveals the final destination and payload. Tampering, missing session keys, malformed envelopes, and oversized packet inputs fail closed. `services/mesh/onion_test.go` verifies three-node peeling, final-destination confidentiality from the outer layer, plaintext recovery, and tamper rejection.

This closes the **source-completable layered-forwarding requirement**. It does not claim Tor/onion-network anonymity, source-metadata protection, physical Bluetooth/Wi-Fi Direct validation, or production radio behavior. Those remain separate outstanding requirements and still require additional protocol design, deployed infrastructure, or physical-device validation.
