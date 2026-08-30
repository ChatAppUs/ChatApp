-- 011_groups_pages.sql — Facebook-style content Groups, business Pages, and
-- Events. Posts reuse the existing posts table with an optional owner entity.
BEGIN;

DO $$ BEGIN
  CREATE TYPE group_privacy AS ENUM ('public', 'private');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS content_groups (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT NOT NULL,
  slug        CITEXT UNIQUE NOT NULL,
  description TEXT DEFAULT '',
  cover_url   TEXT DEFAULT '',
  privacy     group_privacy NOT NULL DEFAULT 'public',
  created_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  member_count INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS group_members (
  group_id   UUID NOT NULL REFERENCES content_groups(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','moderator','member')),
  status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','pending')),
  joined_at  TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (group_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);

CREATE TABLE IF NOT EXISTS pages (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT NOT NULL,
  slug        CITEXT UNIQUE NOT NULL,
  category    TEXT DEFAULT '',
  description TEXT DEFAULT '',
  avatar_url  TEXT DEFAULT '',
  cover_url   TEXT DEFAULT '',
  owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  follower_count INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS page_followers (
  page_id    UUID NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (page_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_page_followers_user ON page_followers(user_id);

-- Events belong to a group, a page, or a user (standalone).
CREATE TABLE IF NOT EXISTS events (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title       TEXT NOT NULL,
  description TEXT DEFAULT '',
  location    TEXT DEFAULT '',
  cover_url   TEXT DEFAULT '',
  starts_at   TIMESTAMPTZ NOT NULL,
  ends_at     TIMESTAMPTZ,
  group_id    UUID REFERENCES content_groups(id) ON DELETE CASCADE,
  page_id     UUID REFERENCES pages(id) ON DELETE CASCADE,
  created_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_events_starts ON events(starts_at);

CREATE TABLE IF NOT EXISTS event_rsvps (
  event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  response   TEXT NOT NULL CHECK (response IN ('going','interested','declined')),
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (event_id, user_id)
);

-- Posts can be scoped to a group or a page in addition to a user profile.
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES content_groups(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS page_id  UUID REFERENCES pages(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_posts_group ON posts(group_id, created_at DESC) WHERE group_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_posts_page ON posts(page_id, created_at DESC) WHERE page_id IS NOT NULL;

COMMIT;
