# Identity, Authentication & Account Security Specification

> **Scope.** This specification governs the entire identity-and-account-security surface of ChatApp: unified smart authentication; session management; 2FA; passkeys; recovery; KYC; financial-activity gating; and credential-change security. It applies to every client: Android, iOS, Windows, Linux, macOS, Web, Admin, Desktop, and Extension, with identical files, features,and functionality across all platforms.

> **Global requirements.** No demo, no simulation, no stubs, no fake or mock data, no skeletons, no bugs, no broken files, no security vulnerabilities, no bypasses,and no cyber threats are permitted. All systems are fully dynamic, production-ready, scalable, secure,and implemented with complete real business logic. The light/dark theme switch must work on every page of every app.

## Implementation status — audited 2026-09-12

This status is maintained against the source tree on `main`. **Implemented** means that the API, persistence, and client integration are present in the repository. **Implemented with runtime validation pending** means that static code evidence exists but a configured database, external provider, or native toolchain is still required for end-to-end proof. **Not complete** means that the specification requires behavior that is not yet present and must not be represented as shipped.

| Requirement area | Status | Source evidence | Remaining work |
|---|---|---|---|
| Unified email/phone authentication, registration, login, refresh, logout, reset, phone OTP, country catalog | Implemented with runtime validation pending | `services/api/handlers_auth.go`, `services/api/otp.go`, `services/api/data/countries.json`, web login/register/reset pages | Run database-backed flows and verify every native client. |
| Password hashing, JWT, sessions, recovery codes, TOTP 2FA | Implemented with runtime validation pending | Argon2id/JWT helpers, `handlers_security.go`, `handlers_gap9.go`, `services/authn/`, session routes | Run Go/Rust security tests and authn-service delegation tests. |
| Passkeys, Google OAuth, QR login, trusted recovery, app lock, screen time, data export | Implemented with runtime validation pending | `handlers_webauthn.go`, `handlers_oauth.go`, `handlers_qrlogin.go`, `handlers_accounts.go`, `handlers_gap8.go` | Verify provider credentials, WebAuthn ceremonies, and database behavior end to end. |
| KYC submission, ML score threshold, sanctions check, admin review, financial KYC gates | Implemented with runtime validation pending | `handlers_wallet.go`, `handlers_features.go`, `handlers_crypto.go`, `handlers_p2p.go`, `handlers_staking.go`, `handlers_cards.go` | Run configured ML/sanctions/admin review tests. |
| 48-hour withdrawal freeze after 2FA changes | Implemented | `infra/db/026_identity_security_lifecycle.sql`, `handlers_security.go`, `handlers_gap9.go`, `handlers_crypto.go` | Wire the same freeze atomically into email, phone, and password changes when those mutation APIs are added. |
| Email/phone/password credential-change verification with OTP, KYC face match, and five-second liveness | Implemented with runtime validation pending | `handlers_credential_security.go` and `028_credential_change_challenges.sql` add server-owned current/new email or phone OTP challenges, password updates, atomic contact changes, session revocation, and the 48-hour withdrawal freeze. | Run database-backed flows and integrate a dedicated KYC face-match/liveness proof into credential changes before claiming biometric parity. |
| Account deletion with email OTP, phone OTP, liveness, asset confirmation, 30-day cancellation, and permanent deletion | Implemented with runtime validation pending | `handlers_identity_lifecycle.go`, `handlers_deletion_verification.go`, `026_identity_security_lifecycle.sql`, and `027_deletion_verification_challenges.sql` implement password proof, asset confirmation, KYC gate, hashed/expiring email and phone OTPs, ML-provider-backed fresh-selfie face-match/liveness scoring, 30-day scheduling, cancellation-on-login, session revocation, status, and permanent cleanup. | Validate the configured ML service with real KYC media and run the complete database-backed flow. |
| Cross-platform identical feature parity and production operations | Not proven complete | Parity scanner passes route coverage, but native runtime, deployment, load, backup, and disaster-recovery proof is unavailable in this checkout. | Run configured cross-platform, load, security, observability, backup, and restore validation. |

The repository must not claim the full specification is complete until the rows marked **Not complete** and **Not proven complete** have passed their required implementation and runtime validation.

---

## 1. Non-Negotiable Platform Rules

1. **Feature parity** - All apps must ship the same files,the same features,and the same functionality,with no missing,incomplete,duplicated,disconnected,or broken pieces across frontend,backend,database,blockchain,or infrastructure layers.



2. **Language allocation** - Super-first-speed,ultra-low-latency data planes run in **C++**;security/safety-critical, easily maintainable components run in **Rust**;high-load, world-wide-distributed control planes run in **Go**.



3. **No placeholders** - Every flow described here must be implemented end-to-end with real business logic,everywhere.



4. **Verify end-to-end** - Each authentication-and-identity flow must be reviewed across frontend,backend,database,blockchain,and infrastructure layers for missing,incomplete,duplicated,disconnected,or broken components.



##  .2. Unified Smart Authentication Input

All authentication-and-identity-related forms must use **one unified input field** - no toggle,no switch,no dropdown mode selector. This applies to: **Login, Register, Reset Password, 2FA Reset, Email/Phone Change, Account Deletion,and Recovery flows**.



###2.1 Real-Time Auto-Detection

The single input field auto-detects the identifier type in real time, exactly as the user types:

| Mode | Trigger | Behavior |
|---|---|---|
| **Phone mode** | Numeric input detected | Country flag selector plus international dial code;supports 200+ countries;searchable dropdown;region-based auto-formatting;real-time phone-structure validation |
| **Email mode** | Alphabet or email pattern detected | Hides the country selector and enables RFC-style email validation engine |

###2.2 Behavior Rules

- Instant switching without page refresh(no flicker,no reload.
- Supports paste,autofill,and autocomplete detection.
- Backspace dynamically recalculates the mode.
- Seamless transition without losing user input.
- The backend receives an explicit `type` flag (`email`/`phone`)from the client.
- During transition,never destroy,mask,or reset the field's value.

##3. Login

- **Unified smart input**(email/phone auto-detection.
- **Account-existence check in real time**;if the account is not registered,automatically redirect to **Sign Up**.
- **Continue button** functional.
- **Password field** with visibility toggle(**eye emoji**).
- **Failed attempts** - 5 consecutive failures lock the account for **48 hours**.
- **Remember Me** - persistent `localStorage` session.
- **Submit button** functional.

###3.1 Flow

1.**Email OTP** - 6-digit code.
2.**Phone OTP** - 6-digit code.
3.**Conditional 2FA** - required only when enabled.
4.**Skip 2FA** when disabled.
5.**Trusted-device login** - 30-day passwordless option for verified devices.
6.**Login button** functional.
7.**Redirect** to User Home.

###3.2 Features

- **Forgot Password** flow.
- **Social login:**Google OAuth,Apple OAuth,etc. Social login still enforces email verification,phone verification,and 2FA verification when enabled.
- **Login button** functional;redirects home.
- **Passkey authentication**(WebAuthn.
- **Loading spinner**.
- **Error/success messages**.
- **Back to home** link.

##4. Register

- **Smart email/phone detection** via the unified input.
- **Duplicate-account prevention check**.
- **Continue button** functional - proceeds to OTP verification.
- **Email/phone OTP verification is required**.
- **Continue button** functional - proceeds to password setup.

###4.1 Password Rules

- Minimum **8 characters**.
- **Strength indicator:**
  - Red **Weak**
  - Yellow **Medium**
  - Green **Strong**
- **Confirm password** validation.
- **Referral code** optional.
- **Terms & Conditions** required.
- **Sign Up button** functional;on success,redirect home.

###4.2 Auth Options

- **Google / Apple OAuth**,etc.
- Once done,redirect home.
- **Passkey authentication**.

###4.3 UI

- **Loading spinner**.
- **Success or error messages**.
- **Back home** link.

##5. Reset Password

- **Unified email/phone smart-input detection**.
- **Account-existence check**;if not  registered,redirect to **Sign Up**;otherwise **Continue button** functional.
- **Next - verification:**
  - **Email OTP** - 6-digit code.
  - **Phone OTP** - 6-digit code.
  - **2FA verification** - only when enabled.
  - **2FA recovery flow** - when the authenticator is lost.
- **Redirect to Home** after success.

##6. 2FA Reset System

- **Unified email/phone smart detection**.
- **Existence check** - if not  registered,redirect to Signup.
- **OTP verification** - email OTP plus phone OTP.
- **Live verification:**
  - **KYC face-match validation**.
  - **Live liveness detection** - random instructions.
  - **Auto verification within 5 seconds**.
- **Remove old 2FA automatically** once identity is proven.
- **Allow new 2FA setup**.
- **Redirect to Home**.

##7. KYC Verification

**Mandatory before any financial access.**

###7.1 Verification Required

- **Email verification**.
- **Phone verification**.
- **User data:**
  - First name,Last name,Title,Address,City,State/Division,Postal code,Country.

###7.2 Documents (NID / Passport / Driving Licence)

- Front ID.
- Back ID.
- Selfie with document.

###7.3 Live Verification

- **Liveness engine** - random instruction-based.
- **Progress meter**.
- **Auto-submit**.
- **Admin review system**.
- **Status tracking** for users.

###7.4 Rules

- **No withdrawals without approved KYC**.

##8. Security - Credential & Account Change

**Any change to Email,Phone,Password,or 2FA triggers:**

- **48-hour withdrawal freeze** - auto re-enabled after the cooldown.
- **Smart detection input**.
- **Verify current email or phone**.
- **Verify new email or phone**.
- **OTP verification**.
- **KYC-linked face verification**.
- **5-second liveness confirmation**.
- **Global update propagation** - change applies system-wide to every client.

##9. Account Deletion

- **Verification required:**Email OTP,Phone OTP,Liveness verification,and the **"I have withdrawn all assets"** checkbox.
- **30-day pending deletion** grace period.
- **Login during the grace period cancels deletion**.
- **Permanent deletion after 30 days**.
- **No recovery after final deletion**.
- **Auto logout after request**.

##10. Phone Input Support

All phone inputs must support:

- **Country flags**.
- **Country codes**.
- **200+ countries**.

---

## Appendix A - Targeted API Reference(verified against `services/api/main.go`)

The following endpoints implement(or anchor)the surface described in sections 2-9,so implementers can ground every spec item in thereal code:

- `POST /api/auth/register` - unified email/phone registration.
- `POST /api/auth/login` - identifier-based login(username,email or phone),password,optional `totp_code`,2FA-and-recovery-code unlock.
- `POST /api/auth/refresh` - refresh-token rotation with per-device session rows.
- `POST /api/auth/logout` - revokes the session.
- `POST /api/auth/forgot-password` - unified smart-input resolution(username,email-or-phone)without existence leaks;email reset link(1-hour token.
- `POST /api/auth/reset-password` - one-time token,password policy re-check,all-session revocation on change.
- `POST /api/auth/phone/send-code` `POST /api/auth/phone/check-code` - throttled 6-digit phone OTP engine.
- `POST /api/auth/google` - Google OAuth complete login flow.
- `POST /api/auth/passkey/login/begin` `finish` - passkey sign-in.
- `POST /api/auth/passkey/register/begin` `finish` - passkey enrollment;`GET /api/auth/passkeys`,`DELETE /api/auth/passkeys/{id}`.
- `POST /api/auth/qr/new`,`GET /api/auth/qr/{token}`,`POST /api/auth/qr/{token}/approve|reject` - QR-code login pairing.
- `POST /api/auth/2fa/setup|enable|disable` - RFC 6238 TOTP lifecycle;recovery codes minted on enable.
- `POST /api/auth/2fa/recovery-codes/generate` - mints 8 one-time scratch codes(SHA-256 stored;shown exactly once.
- `GET /api/auth/2fa/recovery-codes` - remaining unused count.
- `POST /api/auth/2fa/recovery-codes/redeem` - two-leg redeem;claim-token leg 1;consumed at login leg  .2.
- `POST /api/auth/2fa/recovery-codes/disable` - disables 2FA via recovery codes.
- `POST /api/auth/verify-password` - re-auth password proof for sensitive flows(app lock,credential changes.
- `GET /api/me/sessions` `DELETE /api/me/sessions/{id}` - device list;remote revoke.
- `POST /api/kyc/submit` `GET /api/kyc/status` - document-plus-selfie submission;ML-scored verification;auto-verify at score >=  .0.75 where sanctions-clean;admin review fallback.
- `GET /api/countries` - country flag/dial-code catalog for the unified input.
- `POST /api/recovery/trusted/request|reveal|redeem`;`GET /api/me/trusted-contacts`,etc - trusted-contact account recovery.
- `POST /api/me/export` - full user-data export(GDPR-style,useful before deletion.
- `PUT /api/me/screen-time` `POST /api/me/app-lock` - app-lock/screen-time auth-adjacent self-service controls.

## Appendix B - Security Invariants

1.**Passwords** are never stored plaintext-or-reversible;hashing/verification delegates to the **Rust AuthN** service(Argon2id.
2.**JWT** mint/verify,TOTP(RFC 6238,6-digit OTP engine,CSPRNG,and HMAC live in the **Rust AuthN** service.
3.**Authorization** is always server-side;never trust client-supplied roles or flags.
4.**Sessions** carry identifier,user ID,creation time,expiration,revocation state,and device metadata;all sessions listableand revocable per device.
5.**Recovery codes**and reset tokens are stored hashed(SHA-256;only one plaintext exposure.
6.**Credential or contact changes** revoke auth sessionsand freeze withdrawals for 48 hours(config-driven cooldown.
7.**KYC** gates any financial surface:payouts,P2P,cards,staking,convert,and withdrawals - enforced server-side(see `handlers_wallet.go`,`handlers_crypto.go`,`handlers_p2p.go`,`handlers_staking.go`,`handlers_cards.go`,`handlers_features.go`).
8.**Verification codes**,reset links,and recovery codes expire-or-throttle;no existence leaks in error responses.
