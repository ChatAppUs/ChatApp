-- 009_platform_tokens.sql — self-built OTP support + platform wallet tokens
BEGIN;

-- Salted OTP codes (self-built OTP engine).
ALTER TABLE phone_verifications ADD COLUMN IF NOT EXISTS salt TEXT;

-- Channels (Telegram-style broadcast conversations) + public live discovery.
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS is_channel BOOLEAN DEFAULT FALSE;

-- Our own SMS delivery queue, drained by the ChatApp carrier-gateway daemon
-- (SMPP interconnect we operate; no third-party verification service).
CREATE TABLE IF NOT EXISTS sms_outbox (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  phone_e164  TEXT NOT NULL,
  message     TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'queued',  -- queued | sent | failed
  attempts    INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ DEFAULT now(),
  sent_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sms_outbox_status ON sms_outbox(status) WHERE status = 'queued';

-- Platform tokens for the built-in multichain wallet. Managed exclusively by
-- superadmin/finance through the admin system; the user wallet lists only
-- enabled tokens from this table.
CREATE TABLE IF NOT EXISTS platform_tokens (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  symbol           TEXT NOT NULL,
  name             TEXT NOT NULL,
  chain            TEXT NOT NULL,
  contract_address TEXT,               -- NULL for native coins
  decimals         INT NOT NULL DEFAULT 18,
  logo_url         TEXT,
  is_native        BOOLEAN NOT NULL DEFAULT false,
  enabled          BOOLEAN NOT NULL DEFAULT true,
  added_by         UUID REFERENCES users(id),
  created_at       TIMESTAMPTZ DEFAULT now(),
  UNIQUE(symbol, chain)
);

INSERT INTO platform_tokens (symbol, name, chain, decimals, is_native) VALUES
  ('BTC',  'Bitcoin',        'bitcoin',  8,  true),
  ('ETH',  'Ether',          'ethereum', 18, true),
  ('USDT', 'Tether USD',     'ethereum', 6,  false),
  ('USDT', 'Tether USD',     'tron',     6,  false),
  ('USDT', 'Tether USD',     'bsc',      18, false),
  ('USDT', 'Tether USD',     'polygon',  6,  false),
  ('USDC', 'USD Coin',       'ethereum', 6,  false),
  ('USDC', 'USD Coin',       'polygon',  6,  false),
  ('USDC', 'USD Coin',       'solana',   6,  false),
  ('USDC', 'USD Coin',       'base',     6,  false),
  ('SOL',  'Solana',         'solana',   9,  true),
  ('MATIC','Polygon',        'polygon',  18, true),
  ('BNB',  'BNB',            'bsc',      18, true),
  ('USD',  'US Dollar (internal ledger)', 'internal', 2, true)
ON CONFLICT (symbol, chain) DO NOTHING;

COMMIT;
