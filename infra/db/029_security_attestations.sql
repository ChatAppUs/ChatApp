CREATE TABLE IF NOT EXISTS security_attestations (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose     TEXT NOT NULL CHECK (purpose IN ('credential_change')),
  score       NUMERIC(5,4) NOT NULL,
  checks      JSONB NOT NULL DEFAULT '{}',
  verified_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at  TIMESTAMPTZ NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_security_attestations_user
  ON security_attestations(user_id, purpose, verified_at DESC);
