-- 008_ttl.sql — disappearing messages (WhatsApp/Telegram parity)
BEGIN;

ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS message_ttl_seconds INT NOT NULL DEFAULT 0;

ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_messages_expires
    ON messages(expires_at) WHERE expires_at IS NOT NULL AND deleted_at IS NULL;

COMMIT;
