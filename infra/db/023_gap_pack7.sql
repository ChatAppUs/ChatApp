-- 022_gap_pack7.sql — quiz polls (Telegram quizzes + anonymous voting),
-- X Moments (curated collections), audio-room recording & replay (X Spaces),
-- live-room replay, chunked resumable uploads (Telegram 2GB files), and
-- related-reel text embeddings (TikTok recommendation depth).
BEGIN;

-- Telegram quiz mode + anonymous voting on chat polls.
ALTER TABLE message_polls
  ADD COLUMN IF NOT EXISTS is_quiz BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS correct_option_id UUID,
  ADD COLUMN IF NOT EXISTS explanation TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS anonymous BOOLEAN NOT NULL DEFAULT FALSE;

-- X Moments: editorially curated collections of posts.
CREATE TABLE IF NOT EXISTS moments (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title        TEXT NOT NULL,
  summary      TEXT NOT NULL DEFAULT '',
  cover_url    TEXT NOT NULL DEFAULT '',
  created_by   UUID REFERENCES users(id) ON DELETE SET NULL,
  published_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS moment_items (
  moment_id UUID NOT NULL REFERENCES moments(id) ON DELETE CASCADE,
  post_id   UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  position  INT NOT NULL DEFAULT 0,
  PRIMARY KEY (moment_id, post_id)
);
CREATE INDEX IF NOT EXISTS moments_published_idx
  ON moments(published_at DESC) WHERE published_at IS NOT NULL;

-- X Spaces recording & replay: recordings attached to audio rooms.
CREATE TABLE IF NOT EXISTS audio_room_recordings (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id    UUID NOT NULL REFERENCES audio_rooms(id) ON DELETE CASCADE,
  media_id   TEXT NOT NULL,
  duration_s INT NOT NULL DEFAULT 0,
  created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audio_room_recordings_room_idx
  ON audio_room_recordings(room_id, created_at DESC);

-- Live replay: a finished live room can attach its recording for replay.
ALTER TABLE live_rooms
  ADD COLUMN IF NOT EXISTS replay_media_id TEXT;

-- Chunked resumable uploads (Telegram 2GB files): session metadata lives
-- here; bytes flow straight through the C++ media edge (.part files).
CREATE TABLE IF NOT EXISTS upload_sessions (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  filename       TEXT NOT NULL,
  total_bytes    BIGINT NOT NULL,
  received_bytes BIGINT NOT NULL DEFAULT 0,
  status         TEXT NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active','completed','aborted')),
  media_url      TEXT NOT NULL DEFAULT '',
  expires_at     TIMESTAMPTZ NOT NULL DEFAULT now() + interval '24 hours',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS upload_sessions_user_idx
  ON upload_sessions(user_id, created_at DESC);

-- Related-reel embeddings (TikTok content embeddings): 256-dim hashing
-- vectors computed by services/ml (/embed), stored inline so cosine
-- similarity runs in plain SQL/Go without a vector extension.
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS embedding REAL[];

COMMIT;
