-- 000048_anonymous_md_features.up.sql
-- Anonymous.md privacy and security feature tables.

-- Panic codes for emergency account actions
CREATE TABLE IF NOT EXISTS panic_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_panic_codes_user ON panic_codes(user_id);

-- Anonymous addresses mapped to real users
CREATE TABLE IF NOT EXISTS anonymous_addresses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    address_type TEXT NOT NULL DEFAULT 'onion',
    address TEXT NOT NULL UNIQUE,
    public_key TEXT NOT NULL,
    pairwise_id TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_anonymous_user ON anonymous_addresses(user_id);
CREATE INDEX IF NOT EXISTS idx_anonymous_map ON anonymous_addresses(address, created_at);

-- Incognito mode sessions
CREATE TABLE IF NOT EXISTS incognito_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_token TEXT NOT NULL UNIQUE,
    ephemeral_pubkey TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '24 hours'),
    active BOOLEAN NOT NULL DEFAULT true
);
CREATE INDEX IF NOT EXISTS idx_incognito_user ON incognito_sessions(user_id);

-- Single-use invite links
CREATE TABLE IF NOT EXISTS single_use_invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    inviter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code TEXT NOT NULL UNIQUE,
    max_uses INT NOT NULL DEFAULT 1,
    use_count INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked BOOLEAN NOT NULL DEFAULT false
);

-- Transport isolation configuration per user
CREATE TABLE IF NOT EXISTS transport_isolation_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE UNIQUE,
    tor_enabled BOOLEAN NOT NULL DEFAULT true,
    bridge_type TEXT,
    bridge_config JSONB,
    custom_relay TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Censorship resistance bridge registries
CREATE TABLE IF NOT EXISTS censorship_domains (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain TEXT NOT NULL UNIQUE,
    is_frontable BOOLEAN NOT NULL DEFAULT false,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS censorship_bridges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bridge_type TEXT NOT NULL,
    address TEXT NOT NULL,
    fingerprint TEXT,
    capacity INT NOT NULL DEFAULT 100,
    region TEXT,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    active BOOLEAN NOT NULL DEFAULT true
);

-- Contact verification safety numbers (SAF)
CREATE TABLE IF NOT EXISTS contact_verification_saf (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    safety_number TEXT NOT NULL,
    verified_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(user_id, contact_user_id)
);

-- Multi-device sync sessions
CREATE TABLE IF NOT EXISTS multi_device_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL,
    device_name TEXT,
    platform TEXT,
    public_key TEXT NOT NULL,
    session_token TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked BOOLEAN NOT NULL DEFAULT false,
    UNIQUE(user_id, device_id)
);

-- Security events audit log
CREATE TABLE IF NOT EXISTS security_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID,
    event_type TEXT NOT NULL,
    detail TEXT,
    ip_address INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_emergency_user ON security_events(user_id, created_at);