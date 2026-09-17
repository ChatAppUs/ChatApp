-- Identity/authentication hardening pass.
-- This migration makes the 48-hour security freeze and trusted-device
-- invalidation database-enforced, so alternate code paths cannot accidentally
-- omit the control. It also stores the complete KYC identity profile required
-- by the identity specification.

BEGIN;

ALTER TABLE kyc_submissions
  ADD COLUMN IF NOT EXISTS first_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS address_line TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS city TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS state_division TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS postal_code TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_kyc_submissions_user_status_created
  ON kyc_submissions(user_id, status, created_at DESC);

-- A security-sensitive identity change must always impose the withdrawal
-- cooldown, even when it is performed by a future endpoint or maintenance
-- path rather than the current HTTP handler.
CREATE OR REPLACE FUNCTION enforce_identity_change_freeze()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.email IS DISTINCT FROM NEW.email
     OR OLD.phone_e164 IS DISTINCT FROM NEW.phone_e164
     OR OLD.password_hash IS DISTINCT FROM NEW.password_hash
     OR OLD.totp_secret IS DISTINCT FROM NEW.totp_secret
     OR OLD.totp_enabled IS DISTINCT FROM NEW.totp_enabled
  THEN
    NEW.withdrawal_freeze_until := GREATEST(
      COALESCE(OLD.withdrawal_freeze_until, now()),
      now() + interval '48 hours'
    );
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_identity_change_freeze ON users;
CREATE TRIGGER trg_identity_change_freeze
BEFORE UPDATE OF email, phone_e164, password_hash, totp_secret, totp_enabled
ON users
FOR EACH ROW
WHEN (
  OLD.email IS DISTINCT FROM NEW.email
  OR OLD.phone_e164 IS DISTINCT FROM NEW.phone_e164
  OR OLD.password_hash IS DISTINCT FROM NEW.password_hash
  OR OLD.totp_secret IS DISTINCT FROM NEW.totp_secret
  OR OLD.totp_enabled IS DISTINCT FROM NEW.totp_enabled
)
EXECUTE FUNCTION enforce_identity_change_freeze();

-- Factor/contact changes invalidate passwordless device trust immediately.
-- Session revocation is deliberately included as well: the identity change
-- must not leave an already-issued authenticated session usable.
CREATE OR REPLACE FUNCTION revoke_sessions_after_identity_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF OLD.email IS DISTINCT FROM NEW.email
     OR OLD.phone_e164 IS DISTINCT FROM NEW.phone_e164
     OR OLD.password_hash IS DISTINCT FROM NEW.password_hash
     OR OLD.totp_secret IS DISTINCT FROM NEW.totp_secret
     OR OLD.totp_enabled IS DISTINCT FROM NEW.totp_enabled
  THEN
    UPDATE sessions
       SET revoked_at = COALESCE(revoked_at, now())
     WHERE user_id = NEW.id AND revoked_at IS NULL;

    UPDATE device_trusts
       SET revoked_at = COALESCE(revoked_at, now())
     WHERE user_id = NEW.id AND revoked_at IS NULL;
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_revoke_sessions_after_identity_change ON users;
CREATE TRIGGER trg_revoke_sessions_after_identity_change
AFTER UPDATE OF email, phone_e164, password_hash, totp_secret, totp_enabled
ON users
FOR EACH ROW
WHEN (
  OLD.email IS DISTINCT FROM NEW.email
  OR OLD.phone_e164 IS DISTINCT FROM NEW.phone_e164
  OR OLD.password_hash IS DISTINCT FROM NEW.password_hash
  OR OLD.totp_secret IS DISTINCT FROM NEW.totp_secret
  OR OLD.totp_enabled IS DISTINCT FROM NEW.totp_enabled
)
EXECUTE FUNCTION revoke_sessions_after_identity_change();

COMMIT;
