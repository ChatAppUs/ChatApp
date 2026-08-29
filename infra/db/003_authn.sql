-- Migration 003: federated identity (Google OAuth), WebAuthn/passkeys, QR login.

-- Linked third-party identity providers (Google today; Apple/GitHub slots ready).
CREATE TABLE IF NOT EXISTS oauth_accounts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider     TEXT NOT NULL,                -- 'google'
    provider_sub TEXT NOT NULL,                -- stable subject id from provider
    email        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_sub)
);
CREATE INDEX IF NOT EXISTS idx_oauth_user ON oauth_accounts(user_id);

-- WebAuthn/passkey credentials. Public key stored in COSE (CBOR) form.
CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id BYTEA NOT NULL UNIQUE,       -- raw credential id from authenticator
    public_key   BYTEA NOT NULL,               -- COSE-encoded public key
    sign_count   BIGINT NOT NULL DEFAULT 0,
    transports   TEXT[] NOT NULL DEFAULT '{}', -- usb/nfc/ble/internal (platform = biometric)
    name         TEXT NOT NULL DEFAULT 'Passkey',
    aaguid       BYTEA,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_webauthn_user ON webauthn_credentials(user_id);

-- Short-lived ceremony challenges (registration + authentication).
CREATE TABLE IF NOT EXISTS webauthn_challenges (
    challenge  TEXT PRIMARY KEY,               -- base64url random 32 bytes
    user_id    UUID REFERENCES users(id) ON DELETE CASCADE, -- null for username-less login
    kind       TEXT NOT NULL,                  -- 'register' | 'login'
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_webauthn_chal_exp ON webauthn_challenges(expires_at);

-- Telegram-style QR login: new device shows QR containing the token;
-- an already-authenticated device scans it and approves.
CREATE TABLE IF NOT EXISTS qr_login_tokens (
    token       TEXT PRIMARY KEY,              -- base64url random 32 bytes
    status      TEXT NOT NULL DEFAULT 'pending', -- pending | approved | consumed | expired
    user_id     UUID REFERENCES users(id) ON DELETE CASCADE,
    created_ip  INET,
    approved_ip INET,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_qr_login_exp ON qr_login_tokens(expires_at);

-- Periodic sweeper for expired challenges/tokens (called from app too).
CREATE OR REPLACE FUNCTION purge_expired_authn() RETURNS void AS $$
BEGIN
    DELETE FROM webauthn_challenges WHERE expires_at < now();
    DELETE FROM qr_login_tokens WHERE expires_at < now() AND status = 'pending';
END;
$$ LANGUAGE plpgsql;
