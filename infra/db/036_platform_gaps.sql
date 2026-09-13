-- Platform gap features (master plan §20 Live Shopping, §23 AI Creator Tools,
-- §30 Forums, §32 ChatApp Pulse, §38 AI Assistant).
--
-- Every table here backs a real, server-side implementation path:
--   * forums            — communities/forums with topics and threaded posts
--   * pulse_*           — X/Twitter-class public conversation (short posts,
--                         threads, quotes, reposts, topics, trends, lists)
--   * live_products /   — live shopping: product pins, coupons, inventory,
--     live_product_pins   and real double-entry checkout orders
--   * media_dubs        — AI dubbing jobs (labelled translated audio)
--   * ai_clip_jobs /    — AI clip analysis of long video + per-clip approval
--     ai_clips
--   * assistant_*       — AI assistant conversations, messages and proposals
--                         that require explicit human approval before applying
--
-- Provider-backed work (ASR/TTS/translation) is never faked: the ML service
-- returns `available:false` with the reason when the model is not configured,
-- and the API persists that truthful state.

-- ---------------------------------------------------------------- Forums ----
CREATE TABLE IF NOT EXISTS forums (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  group_id    UUID,
  title       TEXT NOT NULL,
  slug        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  visibility  TEXT NOT NULL DEFAULT 'public'
              CHECK (visibility IN ('public','private','secret')),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_forums_owner ON forums(owner_id);
CREATE INDEX IF NOT EXISTS idx_forums_group ON forums(group_id);

CREATE TABLE IF NOT EXISTS forum_moderators (
  forum_id  UUID NOT NULL REFERENCES forums(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (forum_id, user_id)
);

CREATE TABLE IF NOT EXISTS forum_topics (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  forum_id         UUID NOT NULL REFERENCES forums(id) ON DELETE CASCADE,
  author_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title            TEXT NOT NULL,
  body             TEXT NOT NULL DEFAULT '',
  pinned           BOOLEAN NOT NULL DEFAULT FALSE,
  locked           BOOLEAN NOT NULL DEFAULT FALSE,
  post_count       INT NOT NULL DEFAULT 0,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_activity_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_forum_topics_forum
  ON forum_topics(forum_id, pinned DESC, last_activity_at DESC);

CREATE TABLE IF NOT EXISTS forum_posts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  topic_id   UUID NOT NULL REFERENCES forum_topics(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  parent_id  UUID REFERENCES forum_posts(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_forum_posts_topic ON forum_posts(topic_id, created_at);

-- ---------------------------------------------------- ChatApp Pulse (§32) ---
CREATE TABLE IF NOT EXISTS pulse_posts (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  parent_id  UUID REFERENCES pulse_posts(id) ON DELETE CASCADE,  -- reply / thread
  quote_of   UUID REFERENCES pulse_posts(id) ON DELETE SET NULL, -- quote post
  repost_of  UUID REFERENCES pulse_posts(id) ON DELETE SET NULL, -- repost
  topics     TEXT[] NOT NULL DEFAULT '{}',
  local_tag  TEXT NOT NULL DEFAULT '',                           -- local feed
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pulse_posts_created ON pulse_posts(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pulse_posts_author ON pulse_posts(author_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pulse_posts_parent ON pulse_posts(parent_id);
CREATE INDEX IF NOT EXISTS idx_pulse_posts_topics ON pulse_posts USING GIN (topics);

CREATE TABLE IF NOT EXISTS pulse_topics (
  name        TEXT PRIMARY KEY,
  description TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Trends are computed from real post volume in a rolling window.
CREATE TABLE IF NOT EXISTS pulse_trends (
  topic        TEXT NOT NULL,
  scope        TEXT NOT NULL DEFAULT 'global' CHECK (scope IN ('global','local')),
  region       TEXT NOT NULL DEFAULT '',
  score        INT NOT NULL DEFAULT 0,
  window_start TIMESTAMPTZ NOT NULL,
  computed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (topic, scope, region)
);
CREATE INDEX IF NOT EXISTS idx_pulse_trends_rank ON pulse_trends(scope, score DESC);

CREATE TABLE IF NOT EXISTS pulse_lists (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (owner_id, name)
);

CREATE TABLE IF NOT EXISTS pulse_list_members (
  list_id   UUID NOT NULL REFERENCES pulse_lists(id) ON DELETE CASCADE,
  member_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (list_id, member_id)
);

-- --------------------------------------------------- Live Shopping (§20) ----
CREATE TABLE IF NOT EXISTS live_products (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id       UUID NOT NULL,
  seller_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title         TEXT NOT NULL,
  description   TEXT NOT NULL DEFAULT '',
  price_usd     NUMERIC(38,18) NOT NULL CHECK (price_usd > 0),
  inventory     INT NOT NULL DEFAULT 0 CHECK (inventory >= 0),
  discount_pct  NUMERIC(8,4) NOT NULL DEFAULT 0
                CHECK (discount_pct >= 0 AND discount_pct <= 100),
  image_url     TEXT NOT NULL DEFAULT '',
  active        BOOLEAN NOT NULL DEFAULT TRUE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_live_products_room ON live_products(room_id, created_at DESC);

-- The pinned/carousel product for a live room (one active pin per room).
CREATE TABLE IF NOT EXISTS live_product_pins (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id    UUID NOT NULL,
  product_id UUID NOT NULL REFERENCES live_products(id) ON DELETE CASCADE,
  pinned_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  pinned_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  unpinned_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_live_pins_room ON live_product_pins(room_id, pinned_at DESC);

CREATE TABLE IF NOT EXISTS live_coupons (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id      UUID NOT NULL,
  code         TEXT NOT NULL,
  discount_pct NUMERIC(8,4) NOT NULL CHECK (discount_pct > 0 AND discount_pct <= 100),
  max_uses     INT NOT NULL DEFAULT 0,
  uses         INT NOT NULL DEFAULT 0,
  active       BOOLEAN NOT NULL DEFAULT TRUE,
  created_by   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (room_id, code)
);

-- Real purchases: inventory is decremented under a row lock and funds move on
-- the double-entry ledger (buyer -> seller, platform fee -> treasury).
CREATE TABLE IF NOT EXISTS live_orders (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id     UUID NOT NULL REFERENCES live_products(id) ON DELETE RESTRICT,
  room_id        UUID NOT NULL,
  buyer_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  seller_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  quantity       INT NOT NULL CHECK (quantity > 0),
  unit_price_usd NUMERIC(38,18) NOT NULL,
  discount_usd   NUMERIC(38,18) NOT NULL DEFAULT 0,
  coupon_code    TEXT NOT NULL DEFAULT '',
  total_usd      NUMERIC(38,18) NOT NULL,
  platform_fee_usd NUMERIC(38,18) NOT NULL DEFAULT 0,
  ledger_tx      UUID,
  status         TEXT NOT NULL DEFAULT 'paid' CHECK (status IN ('paid','refunded')),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_live_orders_room ON live_orders(room_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_live_orders_buyer ON live_orders(buyer_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_live_orders_seller ON live_orders(seller_id, created_at DESC);

-- ------------------------------------------ AI Creator Tools (§23) ----------
-- AI dubbing: optional translated audio with explicit labelling. Status stays
-- 'unavailable' with the provider reason when the model is not configured.
CREATE TABLE IF NOT EXISTS media_dubs (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  media_url   TEXT NOT NULL,
  source_lang TEXT NOT NULL DEFAULT '',
  target_lang TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'pending'
              CHECK (status IN ('pending','ready','unavailable','failed')),
  audio_url   TEXT NOT NULL DEFAULT '',
  label       TEXT NOT NULL DEFAULT '',
  reason      TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (owner_id, media_url, target_lang)
);

-- AI clip analysis of a long video, plus per-clip human approval.
CREATE TABLE IF NOT EXISTS ai_clip_jobs (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  media_url     TEXT NOT NULL,
  duration_s    NUMERIC(12,3) NOT NULL DEFAULT 0,
  status        TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','analyzed','unavailable','failed')),
  reason        TEXT NOT NULL DEFAULT '',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_clip_jobs_owner ON ai_clip_jobs(owner_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ai_clips (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  job_id     UUID NOT NULL REFERENCES ai_clip_jobs(id) ON DELETE CASCADE,
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  start_s    NUMERIC(12,3) NOT NULL,
  end_s      NUMERIC(12,3) NOT NULL,
  score      NUMERIC(8,4) NOT NULL DEFAULT 0,
  reason     TEXT NOT NULL DEFAULT '',
  title      TEXT NOT NULL DEFAULT '',
  approved   BOOLEAN NOT NULL DEFAULT FALSE,
  published  BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_clips_job ON ai_clips(job_id, score DESC);

-- -------------------------------------------------- AI Assistant (§38) ------
CREATE TABLE IF NOT EXISTS assistant_conversations (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title      TEXT NOT NULL DEFAULT 'New conversation',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_assistant_conv_owner
  ON assistant_conversations(owner_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS assistant_messages (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id UUID NOT NULL REFERENCES assistant_conversations(id) ON DELETE CASCADE,
  role            TEXT NOT NULL CHECK (role IN ('user','assistant')),
  content         TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_assistant_msg_conv
  ON assistant_messages(conversation_id, created_at);

-- Proposals the assistant may not apply on its own. The spec requires explicit
-- human approval before AI-authored content is published.
CREATE TABLE IF NOT EXISTS assistant_actions (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID REFERENCES assistant_conversations(id) ON DELETE CASCADE,
  kind            TEXT NOT NULL,
  payload         JSONB NOT NULL DEFAULT '{}',
  status          TEXT NOT NULL DEFAULT 'proposed'
                  CHECK (status IN ('proposed','approved','dismissed')),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  decided_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_assistant_actions_owner
  ON assistant_actions(owner_id, status, created_at DESC);
