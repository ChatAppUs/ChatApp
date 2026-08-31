-- Gap pack 4: closes the remaining implementable competitor gaps.
-- Drafts, topics, verified organizations, audio rooms (Spaces/voice chats),
-- premium plans, self-hosted GIF catalog, message entities (spoiler/bold/
-- italic/mono/link), contact-card messages, channel discussion groups +
-- stats, anonymous group admins, sound library, post shares, marketplace,
-- fundraisers, paywalled series, restricted mode + content ratings,
-- family pairing, XP/levels, people-nearby discovery, bot invoices,
-- inline-bot queries, mini apps, user interest vectors, live-gift ledger,
-- creator marketplace, professional analytics.

-- ---- Drafts (X/TikTok): server-side post drafts ----
CREATE TABLE IF NOT EXISTS post_drafts (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type        text NOT NULL DEFAULT 'post',
  body        text NOT NULL DEFAULT '',
  media       jsonb NOT NULL DEFAULT '[]',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS post_drafts_user_idx ON post_drafts(user_id, updated_at DESC);

-- ---- Topics / interests (X) ----
CREATE TABLE IF NOT EXISTS topics (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS topic_follows (
  topic_id   uuid NOT NULL REFERENCES topics(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (topic_id, user_id)
);
ALTER TABLE posts ADD COLUMN IF NOT EXISTS topic_id uuid REFERENCES topics(id) ON DELETE SET NULL;

-- ---- Verified organizations + affiliations (X) ----
CREATE TABLE IF NOT EXISTS organizations (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text NOT NULL,
  handle      text NOT NULL UNIQUE,
  owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  is_verified boolean NOT NULL DEFAULT false,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS organization_members (
  org_id     uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);
ALTER TABLE users ADD COLUMN IF NOT EXISTS affiliated_org_id uuid REFERENCES organizations(id) ON DELETE SET NULL;

-- ---- Audio rooms (X Spaces / Telegram voice chats / imo voice clubs) ----
CREATE TABLE IF NOT EXISTS audio_rooms (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title         text NOT NULL,
  status        text NOT NULL DEFAULT 'scheduled', -- scheduled|live|ended
  scheduled_at  timestamptz,
  ticket_price  numeric(20,8) NOT NULL DEFAULT 0,  -- 0 = free; >0 = ticketed space
  is_public     boolean NOT NULL DEFAULT true,
  created_at    timestamptz NOT NULL DEFAULT now(),
  started_at    timestamptz,
  ended_at      timestamptz
);
CREATE INDEX IF NOT EXISTS audio_rooms_status_idx ON audio_rooms(status, scheduled_at);
CREATE TABLE IF NOT EXISTS audio_room_participants (
  room_id     uuid NOT NULL REFERENCES audio_rooms(id) ON DELETE CASCADE,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role        text NOT NULL DEFAULT 'listener',    -- host|cohost|speaker|listener
  hand_raised boolean NOT NULL DEFAULT false,
  joined_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (room_id, user_id)
);
CREATE TABLE IF NOT EXISTS audio_room_tickets (
  room_id    uuid NOT NULL REFERENCES audio_rooms(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  amount_usd numeric(20,8) NOT NULL,
  tx_id      uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (room_id, user_id)
);

-- ---- Premium plans (X Premium / Telegram Premium) ----
CREATE TABLE IF NOT EXISTS premium_plans (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name       text NOT NULL UNIQUE,
  price_usd  numeric(20,8) NOT NULL,
  features   jsonb NOT NULL DEFAULT '[]',
  active     boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO premium_plans (name, price_usd, features) VALUES
  ('premium', 4.99, '["longer_posts","priority_ranking","badge"]'),
  ('premium_plus', 14.99, '["longer_posts","priority_ranking","badge","no_ads","creator_tools"]')
ON CONFLICT (name) DO NOTHING;
CREATE TABLE IF NOT EXISTS premium_subscriptions (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_id    uuid NOT NULL REFERENCES premium_plans(id) ON DELETE CASCADE,
  status     text NOT NULL DEFAULT 'active', -- active|expired|cancelled
  expires_at timestamptz NOT NULL,
  tx_id      uuid,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS premium_subs_user_idx ON premium_subscriptions(user_id, status);
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_premium boolean NOT NULL DEFAULT false;
-- Platform treasury: subscription revenue accrues here (internal ledger only).
INSERT INTO users (id, username, display_name, password_hash, status)
VALUES ('00000000-0000-0000-0000-000000000000', 'platform', 'Platform Treasury',
        '!locked', 'suspended')
ON CONFLICT (id) DO NOTHING;

-- ---- Self-hosted GIF catalog (replaces GIPHY/Tenor dependency) ----
CREATE TABLE IF NOT EXISTS gif_catalog (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  uploader_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      text NOT NULL DEFAULT '',
  tags       text[] NOT NULL DEFAULT '{}',
  media_url  text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS gif_catalog_tags_idx ON gif_catalog USING gin(tags);

-- ---- Message entities (Telegram spoilers + rich text) ----
ALTER TABLE messages ADD COLUMN IF NOT EXISTS entities jsonb NOT NULL DEFAULT '[]';

-- ---- Channel discussion groups + anonymous admins (Telegram) ----
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS discussion_group_id uuid REFERENCES conversations(id) ON DELETE SET NULL;
ALTER TABLE conversation_members ADD COLUMN IF NOT EXISTS is_anonymous boolean NOT NULL DEFAULT false;

-- ---- Sound library (self-hosted; replaces licensed-music gap) ----
CREATE TABLE IF NOT EXISTS sounds (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  uploader_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       text NOT NULL,
  artist      text NOT NULL DEFAULT '',
  media_url   text NOT NULL,
  duration_s  integer NOT NULL DEFAULT 0,
  use_count   bigint NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE posts ADD COLUMN IF NOT EXISTS sound_id uuid REFERENCES sounds(id) ON DELETE SET NULL;

-- ---- Post shares with counter (TikTok) ----
CREATE TABLE IF NOT EXISTS shares (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id    uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel    text NOT NULL DEFAULT 'link', -- link|dm|repost|external
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS shares_post_idx ON shares(post_id);

-- ---- Marketplace (Facebook) ----
CREATE TABLE IF NOT EXISTS marketplace_listings (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  seller_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       text NOT NULL,
  description text NOT NULL DEFAULT '',
  price_usd   numeric(20,8) NOT NULL,
  category    text NOT NULL DEFAULT 'general',
  media       jsonb NOT NULL DEFAULT '[]',
  status      text NOT NULL DEFAULT 'active', -- active|sold|removed
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS marketplace_active_idx ON marketplace_listings(status, category, created_at DESC);

-- ---- Fundraisers (Facebook) ----
CREATE TABLE IF NOT EXISTS fundraisers (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  creator_id  uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       text NOT NULL,
  description text NOT NULL DEFAULT '',
  goal_usd    numeric(20,8) NOT NULL,
  raised_usd  numeric(20,8) NOT NULL DEFAULT 0,
  status      text NOT NULL DEFAULT 'active', -- active|closed
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS fundraiser_donations (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  fundraiser_id uuid NOT NULL REFERENCES fundraisers(id) ON DELETE CASCADE,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  amount_usd    numeric(20,8) NOT NULL,
  tx_id         uuid,
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- ---- Paywalled series / paid posts (TikTok Series) ----
ALTER TABLE posts ADD COLUMN IF NOT EXISTS price_usd numeric(20,8) NOT NULL DEFAULT 0;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS content_rating text NOT NULL DEFAULT 'everyone'; -- everyone|mature
CREATE TABLE IF NOT EXISTS content_purchases (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id    uuid NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  amount_usd numeric(20,8) NOT NULL,
  tx_id      uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (post_id, user_id)
);

-- ---- Restricted mode (TikTok) ----
ALTER TABLE users ADD COLUMN IF NOT EXISTS restricted_mode boolean NOT NULL DEFAULT false;

-- ---- Family pairing (TikTok) ----
CREATE TABLE IF NOT EXISTS family_links (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  guardian_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  child_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      text NOT NULL DEFAULT 'pending', -- pending|active|revoked
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (guardian_id, child_id)
);

-- ---- XP / levels (imo gamification) ----
ALTER TABLE users ADD COLUMN IF NOT EXISTS xp bigint NOT NULL DEFAULT 0;

-- ---- People nearby / group discovery (Telegram + imo) ----
ALTER TABLE users ADD COLUMN IF NOT EXISTS discoverable boolean NOT NULL DEFAULT false;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS category text NOT NULL DEFAULT '';

-- ---- Bot payments + inline bots + mini apps (Telegram) ----
CREATE TABLE IF NOT EXISTS bot_invoices (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  bot_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      text NOT NULL,
  amount_usd numeric(20,8) NOT NULL,
  payload    text NOT NULL DEFAULT '',
  status     text NOT NULL DEFAULT 'pending', -- pending|paid|cancelled
  tx_id      uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  paid_at    timestamptz
);
-- Mini apps reuse bots.mini_app_url (migration 013); no new table.

-- ---- Implicit user interest vector (TikTok ranking) ----
CREATE TABLE IF NOT EXISTS user_interests (
  user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  hashtag    text NOT NULL,
  score      double precision NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, hashtag)
);

-- ---- Live gifts ledger (TikTok live gifts + leaderboard, imo room gifts) ----
CREATE TABLE IF NOT EXISTS live_gifts (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id     text NOT NULL,
  gift_id     uuid NOT NULL REFERENCES gift_catalog(id) ON DELETE CASCADE,
  from_user   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  amount_usd  numeric(20,8) NOT NULL,
  tx_id       uuid,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS live_gifts_room_idx ON live_gifts(room_id, created_at);

-- ---- Creator marketplace (TikTok brand deals) ----
CREATE TABLE IF NOT EXISTS brand_deals (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  brand_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  creator_id  uuid REFERENCES users(id) ON DELETE SET NULL,
  title       text NOT NULL,
  brief       text NOT NULL DEFAULT '',
  budget_usd  numeric(20,8) NOT NULL,
  status      text NOT NULL DEFAULT 'open', -- open|accepted|completed|cancelled
  created_at  timestamptz NOT NULL DEFAULT now()
);
