-- 044 — Gap pack 11 (master-plan §4.1/§4.2/§4.5/§4.6/§13/§15/§17/§29/§50/§63/§70):
--   * rich profile fields (pronouns, cover, work/education, relationship privacy,
--     featured content, professional/business mode)
--   * unified relationship graph beyond follow/friend (family, colleague,
--     classmate, community member, creator supporter, subscriber, business
--     customer, restricted)
--   * comment lifecycle: edits with history, GIF/image/video comment media,
--     server-computed discussion summary for very large threads
--   * share targets: feed, story, group, community, external link
--   * explainable recommendations: per-item reasons + user controls
--     (more-like-this, less-like-this, hide creator, remove topic, reset)
--   * long-form video: chapters, subtitle tracks, continue-watching progress
--   * rich story kinds: link, location, countdown, question
--   * events: paid ticket tiers, purchases, waitlist, QR check-in
--   * developer platform: third-party apps (client credentials, scopes,
--     webhooks) distinct from first-party bots
--   * domain event log for event-driven pipelines (§63)
--   * copyright: per-post rights owner + remix policy (§70)

-- ---- Rich profiles (§4.1) ----
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS pronouns             TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS cover_url            TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS location_text        TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS location_visibility  TEXT NOT NULL DEFAULT 'private'
      CHECK (location_visibility IN ('private','followers','public')),
  ADD COLUMN IF NOT EXISTS work_history         JSONB,
  ADD COLUMN IF NOT EXISTS education            JSONB,
  ADD COLUMN IF NOT EXISTS relationship_status  TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS relationship_privacy TEXT NOT NULL DEFAULT 'private'
      CHECK (relationship_privacy IN ('private','friends','followers','public')),
  ADD COLUMN IF NOT EXISTS featured_post_ids    UUID[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS professional_mode    BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS business_category    TEXT NOT NULL DEFAULT '';

-- ---- Unified relationship graph (§4.2) ----
CREATE TABLE IF NOT EXISTS user_relationships (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  other_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN
      ('family','close_friend','colleague','classmate','community_member',
       'creator_supporter','subscriber','business_customer','restricted')),
  note       TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, other_id, kind),
  CHECK (user_id <> other_id)
);
CREATE INDEX IF NOT EXISTS idx_user_relationships_other ON user_relationships(other_id);

-- ---- Comment lifecycle (§4.5, §9) ----
ALTER TABLE comments
  ADD COLUMN IF NOT EXISTS edited_at  TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS media_url  TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS media_kind TEXT NOT NULL DEFAULT ''
      CHECK (media_kind IN ('','gif','image','video')),
  ADD COLUMN IF NOT EXISTS edited_body TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS comment_edit_history (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  comment_id UUID NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  edited_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_comment_edit_history ON comment_edit_history(comment_id, id);

-- ---- Share targets (§4.6) ----
ALTER TABLE shares ALTER COLUMN channel TYPE text; -- widen beyond original enum comment

-- ---- Explainable recommendations (§13) ----
CREATE TABLE IF NOT EXISTS recommendation_feedback (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id    UUID REFERENCES posts(id) ON DELETE CASCADE,
  action     TEXT NOT NULL CHECK (action IN
      ('more_like_this','less_like_this','hide_creator','remove_topic','not_interested')),
  reason_key TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_recommendation_feedback_user ON recommendation_feedback(user_id, created_at DESC);

-- ---- Long-form video: chapters, subtitles, continue-watching (§14/§15/§16) ----
CREATE TABLE IF NOT EXISTS video_chapters (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  start_ms   INT NOT NULL CHECK (start_ms >= 0),
  title      TEXT NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (post_id, start_ms)
);
CREATE INDEX IF NOT EXISTS idx_video_chapters_post ON video_chapters(post_id, start_ms);

CREATE TABLE IF NOT EXISTS video_captions (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  lang       TEXT NOT NULL,
  label      TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL,
  source     TEXT NOT NULL DEFAULT 'creator' CHECK (source IN ('creator','ai')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (post_id, lang)
);
CREATE INDEX IF NOT EXISTS idx_video_captions_post ON video_captions(post_id);

CREATE TABLE IF NOT EXISTS continue_watching (
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id       UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  position_ms   INT NOT NULL DEFAULT 0,
  duration_ms   INT NOT NULL DEFAULT 0,
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, post_id)
);

-- ---- Rich stories (§17) ----
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS story_kind         TEXT NOT NULL DEFAULT ''
      CHECK (story_kind IN ('','photo','video','text','music','poll','question','countdown','link','location')),
  ADD COLUMN IF NOT EXISTS story_link         TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS story_question     JSONB,
  ADD COLUMN IF NOT EXISTS story_countdown_to TIMESTAMPTZ;

-- ---- Events: tickets, waitlist, check-in (§29) ----
CREATE TABLE IF NOT EXISTS event_ticket_tiers (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  event_id    UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  price_cents BIGINT NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
  quantity    INT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
  sold        INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_event_ticket_tiers ON event_ticket_tiers(event_id);

CREATE TABLE IF NOT EXISTS event_ticket_purchases (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tier_id     UUID NOT NULL REFERENCES event_ticket_tiers(id) ON DELETE CASCADE,
  event_id    UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  checkin_token TEXT NOT NULL DEFAULT encode(gen_random_bytes(12),'hex'),
  checked_in  BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tier_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_event_purchases_event ON event_ticket_purchases(event_id);

CREATE TABLE IF NOT EXISTS event_waitlist (
  event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, user_id)
);

CREATE TABLE IF NOT EXISTS event_checkins (
  event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (event_id, user_id)
);

-- ---- Developer platform (§50) ----
CREATE TABLE IF NOT EXISTS developer_apps (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name              TEXT NOT NULL,
  description       TEXT NOT NULL DEFAULT '',
  client_id         TEXT NOT NULL UNIQUE,
  client_secret_hash TEXT NOT NULL,
  redirect_uris     JSONB NOT NULL DEFAULT '[]',
  scopes            JSONB NOT NULL DEFAULT '[]',
  webhook_url       TEXT NOT NULL DEFAULT '',
  webhook_secret    TEXT NOT NULL DEFAULT '',
  status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_developer_apps_owner ON developer_apps(owner_id);

CREATE TABLE IF NOT EXISTS developer_app_tokens (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  app_id     UUID NOT NULL REFERENCES developer_apps(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL,
  scopes     JSONB NOT NULL DEFAULT '[]',
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_dev_app_tokens_app ON developer_app_tokens(app_id);

-- ---- Domain event log (§63) ----
CREATE TABLE IF NOT EXISTS domain_events (
  id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name         TEXT NOT NULL,
  aggregate_id TEXT NOT NULL DEFAULT '',
  payload      JSONB NOT NULL DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_domain_events_name ON domain_events(name, created_at DESC);

-- ---- Copyright: per-post rights (§70) ----
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS rights_owner TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS remix_policy TEXT NOT NULL DEFAULT 'open'
      CHECK (remix_policy IN ('open','approval','closed'));
