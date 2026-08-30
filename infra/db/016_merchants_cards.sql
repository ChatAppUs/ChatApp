-- 016_merchants_cards.sql - P2P merchant verification + tiers, self-issued
-- virtual crypto cards (platform side), post reactions, pinned posts, post
-- edit history, scheduled posts, photo albums, people tags, feeling/location
-- metadata, chat themes + per-chat nicknames, and event reminders.
BEGIN;

-- ============ P2P merchant verification + tiers ============

-- Tier ladder. Admins promote/demote merchants between levels; limits are
-- enforced at trade-open time against the fiat value of the trade.
CREATE TABLE IF NOT EXISTS p2p_merchant_tiers (
  level                INT PRIMARY KEY,
  name                 TEXT NOT NULL,
  max_trade_usd        NUMERIC(38,18) NOT NULL CHECK (max_trade_usd > 0),
  daily_volume_usd     NUMERIC(38,18) NOT NULL CHECK (daily_volume_usd > 0),
  min_completed_trades INT NOT NULL DEFAULT 0,
  min_completion_rate  NUMERIC(5,4) NOT NULL DEFAULT 0
);

INSERT INTO p2p_merchant_tiers (level, name, max_trade_usd, daily_volume_usd, min_completed_trades, min_completion_rate) VALUES
  (1, 'Bronze', 1000,   5000,   0,   0),
  (2, 'Silver', 10000,  50000,  25,  0.90),
  (3, 'Gold',   100000, 500000, 100, 0.95)
ON CONFLICT (level) DO NOTHING;

-- A merchant application/decision per user. Verified merchants get a badge on
-- their offers and tier-scaled trading limits.
CREATE TABLE IF NOT EXISTS p2p_merchants (
  user_id       UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  business_name TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','verified','rejected','revoked')),
  tier          INT NOT NULL DEFAULT 1 REFERENCES p2p_merchant_tiers(level),
  note          TEXT NOT NULL DEFAULT '',
  applied_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  decided_by    UUID REFERENCES users(id),
  decided_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_p2p_merchants_status ON p2p_merchants(status) WHERE status = 'pending';

-- ============ Crypto cards (platform-issued virtual cards) ============

-- Virtual cards spend from the user's USD internal wallet account; top-ups
-- convert crypto -> USD through the convert engine. pan_hash is SHA-256 of
-- the full PAN — the full number is returned exactly once at issuance.
CREATE TABLE IF NOT EXISTS cards (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label             TEXT NOT NULL DEFAULT 'Virtual card',
  pan_hash          TEXT NOT NULL UNIQUE,
  pan_last4         TEXT NOT NULL,
  cvv_hash          TEXT NOT NULL,
  expiry_month      INT NOT NULL,
  expiry_year       INT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active','frozen','terminated')),
  daily_limit_usd   NUMERIC(38,18) NOT NULL DEFAULT 1000 CHECK (daily_limit_usd > 0),
  monthly_limit_usd NUMERIC(38,18) NOT NULL DEFAULT 5000 CHECK (monthly_limit_usd > 0),
  created_at        TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_cards_user ON cards(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS card_transactions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  card_id     UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
  merchant    TEXT NOT NULL,
  amount_usd  NUMERIC(38,18) NOT NULL CHECK (amount_usd > 0),
  kind        TEXT NOT NULL CHECK (kind IN ('purchase','refund')),
  status      TEXT NOT NULL DEFAULT 'captured'
              CHECK (status IN ('captured','declined','reversed')),
  ledger_tx   UUID,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_card_tx_card ON card_transactions(card_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_card_tx_spend ON card_transactions(card_id, created_at)
  WHERE kind = 'purchase' AND status = 'captured';

-- ============ Social parity ============

-- Extended post reactions (likes stay as their own table for compatibility;
-- a 'like' reaction is mirrored into both).
CREATE TABLE IF NOT EXISTS post_reactions (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  reaction   TEXT NOT NULL CHECK (reaction IN ('like','love','haha','wow','sad','angry')),
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, post_id)
);
CREATE INDEX IF NOT EXISTS idx_post_reactions_post ON post_reactions(post_id);

-- One pinned post per profile.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS pinned_post_id UUID REFERENCES posts(id) ON DELETE SET NULL;

-- Full edit history: every edit archives the previous body.
CREATE TABLE IF NOT EXISTS post_edits (
  id        BIGSERIAL PRIMARY KEY,
  post_id   UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  old_body  TEXT NOT NULL,
  edited_at TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_post_edits_post ON post_edits(post_id, edited_at DESC);

-- Feeling/activity + location check-in metadata, and scheduled publishing.
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS feeling    TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS location   TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS publish_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_posts_scheduled ON posts(publish_at)
  WHERE publish_at IS NOT NULL AND deleted_at IS NULL;

-- People tagging on posts.
CREATE TABLE IF NOT EXISTS post_tags (
  post_id UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_post_tags_user ON post_tags(user_id);

-- Photo albums: named collections of the owner's own media posts.
CREATE TABLE IF NOT EXISTS albums (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_albums_owner ON albums(owner_id, created_at DESC);

CREATE TABLE IF NOT EXISTS album_items (
  album_id UUID NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  post_id  UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  position INT NOT NULL DEFAULT 0,
  PRIMARY KEY (album_id, post_id)
);

-- Chat themes (per conversation) + per-chat nicknames (per member).
ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS theme TEXT NOT NULL DEFAULT '';
ALTER TABLE conversation_members
  ADD COLUMN IF NOT EXISTS nickname TEXT NOT NULL DEFAULT '';

-- Event reminders: created on RSVP, delivered by the scheduler one hour
-- before the event starts.
CREATE TABLE IF NOT EXISTS event_reminders (
  event_id  UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  remind_at TIMESTAMPTZ NOT NULL,
  sent_at   TIMESTAMPTZ,
  PRIMARY KEY (event_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_event_reminders_due ON event_reminders(remind_at) WHERE sent_at IS NULL;

-- The built-in finance role gains the new permissions; superadmin already
-- holds the '*' wildcard.
UPDATE admin_role_defs
SET permissions = array_cat(permissions, ARRAY['merchants.review','cards.manage','transfers.review'])
WHERE name = 'finance' AND NOT (permissions @> ARRAY['merchants.review']);

INSERT INTO p2p_payment_methods (country_iso, name, kind) VALUES
  ('US', 'Zelle', 'ewallet'),
  ('US', 'Venmo', 'ewallet'),
  ('US', 'Cash App', 'ewallet'),
  ('US', 'Bank transfer', 'bank_transfer'),
  ('US', 'Cash in person', 'cash')
ON CONFLICT DO NOTHING;

COMMIT;
