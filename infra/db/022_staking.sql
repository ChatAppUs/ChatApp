-- 022_staking.sql - native staking system with admin-managed assets/APY,
-- treasury custody of pooled stakes, and the live crypto price feed.
-- No external provider is involved: rewards are settled from the platform
-- treasury ledger (double-entry), and prices come from CoinGecko with an
-- on-platform order-book fallback plus manual admin overrides.
BEGIN;

ALTER TABLE platform_tokens
  ADD COLUMN IF NOT EXISTS coingecko_id TEXT NOT NULL DEFAULT '';

-- Latest known USD price per listed asset+chain. Sources: 'coingecko'
-- (worker), 'orderbook' (our own P2P market), 'admin' (manual override;
-- overrides take precedence over the worker until changed again).
CREATE TABLE IF NOT EXISTS crypto_prices (
  asset      TEXT NOT NULL,
  chain      TEXT NOT NULL,
  price_usd  NUMERIC(38,10) NOT NULL CHECK (price_usd > 0),
  source     TEXT NOT NULL CHECK (source IN ('coingecko','orderbook','admin')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (asset, chain)
);

-- Stakable assets. APY is annual % with simple (non-compounding) rewards.
CREATE TABLE IF NOT EXISTS staking_assets (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset          TEXT NOT NULL,
  chain          TEXT NOT NULL,
  apy            NUMERIC(9,2) NOT NULL CHECK (apy >= 0 AND apy <= 1000),
  min_amount     NUMERIC(38,18) NOT NULL DEFAULT 0,
  durations_days INT[] NOT NULL DEFAULT '{7,30,90,180,365}' CHECK (array_length(durations_days,1) > 0),
  active         BOOLEAN NOT NULL DEFAULT true,
  created_by     UUID REFERENCES users(id),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (asset, chain),
  FOREIGN KEY (asset, chain) REFERENCES platform_tokens (symbol, chain) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS idx_staking_assets_active ON staking_assets(active) WHERE active;

-- Immutable audit of every APY change (positions lock their APY at stake
-- time, so a change never retroactively affects open positions).
CREATE TABLE IF NOT EXISTS staking_rates (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id  UUID NOT NULL REFERENCES staking_assets(id) ON DELETE CASCADE,
  apy       NUMERIC(9,2) NOT NULL,
  set_by    UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Positions. Status flow: active -> (at maturity) unlock_requested -> closed.
-- Before ends_at the unlock endpoint returns 403; after maturity it either
-- settles immediately (treasury liquid) or queues (unlock_requested) until a
-- treasury "in" move / manual settle makes it liquid.
CREATE TABLE IF NOT EXISTS stake_positions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  asset       TEXT NOT NULL,
  chain       TEXT NOT NULL,
  amount      NUMERIC(38,18) NOT NULL CHECK (amount > 0),
  apy         NUMERIC(9,2) NOT NULL,
  duration_days INT NOT NULL CHECK (duration_days > 0),
  started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  ends_at     TIMESTAMPTZ NOT NULL,
  status      TEXT NOT NULL DEFAULT 'active'
              CHECK (status IN ('active','unlock_requested','closed')),
  reward_paid NUMERIC(38,18),
  lock_tx     UUID,
  unlock_tx   UUID,
  unlock_requested_at TIMESTAMPTZ,
  closed_at   TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_stake_positions_user ON stake_positions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_stake_positions_queue ON stake_positions(status)
  WHERE status = 'unlock_requested';

-- Superadmin treasury movements: pooled stake principal can be deployed
-- externally (e.g. buying stock) and later redeposited. Every move is
-- ledgered against the treasury's wallet account; unlock queue settlement
-- checks liquidity from the very same ledger.
CREATE TABLE IF NOT EXISTS staking_treasury_moves (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  admin_id   UUID NOT NULL REFERENCES users(id),
  asset      TEXT NOT NULL,
  chain      TEXT NOT NULL,
  amount     NUMERIC(38,18) NOT NULL CHECK (amount > 0),
  direction  TEXT NOT NULL CHECK (direction IN ('out','in')),
  purpose    TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_staking_moves_asset ON staking_treasury_moves(asset, chain, created_at DESC);

-- finance role gains staking management; superadmin '*' always applies.
UPDATE admin_role_defs
  SET permissions = array_append(permissions, 'staking.manage')
  WHERE name = 'finance' AND NOT ('staking.manage' = ANY(permissions));

COMMIT;
