-- Identity and account-security lifecycle state.
-- Sensitive credential/contact/2FA changes set withdrawal_freeze_until to now()+48h.
-- Account deletion flows may use the pending/scheduled timestamps without
-- destroying the account before the documented 30-day grace period ends.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS withdrawal_freeze_until TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deletion_requested_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deletion_scheduled_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_withdrawal_freeze
  ON users(withdrawal_freeze_until)
  WHERE withdrawal_freeze_until IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_deletion_schedule
  ON users(deletion_scheduled_at)
  WHERE deletion_scheduled_at IS NOT NULL;
