-- 018_gap_pack3.sql — third competitor gap-closure batch:
-- privacy suite depth (presence/phone granularity, data saver, account TTL,
-- safety mode), X features (reply policy, content warnings, alt text, lists,
-- bookmark folders, hidden replies, paid verification), TikTok features
-- (creator comment pinning, playlists, profile visitors), Telegram features
-- (sticker packs, chat folders, archive, public group handles, granular
-- group-admin permissions).
BEGIN;

-- ---------- Users: privacy & safety ----------
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS last_seen_privacy TEXT NOT NULL DEFAULT 'everyone',
  ADD COLUMN IF NOT EXISTS phone_privacy     TEXT NOT NULL DEFAULT 'everyone',
  ADD COLUMN IF NOT EXISTS data_saver        BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS account_ttl_days  INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS safety_mode       BOOLEAN NOT NULL DEFAULT FALSE;

-- ---------- Posts: X parity ----------
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS reply_policy   TEXT NOT NULL DEFAULT 'everyone',
  ADD COLUMN IF NOT EXISTS content_warning TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS sensitive      BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS pinned_comment_id UUID REFERENCES comments(id) ON DELETE SET NULL;

-- Accessibility: alt text on media items.
ALTER TABLE post_media ADD COLUMN IF NOT EXISTS alt_text TEXT NOT NULL DEFAULT '';

-- Hidden replies: the post author (or comment author) hides a comment.
ALTER TABLE comments ADD COLUMN IF NOT EXISTS hidden_at TIMESTAMPTZ;

-- ---------- Chat: archive, per-admin permissions, public handles ----------
ALTER TABLE conversation_members
  ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS perms JSONB;

ALTER TABLE conversations ADD COLUMN IF NOT EXISTS handle TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_conversations_handle
  ON conversations(handle) WHERE handle IS NOT NULL;

-- ---------- Stickers (self-hosted packs; no third-party dependency) ----------
CREATE TABLE IF NOT EXISTS sticker_packs (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL UNIQUE,
  title      TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE IF NOT EXISTS stickers (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  pack_id   UUID NOT NULL REFERENCES sticker_packs(id) ON DELETE CASCADE,
  emoji     TEXT NOT NULL DEFAULT '',
  media_url TEXT NOT NULL,
  position  INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_stickers_pack ON stickers(pack_id, position);

-- ---------- Chat folders (per-user conversation grouping) ----------
CREATE TABLE IF NOT EXISTS chat_folders (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (user_id, name)
);
CREATE TABLE IF NOT EXISTS chat_folder_items (
  folder_id       UUID NOT NULL REFERENCES chat_folders(id) ON DELETE CASCADE,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  PRIMARY KEY (folder_id, conversation_id)
);

-- ---------- Lists (curated user groups with their own feed) ----------
CREATE TABLE IF NOT EXISTS user_lists (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (owner_id, name)
);
CREATE TABLE IF NOT EXISTS user_list_members (
  list_id UUID NOT NULL REFERENCES user_lists(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (list_id, user_id)
);

-- ---------- Bookmark folders ----------
CREATE TABLE IF NOT EXISTS bookmark_folders (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (user_id, name)
);
ALTER TABLE bookmarks
  ADD COLUMN IF NOT EXISTS folder_id UUID REFERENCES bookmark_folders(id) ON DELETE SET NULL;

-- ---------- Profile visitors ("who viewed me") ----------
CREATE TABLE IF NOT EXISTS profile_views (
  profile_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  viewer_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  viewed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (profile_id, viewer_id)
);

-- ---------- Playlists (creator-curated reel collections) ----------
CREATE TABLE IF NOT EXISTS playlists (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (owner_id, title)
);
CREATE TABLE IF NOT EXISTS playlist_items (
  playlist_id UUID NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
  post_id     UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  position    INT NOT NULL DEFAULT 0,
  PRIMARY KEY (playlist_id, post_id)
);

-- ---------- Paid verification flow ----------
CREATE TABLE IF NOT EXISTS verification_requests (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tier        TEXT NOT NULL DEFAULT 'blue',
  status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
  note        TEXT NOT NULL DEFAULT '',
  reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  reviewed_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_verification_requests_status
  ON verification_requests(status) WHERE status = 'pending';

COMMIT;
