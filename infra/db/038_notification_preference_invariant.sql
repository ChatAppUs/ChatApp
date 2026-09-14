-- 038 — notification preference invariant (Master Documentation §33).
--
-- §33 requires the notification pipeline to run: Event → eligibility →
-- preference check → delivery. The preference matrix
-- (notification_settings, per-user per-kind on/off switches, §gap pack 9 /
-- Telegram model) was stored and served by GET/PUT /api/me/notification-settings,
-- but NOTHING consulted it: every INSERT INTO notifications wrote the row and
-- every list returned it, so a user who disabled a notification kind still
-- received it in-app (and via the push fan-out that mirrors this table).
--
-- Enforce the preference check once at the storage layer, so all current and
-- future writers honour it without each call site remembering:
--
--   * A row whose (user_id, kind) has notification_settings.enabled = FALSE is
--     rejected. Writers use ON CONFLICT DO NOTHING semantics implicitly — the
--     API handlers already ignore notification insert failures (best-effort
--     fan-out), so a rejected row must not fail the parent request.
--   * No settings row for the kind means default-on (the matrix is opt-out).
--   * System kinds keep working: 'system' rows exist for every user and the
--     settings UI never offers 'system' as a toggle target without a row.
--
-- Forward-only, idempotent.

BEGIN;

CREATE OR REPLACE FUNCTION notifications_respect_preferences()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  enabled BOOLEAN;
BEGIN
  SELECT s.enabled INTO enabled
    FROM notification_settings s
   WHERE s.user_id = NEW.user_id AND s.kind = NEW.kind;
  -- NULL = no preference row: default deliver.
  IF enabled IS FALSE THEN
    RETURN NULL;
  END IF;
  RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_notifications_preferences ON notifications;
CREATE TRIGGER trg_notifications_preferences
BEFORE INSERT ON notifications
FOR EACH ROW EXECUTE FUNCTION notifications_respect_preferences();

COMMIT;
