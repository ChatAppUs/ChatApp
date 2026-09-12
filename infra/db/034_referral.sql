-- 034_referral.sql
-- Identity spec §4.1: "Referral code optional" during registration.
-- Adds a referral_code column to users and a referral_credits ledger hook so a
-- referred signup can be attributed to the referrer without breaking the
-- double-entry ledger invariant.
ALTER TABLE users ADD COLUMN IF NOT EXISTS referral_code TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS referred_by UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_users_referral_code ON users(referral_code);
