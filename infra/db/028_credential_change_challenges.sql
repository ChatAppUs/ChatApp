CREATE TABLE IF NOT EXISTS credential_change_challenges (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL CHECK (kind IN ('current_email','new_email','current_phone','new_phone')),
  destination TEXT NOT NULL,
  code_hash   TEXT NOT NULL,
  salt        TEXT NOT NULL,
  attempts    INT NOT NULL DEFAULT 0,
  verified_at TIMESTAMPTZ,
  expires_at  TIMESTAMPTZ NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_credential_challenges_lookup
  ON credential_change_challenges(user_id, kind, created_at DESC);
