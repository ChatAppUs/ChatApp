-- 037_message_ttl_invariant.sql
--
-- Disappearing messages (Master Plan / Master Documentation: per-conversation
-- message TTL, set via PUT /api/conversations/{id}/ttl) were only honoured by
-- ONE of the ten `INSERT INTO messages` call sites — the main chat write path
-- (persistMessage). Every other writer created rows with expires_at = NULL, so
-- those messages became permanent even though the conversation had a TTL:
--
--   handlers_chat.go:270    main send path            (honoured TTL)
--   handlers_chat2.go:134   forward a message         (did NOT)
--   handlers_chat5.go:66    location share            (did NOT)
--   handlers_chat5.go:325   media/attachment send     (did NOT)
--   handlers_chat5.go:511   payment message           (did NOT)
--   handlers_gap3.go:356    pin/system notice         (did NOT)
--   handlers_gap4.go:715    system + entities         (did NOT)
--   handlers_gap4.go:751    media                     (did NOT)
--   handlers_scheduled.go   scheduled send            (did NOT)
--   handlers_social2.go:253 story share in DM         (did NOT)
--
-- A message that every member believes will disappear at the TTL instead stays
-- readable forever — a privacy-correctness bug, not a cosmetic one.
--
-- Fixing ten call sites leaves the invariant one future handler away from
-- breaking again, so it is enforced once at the storage layer instead: any row
-- inserted into `messages` while its conversation has message_ttl_seconds > 0
-- and no explicit expires_at is stamped automatically.
--
-- Semantics:
--   * An explicitly supplied expires_at (including one already in the past, as
--     the TTL-expiry test injects) is LEFT ALONE — callers keep full control.
--   * A conversation with message_ttl_seconds = 0 (the default) is untouched,
--     so this cannot make existing permanent messages disappear.
--
-- Forward-only, idempotent.

BEGIN;

CREATE OR REPLACE FUNCTION messages_apply_conversation_ttl()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    conv_ttl integer;
BEGIN
    IF NEW.expires_at IS NOT NULL THEN
        RETURN NEW;  -- caller decided explicitly; never override
    END IF;

    SELECT c.message_ttl_seconds INTO conv_ttl
      FROM conversations c
     WHERE c.id = NEW.conversation_id;

    IF conv_ttl IS NOT NULL AND conv_ttl > 0 THEN
        NEW.expires_at := now() + make_interval(secs => conv_ttl);
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_messages_conversation_ttl ON messages;

CREATE TRIGGER trg_messages_conversation_ttl
    BEFORE INSERT ON messages
    FOR EACH ROW
    EXECUTE FUNCTION messages_apply_conversation_ttl();

-- Backfill: messages already written into a TTL conversation before this
-- migration never got an expiry. Stamp them from their own created_at so a
-- long-lived row expires immediately and a recent one expires on schedule,
-- matching what persistMessage would have produced.
UPDATE messages m
   SET expires_at = m.created_at + make_interval(secs => c.message_ttl_seconds)
  FROM conversations c
 WHERE c.id = m.conversation_id
   AND m.expires_at IS NULL
   AND c.message_ttl_seconds > 0;

COMMIT;
