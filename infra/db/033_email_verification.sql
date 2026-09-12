-- 033_email_verification.sql
-- Identity spec: "Email/phone OTP verification is required" before account
-- creation. Adds an email_verifications table mirroring phone_verifications so
-- an email address must complete OTP verification before it can be used to
-- register an account.
CREATE TABLE IF NOT EXISTS email_verifications (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email       TEXT NOT NULL,
  code_hash   TEXT NOT NULL,
  salt        TEXT,
  purpose     TEXT NOT NULL DEFAULT 'register',
  attempts    INT DEFAULT 0,
  expires_at  TIMESTAMPTZ NOT NULL,
  verified_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_email_verif_email ON email_verifications(email);
