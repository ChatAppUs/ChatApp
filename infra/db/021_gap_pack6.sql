-- Gap-pack 6 (2026-08-31): closes the remaining server-side gaps from the
-- five competitor inventories (facebook.md, x-twitter.md, telegram.md,
-- tiktok.md, imo.md):
--   * voice-message waveforms (Telegram)
--   * long-form articles (X Premium)
--   * custom audience lists on posts (Facebook)
--   * bio links (TikTok)
--   * custom emoji catalog + reactions (Telegram)
--   * message translation cache (Telegram/imo)
--   * discoverable live rooms with viewer/peak/like tracking (TikTok Live)

-- Voice message waveform: client-computed peak buckets (0-100), rendered as
-- the Telegram-style waveform in the inline player.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS waveform jsonb NOT NULL DEFAULT '[]';

-- Long-form articles (X Premium articles): jsonb
-- {title, subtitle, body, cover_url}; body up to 100k chars.
ALTER TABLE posts ADD COLUMN IF NOT EXISTS article jsonb;

-- Custom audience lists (Facebook custom audiences): visibility='list'
-- restricts the post to members of this list.
ALTER TABLE posts ADD COLUMN IF NOT EXISTS audience_list_id uuid REFERENCES user_lists(id) ON DELETE SET NULL;
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_visibility_check;
ALTER TABLE posts ADD CONSTRAINT posts_visibility_check
  CHECK (visibility = ANY (ARRAY['public','followers','private','close_friends','list']));

-- Bio links (TikTok bio links): jsonb array of {title, url}, max 5.
ALTER TABLE users ADD COLUMN IF NOT EXISTS bio_links jsonb NOT NULL DEFAULT '[]';

-- Custom emoji (Telegram custom emoji): uploaded images usable as message
-- reactions via the :name: shortcode.
CREATE TABLE IF NOT EXISTS custom_emoji (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       text NOT NULL UNIQUE CHECK (name ~ '^[a-z0-9_]{2,32}$'),
  media_url  text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Translation cache (Telegram/imo message translation): engine output is
-- cached per message+lang so repeat taps are free.
CREATE TABLE IF NOT EXISTS message_translations (
  message_id      uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  lang            text NOT NULL,
  translated_text text NOT NULL,
  engine          text NOT NULL DEFAULT 'lexicon-v1',
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (message_id, lang)
);

-- Live rooms (TikTok Live / Facebook Rooms): a persistent, discoverable
-- drop-in room. Media runs on the self-built SFU (room id live-<id>);
-- live chat is a bound group conversation so the existing WS machinery
-- (messages, gifts, moderation) applies unchanged.
CREATE TABLE IF NOT EXISTS live_rooms (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title           text NOT NULL,
  category        text NOT NULL DEFAULT '',
  status          text NOT NULL DEFAULT 'live' CHECK (status IN ('live','ended')),
  conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
  like_count      bigint NOT NULL DEFAULT 0,
  peak_viewers    int NOT NULL DEFAULT 0,
  created_at      timestamptz NOT NULL DEFAULT now(),
  ended_at        timestamptz
);
CREATE INDEX IF NOT EXISTS live_rooms_live_idx ON live_rooms(created_at DESC) WHERE status='live';

CREATE TABLE IF NOT EXISTS live_room_viewers (
  room_id   uuid NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  joined_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (room_id, user_id)
);

-- Ad revenue-share accounting lives in 020 (ad_events.placement_post_id +
-- 55% treasury share) — intentionally not duplicated here.
