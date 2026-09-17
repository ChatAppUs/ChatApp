-- Identity & Account Security spec gaps: trusted-device login, KYC document
-- back side + random-instruction liveness, and the dedicated 2FA reset flow.

-- §3.1 item 5: 30-day passwordless login for verified devices. The raw token
-- lives only on the device; only its SHA-256 hash is stored here.
CREATE TABLE IF NOT EXISTS device_trusts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    device_name TEXT NOT NULL DEFAULT '',
    ip          INET,
    user_agent  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_device_trusts_user ON device_trusts(user_id);

-- §7.2: documents are captured as front AND back side of the ID.
ALTER TABLE kyc_submissions ADD COLUMN IF NOT EXISTS doc_back_url TEXT NOT NULL DEFAULT '';

-- §7.3: random instruction-based liveness. A challenge is minted before the
-- selfie capture; a submission must reference a fresh, unused challenge.
CREATE TABLE IF NOT EXISTS kyc_liveness_challenges (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    instruction TEXT NOT NULL,
    nonce       TEXT NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kyc_liveness_user ON kyc_liveness_challenges(user_id);

-- §6: dedicated 2FA reset flow. OTP challenges go to BOTH the account email
-- and the account phone; face-match liveness is attested separately.
CREATE TABLE IF NOT EXISTS twofa_reset_challenges (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('email','phone')),
    destination TEXT NOT NULL,
    code_hash   TEXT NOT NULL,
    salt        TEXT NOT NULL DEFAULT '',
    attempts    INT NOT NULL DEFAULT 0,
    verified_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_twofa_reset_user ON twofa_reset_challenges(user_id, kind);

ALTER TABLE kyc_submissions ADD COLUMN IF NOT EXISTS liveness_challenge_id UUID REFERENCES kyc_liveness_challenges(id);
