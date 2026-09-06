-- Gap pack 9 (2026-09-05): closes the remaining depth gaps across all
-- five competitor matrices:
--   * notification-settings matrix (Telegram) — per-kind per-user mute switches
--   * 2FA one-time backup / recovery codes (Telegram)
--   * unsend-for-everyone window + message edit history (Telegram)
--   * story archive + "On this day" memories (Facebook)
--   * TikTok-grade reel studio: drafts, effects, voiceovers, captions
--   * creator insights rollup (TikTok / X impressions, reach, follower-growth, top-sounds
--   * share deep-links + OG share-cards (X / TikTok viral loop
--   * call reactions / raise-hand / ratings + scheduled calls (imo)
--   * page insights rollup (Facebook)
--   * group post-approval moderation queue (Facebook)
--   * E2EE payload audit: body_enc holds opaque ciphertext (server never sees plaintext

BEGIN;

-- ---- E2EE payload column: opaque ciphertext only (Telegram Secret Chats) ----
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS body_enc TEXT DEFAULT '',          -- AES-GCM armored envelope
  ADD COLUMN IF NOT EXISTS unsend_at TIMESTAMPTZ,            -- Telegram-style "unsend for everyone" (48h window
  ADD COLUMN IF NOT EXISTS sender_del_at TIMESTAMPTZ;        -- local delete-for-me mark for the sender

CREATE INDEX IF NOT EXISTS idx_messages_unsend ON messages(conversation_id, unsend_at DESC) WHERE unsend_at IS NOT NULL;

-- ---- Message edit history (Telegram "Edited" badge + viewer) ----
CREATE TABLE IF NOT EXISTS message_edits (
  id         BIGSERIAL PRIMARY KEY,
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  edited_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_message_edits_msg ON message_edits(message_id, edited_at DESC);

-- ---- Notification settings matrix (Telegram: per-kind per-chat on/off) ----
CREATE TABLE IF NOT EXISTS notification_settings (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,
  enabled     BOOLEAN NOT NULL DEFAULT TRUE,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, kind)
);
-- kinds: messages, groups, calls, live, gifts, replies, mentions, reposts,
--         sounds, stories, marketplace, withdrawals, deposits, kyc, system
INSERT INTO notification_settings(user_id, kind) SELECT id, 'system' FROM users
  ON CONFLICT DO NOTHING;

-- ---- 2FA one-time recovery codes (stored hashed, shown once) ----
CREATE TABLE IF NOT EXISTS recovery_codes (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  TEXT NOT NULL,                    -- hex(SHA-256(code)) — plaintext never persisted
  used_at    TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, code_hash)
);
CREATE INDEX IF NOT EXISTS idx_recovery_codes_user ON recovery_codes(user_id, created_at DESC);

-- ---- Story archive + Memories ("On this day", Facebook parity) ----
CREATE TABLE IF NOT EXISTS story_archives (
  story_id    UUID PRIMARY KEY REFERENCES posts(id) ON DELETE CASCADE,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  archived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_story_archives_user ON story_archives(user_id, archived_at DESC);

CREATE TABLE IF NOT EXISTS memories (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id  UUID REFERENCES posts(id) ON DELETE CASCADE, -- original post/story
  kind       TEXT NOT NULL DEFAULT 'memory' CHECK (kind IN ('memory','story_archive','highlight')),
  note       TEXT NOT NULL DEFAULT '',
  on_date    DATE NOT NULL,                -- the month-day the memory surfaces on
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, source_id, kind)
);
CREATE INDEX IF NOT EXISTS idx_memories_user_date ON memories(user_id, on_date DESC);

-- ---- TikTok-grade reel studio: drafts with full edit state ----
CREATE TABLE IF NOT EXISTS reel_drafts (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title        TEXT NOT NULL DEFAULT '',
  media_urls   JSONB NOT NULL DEFAULT '[]',      -- ordered multi-clip timeline
  effects      JSONB NOT NULL DEFAULT '{}',      -- {filter, beauty, ar, green_screen}
  voiceover_url TEXT NOT NULL DEFAULT '',
  caption      TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reel_drafts_user ON reel_drafts(user_id, updated_at DESC);

-- ---- Generated captions (ML /captions results cached per media) ----
CREATE TABLE IF NOT EXISTS reel_captions (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  media_url  TEXT NOT NULL,
  text       TEXT NOT NULL,
  lang       TEXT NOT NULL DEFAULT 'en',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, media_url)
);
CREATE INDEX IF NOT EXISTS idx_reel_captions_user ON reel_captions(user_id);

-- ---- Creator insights daily rollup (TikTok / X analytics) ----
CREATE TABLE IF NOT EXISTS creator_insights (
  user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day            DATE NOT NULL,
  reach          BIGINT NOT NULL DEFAULT 0,
  impressions    BIGINT NOT NULL DEFAULT 0,
  watch_time_s   BIGINT NOT NULL DEFAULT 0,
  new_followers  INT NOT NULL DEFAULT 0,
  top_sound      TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, day)
);
CREATE INDEX IF NOT EXISTS idx_creator_insights_user ON creator_insights(user_id, day DESC);

-- ---- Page insights daily rollup (Facebook Page analytics) ----
CREATE TABLE IF NOT EXISTS page_insights (
  page_id       UUID NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  day           DATE NOT NULL,
  impressions    BIGINT NOT NULL DEFAULT 0,
  reach          BIGINT NOT NULL DEFAULT 0,
  new_followers  INT NOT NULL DEFAULT 0,
  PRIMARY KEY (page_id, day)
);
CREATE INDEX IF NOT EXISTS idx_page_insights_page ON page_insights(page_id, day DESC);

-- ---- Share deep-link tokens (viral loop: public /share/<token> landing page + OG card) ----
CREATE TABLE IF NOT EXISTS share_tokens (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  token       TEXT NOT NULL UNIQUE,             -- 128-bit CSPRNG
  post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  creator_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_share_tokens_post ON share_tokens(post_id);

-- ---- In-call engagement: reactions bursts, raise-hand, ratings ----
CREATE TABLE IF NOT EXISTS call_events (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id    TEXT NOT NULL,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('reaction','raise_hand','clap','boo','applause','pinned')),
  payload    TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_call_events_room ON call_events(room_id, created_at DESC);

CREATE TABLE IF NOT EXISTS call_ratings (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id    TEXT NOT NULL,
  rater_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  rating      INT NOT NULL CHECK (rating BETWEEN 1 AND 5),
  comment    TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (room_id, rater_id)
);

-- ---- Scheduled calls + reminders (imo: "call you at…", scheduled-call invite) ----
CREATE TABLE IF NOT EXISTS scheduled_calls (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title           TEXT NOT NULL,
  conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
  scheduled_at    TIMESTAMPTZ NOT NULL,
  is_reminder     BOOLEAN NOT NULL DEFAULT FALSE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_scheduled_calls_user ON scheduled_calls(user_id, scheduled_at);

-- ---- Group post-approval moderation queue (Facebook Groups: pending posts need admin OK) ----
ALTER TABLE content_groups ADD COLUMN IF NOT EXISTS require_post_approval BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS group_post_queue (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id   UUID NOT NULL REFERENCES content_groups(id) ON DELETE CASCADE,
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
  decided_by UUID REFERENCES users(id),
  decided_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_group_queue_group ON group_post_queue(group_id, status, created_at DESC);

COMMIT;