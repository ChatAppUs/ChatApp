-- Gap pack 8 (2026-08-31): TikTok-style duet/stitch reel remixes.
-- remix_mode qualifies a remix (posts.remix_of): 'duet' renders the source
-- reel side-by-side with the response, 'stitch' plays the source clip first
-- followed by the response. NULL = plain remix (attribution only).

ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS remix_mode TEXT;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'posts_remix_mode_check') THEN
    ALTER TABLE posts
      ADD CONSTRAINT posts_remix_mode_check CHECK (remix_mode IN ('duet', 'stitch'));
  END IF;
END $$;

-- A remix layout is meaningless without a source reel.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'posts_remix_mode_requires_source') THEN
    ALTER TABLE posts
      ADD CONSTRAINT posts_remix_mode_requires_source
      CHECK (remix_mode IS NULL OR remix_of IS NOT NULL);
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_posts_remix_of ON posts (remix_of) WHERE remix_of IS NOT NULL;

-- ---- gap-pack-8 continuation blocks ----

-- Gap pack 8: closes the remaining items across all five competitor matrices.
-- Reels following feed (TikTok), duet/stitch/trim/voiceover-mix compositor
-- (TikTok video editing), RTMP-style live ingest with HLS playback for
-- unlimited viewers (Telegram/TikTok live), marketplace checkout + affiliate
-- orders (TikTok Shop / FB Marketplace), profile Q&A (TikTok), screen-time
-- limits (TikTok/FB), animated custom emoji (Telegram), live-room co-hosting
-- (TikTok), media compression option (Telegram photo-vs-file), applock flag
-- (Telegram), and group scale indexes.

BEGIN;

-- ---- Compositor jobs (duet/stitch/trim/mix/live) on the transcode queue ----
ALTER TABLE transcode_jobs DROP CONSTRAINT IF EXISTS transcode_jobs_kind_check;
ALTER TABLE transcode_jobs ADD CONSTRAINT transcode_jobs_kind_check
  CHECK (kind IN ('video','audio','duet','stitch','trim','mix','live'));
ALTER TABLE transcode_jobs ADD COLUMN IF NOT EXISTS params JSONB NOT NULL DEFAULT '{}';
-- params: duet/stitch {"sources":[...urls]} · trim {"start_s":n,"duration_s":n}
--         mix {"sources":[video,audio]} · live {"rtmp_url":..., "room_id":...}

-- ---- Unlimited-viewer live streaming via HLS (RTMP pull → HLS ladder) ----
CREATE TABLE IF NOT EXISTS live_streams (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id    UUID NOT NULL REFERENCES live_rooms(id) ON DELETE CASCADE,
  host_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  stream_key TEXT NOT NULL UNIQUE,               -- 128-bit CSPRNG, gate for ingest start
  hls_url    TEXT NOT NULL DEFAULT '',           -- /live/<id>/master.m3u8
  status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','live','ended')),
  created_at TIMESTAMPTZ DEFAULT now(),
  ended_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_live_streams_room ON live_streams(room_id);

-- ---- Marketplace checkout (Shop) + affiliate attribution ----
CREATE TABLE IF NOT EXISTS marketplace_orders (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  listing_id         UUID NOT NULL REFERENCES marketplace_listings(id),
  seller_id          UUID NOT NULL REFERENCES users(id),
  buyer_id           UUID NOT NULL REFERENCES users(id),
  amount_usd         NUMERIC(20,8) NOT NULL,
  platform_fee_usd   NUMERIC(20,8) NOT NULL DEFAULT 0,
  affiliate_post_id  UUID REFERENCES posts(id) ON DELETE SET NULL,
  affiliate_usd      NUMERIC(20,8) NOT NULL DEFAULT 0,
  status             TEXT NOT NULL DEFAULT 'paid' CHECK (status IN ('paid','refunded')),
  created_at         TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_market_orders_buyer ON marketplace_orders(buyer_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_market_orders_seller ON marketplace_orders(seller_id, created_at DESC);
ALTER TABLE marketplace_listings ADD COLUMN IF NOT EXISTS sold_count INT NOT NULL DEFAULT 0;

-- ---- Profile Q&A (TikTok ask-and-answer) ----
CREATE TABLE IF NOT EXISTS profile_questions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,  -- profile owner
  asker_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  question    TEXT NOT NULL,
  answer      TEXT,
  answered_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_profile_q_public ON profile_questions(user_id, created_at DESC)
  WHERE answered_at IS NOT NULL;

-- ---- Screen-time limits / reminders ----
ALTER TABLE users ADD COLUMN IF NOT EXISTS screen_time_limit_minutes INT NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS screen_time_usage (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  day     DATE NOT NULL,
  minutes DOUBLE PRECISION NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, day)
);

-- ---- App lock (Telegram passkey/biometric local lock) ----
ALTER TABLE users ADD COLUMN IF NOT EXISTS app_lock_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- ---- Animated custom emoji ----
ALTER TABLE custom_emoji ADD COLUMN IF NOT EXISTS animated BOOLEAN NOT NULL DEFAULT FALSE;

-- ---- Live co-hosts (multi-publisher live rooms) ----
ALTER TABLE live_room_viewers ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'viewer'
  CHECK (role IN ('viewer','cohost'));

-- ---- Media compression option (Telegram photo vs file) ----
ALTER TABLE post_media ADD COLUMN IF NOT EXISTS compression TEXT NOT NULL DEFAULT 'original'
  CHECK (compression IN ('original','compressed'));
ALTER TABLE upload_sessions ADD COLUMN IF NOT EXISTS compression TEXT NOT NULL DEFAULT 'original'
  CHECK (compression IN ('original','compressed'));

-- ---- Group scale indexes (100k+ member fanout validation) ----
CREATE INDEX IF NOT EXISTS idx_conv_members_user ON conversation_members(user_id);
CREATE INDEX IF NOT EXISTS idx_messages_conv_created ON messages(conversation_id, created_at DESC);

COMMIT;
