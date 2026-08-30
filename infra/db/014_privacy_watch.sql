-- 014_privacy_watch.sql — privacy suite depth, watch-time signals, stories
-- highlights, close friends, and messaging polish (silent send, per-user
-- delete, invite links, slow mode, topics, link previews).
BEGIN;

-- Watch-time signals feeding the FYP/reels ranker (TikTok parity).
CREATE TABLE IF NOT EXISTS reel_watch_events (
  id          BIGSERIAL PRIMARY KEY,
  user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
  post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  watched_ms  INT NOT NULL DEFAULT 0,
  duration_ms INT NOT NULL DEFAULT 0,
  completed   BOOLEAN NOT NULL DEFAULT FALSE,
  rewatched   BOOLEAN NOT NULL DEFAULT FALSE,
  not_interested BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_watch_post ON reel_watch_events(post_id);
CREATE INDEX IF NOT EXISTS idx_watch_user ON reel_watch_events(user_id, id DESC);

-- Stories: permanent highlight collections + close-friends audience.
CREATE TABLE IF NOT EXISTS story_highlights (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  cover_url  TEXT DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE IF NOT EXISTS story_highlight_items (
  highlight_id UUID NOT NULL REFERENCES story_highlights(id) ON DELETE CASCADE,
  story_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  position     INT NOT NULL DEFAULT 0,
  PRIMARY KEY (highlight_id, story_id)
);

CREATE TABLE IF NOT EXISTS close_friends (
  user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  friend_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, friend_id),
  CHECK (user_id <> friend_id)
);
-- 'close_friends' becomes a valid story/post audience alongside
-- public/followers/private.
ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_visibility_check;
ALTER TABLE posts ADD CONSTRAINT posts_visibility_check
  CHECK (visibility IN ('public','followers','private','close_friends'));

-- Privacy suite: mutes (hide content without unfollow/block), keyword
-- filters, restricted list (see you, you never see their content),
-- profile lock (follow requests required).
CREATE TABLE IF NOT EXISTS user_mutes (
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  muted_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, muted_id),
  CHECK (user_id <> muted_id)
);

CREATE TABLE IF NOT EXISTS word_filters (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  phrase     TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, phrase)
);

CREATE TABLE IF NOT EXISTS restricted_list (
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  restricted_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at    TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, restricted_id),
  CHECK (user_id <> restricted_id)
);

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS profile_locked BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS show_active_status BOOLEAN NOT NULL DEFAULT TRUE;

-- Follow requests for locked profiles.
CREATE TABLE IF NOT EXISTS follow_requests (
  follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (follower_id, followee_id)
);

-- Message requests inbox: first-time DMs from non-followed users land as
-- requests until accepted.
CREATE TABLE IF NOT EXISTS message_requests (
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  recipient_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined')),
  created_at      TIMESTAMPTZ DEFAULT now(),
  responded_at    TIMESTAMPTZ,
  PRIMARY KEY (conversation_id, recipient_id)
);

-- Messaging polish.
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS is_silent BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS link_preview JSONB;

-- Per-user delete ("delete for me"): the row stays, hidden per user.
CREATE TABLE IF NOT EXISTS message_hidden (
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (message_id, user_id)
);

-- Group invite links (public handles) + slow mode + forum topics.
CREATE TABLE IF NOT EXISTS conversation_invites (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  code            TEXT NOT NULL UNIQUE,
  created_by      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  max_uses        INT NOT NULL DEFAULT 0,     -- 0 = unlimited
  use_count       INT NOT NULL DEFAULT 0,
  expires_at      TIMESTAMPTZ,
  revoked_at      TIMESTAMPTZ,
  created_at      TIMESTAMPTZ DEFAULT now()
);

ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS slow_mode_seconds INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS is_forum BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS conversation_topics (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  title           TEXT NOT NULL,
  created_by      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at      TIMESTAMPTZ DEFAULT now(),
  UNIQUE (conversation_id, title)
);
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS topic_id UUID REFERENCES conversation_topics(id) ON DELETE SET NULL;

-- Per-member slow-mode exemption is implied by role (owner/admin exempt).
CREATE TABLE IF NOT EXISTS message_rate_marks (
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  last_sent_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (conversation_id, user_id)
);

-- Cross-device draft sync (server-authoritative drafts).
CREATE TABLE IF NOT EXISTS conversation_drafts (
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body            TEXT NOT NULL DEFAULT '',
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (conversation_id, user_id)
);

-- Transcoding job queue for the C++ transcode worker (SKIP LOCKED claiming).
CREATE TABLE IF NOT EXISTS transcode_jobs (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  media_id    TEXT NOT NULL,               -- media-edge file id
  source_url  TEXT NOT NULL,
  kind        TEXT NOT NULL CHECK (kind IN ('video','audio')),
  status      TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','done','failed')),
  ladder      JSONB NOT NULL DEFAULT '[]', -- produced renditions
  thumb_url   TEXT DEFAULT '',
  duration_ms INT NOT NULL DEFAULT 0,
  error       TEXT DEFAULT '',
  attempts    INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ DEFAULT now(),
  claimed_at  TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_transcode_status ON transcode_jobs(status) WHERE status = 'queued';

COMMIT;
