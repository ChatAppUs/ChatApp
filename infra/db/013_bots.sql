-- 013_bots.sql — Telegram-style bot accounts, bot API (long-poll + webhook)
-- and mini-apps. Bots are regular user rows flagged via the bots table; the
-- bot token authenticates against /api/bot/* routes instead of a JWT.
BEGIN;

CREATE TABLE IF NOT EXISTS bots (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE, -- the bot's account
  owner_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,        -- the human who owns it
  token_hash   TEXT NOT NULL UNIQUE,    -- SHA-256 of the bot token (never stored raw)
  description  TEXT DEFAULT '',
  webhook_url  TEXT DEFAULT '',         -- empty => long-poll mode
  webhook_secret TEXT DEFAULT '',       -- HMAC secret for webhook deliveries
  mini_app_url TEXT DEFAULT '',         -- mini-app entry point (mini-apps platform)
  active       BOOLEAN NOT NULL DEFAULT TRUE,
  created_at   TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bots_owner ON bots(owner_id);

-- Durable update inbox per bot. message.created triggers enqueue here for
-- every bot that is a member of the conversation; the bot drains via
-- getUpdates (long-poll, offset = last id) or we POST to its webhook.
CREATE TABLE IF NOT EXISTS bot_updates (
  id         BIGSERIAL PRIMARY KEY,
  bot_id     UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,             -- message, callback, member_joined
  payload    JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bot_updates_bot ON bot_updates(bot_id, id);

-- Bot-owned mini apps surfaced in chats (name/url/icon), Telegram Web Apps style.
CREATE TABLE IF NOT EXISTS mini_apps (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bot_id     UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  url        TEXT NOT NULL,
  icon_url   TEXT DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (bot_id, title)
);

COMMIT;
