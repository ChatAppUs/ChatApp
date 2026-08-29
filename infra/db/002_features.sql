-- ChatApp migration 002: messaging upgrades, E2EE, 2FA, channels, polls,
-- hashtags, bookmarks, reposts, presence, creator payouts, privacy blocks.

-- Presence, 2FA (TOTP), and end-to-end encryption identity keys on users.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS totp_secret TEXT,
  ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS e2e_identity_key TEXT,        -- base64 SPKI public key (ECDH P-256)
  ADD COLUMN IF NOT EXISTS e2e_key_updated_at TIMESTAMPTZ;

-- Broadcast channels (Telegram-style) and group descriptions.
ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS is_channel BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';

-- Message upgrades: E2EE flag, replies, forwards.
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS is_encrypted BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS reply_to_id UUID REFERENCES messages(id) ON DELETE SET NULL;

-- Emoji reactions on messages (Telegram/WhatsApp style).
CREATE TABLE IF NOT EXISTS message_reactions (
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  emoji      TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (message_id, user_id, emoji)
);

-- Read state per member per conversation (drives read receipts / unread counts).
CREATE TABLE IF NOT EXISTS conversation_reads (
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  last_read_at    TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (conversation_id, user_id)
);

-- Reposts (X-style retweet) and quote body stays in posts.body.
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS repost_of UUID REFERENCES posts(id) ON DELETE SET NULL;

-- Polls attached to posts (Telegram/X style).
CREATE TABLE IF NOT EXISTS poll_options (
  id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  idx     INT NOT NULL,
  label   TEXT NOT NULL,
  UNIQUE (post_id, idx)
);
CREATE TABLE IF NOT EXISTS poll_votes (
  option_id UUID NOT NULL REFERENCES poll_options(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id   UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (post_id, user_id)  -- one vote per user per poll
);

-- Hashtags and trending.
CREATE TABLE IF NOT EXISTS hashtags (
  tag        TEXT PRIMARY KEY,
  use_count  BIGINT DEFAULT 0,
  last_used  TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE IF NOT EXISTS post_hashtags (
  post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  tag     TEXT NOT NULL REFERENCES hashtags(tag) ON DELETE CASCADE,
  PRIMARY KEY (post_id, tag)
);
CREATE INDEX IF NOT EXISTS idx_post_hashtags_tag ON post_hashtags(tag);

-- Bookmarks / saved posts (X/Telegram "Saved Messages" for posts).
CREATE TABLE IF NOT EXISTS bookmarks (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, post_id)
);

-- Privacy: user blocks (WhatsApp/Telegram style).
CREATE TABLE IF NOT EXISTS user_blocks (
  blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (blocker_id, blocked_id),
  CHECK (blocker_id <> blocked_id)
);

-- Creator monetization: payout requests against creator_earnings balance.
CREATE TABLE IF NOT EXISTS payout_requests (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  creator_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  amount     NUMERIC(20,8) NOT NULL CHECK (amount > 0),
  asset      TEXT NOT NULL DEFAULT 'USD',
  status     TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','paid','rejected')),
  destination TEXT NOT NULL DEFAULT '',
  reviewed_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ DEFAULT now(),
  reviewed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_payouts_creator ON payout_requests(creator_id, created_at DESC);

-- Track post views per user for creator revenue share analytics.
CREATE TABLE IF NOT EXISTS post_views (
  post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  viewed_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (post_id, user_id)
);
