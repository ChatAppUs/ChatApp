-- Server-owned account-deletion verification challenges.
CREATE TABLE IF NOT EXISTS deletion_verification_challenges (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         TEXT NOT NULL CHECK (kind IN ('email', 'phone', 'liveness')),
  code_hash    TEXT NOT NULL,
  salt         TEXT NOT NULL,
  instruction  TEXT NOT NULL DEFAULT '',
  attempts     INT NOT NULL DEFAULT 0,
  verified_at  TIMESTAMPTZ,
  expires_at   TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_deletion_challenges_user
  ON deletion_verification_challenges(user_id, kind, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deletion_challenges_expiry
  ON deletion_verification_challenges(expires_at)
  WHERE verified_at IS NULL;
