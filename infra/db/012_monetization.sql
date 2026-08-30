-- 012_monetization.sql — fan subscriptions, tips, and gifts. All money moves
-- through the existing double-entry ledger (wallet_accounts + ledger_entries)
-- in the internal USD asset; creator_earnings records the income events.
BEGIN;

-- Creator-defined subscription tiers (fan -> creator recurring support).
CREATE TABLE IF NOT EXISTS subscription_tiers (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  creator_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  perks       TEXT DEFAULT '',
  price_usd   NUMERIC(18, 2) NOT NULL CHECK (price_usd > 0),
  active      BOOLEAN NOT NULL DEFAULT TRUE,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tiers_creator ON subscription_tiers(creator_id) WHERE active;

CREATE TABLE IF NOT EXISTS subscriptions (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tier_id      UUID NOT NULL REFERENCES subscription_tiers(id) ON DELETE CASCADE,
  subscriber_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  creator_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','cancelled','expired')),
  current_period_start TIMESTAMPTZ NOT NULL DEFAULT now(),
  current_period_end   TIMESTAMPTZ NOT NULL,
  created_at   TIMESTAMPTZ DEFAULT now(),
  cancelled_at TIMESTAMPTZ,
  UNIQUE (tier_id, subscriber_id)
);
CREATE INDEX IF NOT EXISTS idx_subs_creator ON subscriptions(creator_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_subs_renew ON subscriptions(status, current_period_end) WHERE status = 'active';

-- One-off tips (fan -> creator) and gift catalog.
CREATE TABLE IF NOT EXISTS tips (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_user  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  amount_usd NUMERIC(18, 2) NOT NULL CHECK (amount_usd > 0),
  post_id    UUID REFERENCES posts(id) ON DELETE SET NULL,
  message    TEXT DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now(),
  CHECK (from_user <> to_user)
);
CREATE INDEX IF NOT EXISTS idx_tips_to ON tips(to_user, created_at DESC);

CREATE TABLE IF NOT EXISTS gift_catalog (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name       TEXT NOT NULL UNIQUE,
  emoji      TEXT NOT NULL,
  price_usd  NUMERIC(18, 2) NOT NULL CHECK (price_usd > 0),
  active     BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ DEFAULT now()
);

INSERT INTO gift_catalog (name, emoji, price_usd) VALUES
  ('Rose', '🌹', 0.99), ('Coffee', '☕', 1.99), ('Heart', '❤️', 2.99),
  ('Star', '⭐', 4.99), ('Crown', '👑', 9.99), ('Rocket', '🚀', 19.99),
  ('Diamond', '💎', 49.99), ('Galaxy', '🌌', 99.99)
ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS gift_sends (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  gift_id    UUID NOT NULL REFERENCES gift_catalog(id),
  from_user  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  post_id    UUID REFERENCES posts(id) ON DELETE SET NULL,
  amount_usd NUMERIC(18, 2) NOT NULL,   -- snapshot of catalog price at send time
  created_at TIMESTAMPTZ DEFAULT now(),
  CHECK (from_user <> to_user)
);
CREATE INDEX IF NOT EXISTS idx_gifts_to ON gift_sends(to_user, created_at DESC);

COMMIT;
