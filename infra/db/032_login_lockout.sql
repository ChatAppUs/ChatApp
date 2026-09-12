-- 032_login_lockout.sql
-- Identity spec: "Failed attempts - 5 consecutive failures lock the account for
-- 48 hours." Adds persistent per-account login-failure tracking so a brute-force
-- attempt locks the account for 48 hours rather than only being rate-limited.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS failed_login_attempts INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
