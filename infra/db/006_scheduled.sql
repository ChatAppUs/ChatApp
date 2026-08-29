-- 006_scheduled.sql — scheduled messages (Telegram-style "send later")
BEGIN;

CREATE TABLE IF NOT EXISTS scheduled_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body            TEXT NOT NULL,
    media_url       TEXT DEFAULT '',
    send_at         TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_scheduled_due
    ON scheduled_messages (send_at) WHERE sent_at IS NULL;

COMMIT;
