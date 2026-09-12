-- LuckyDraw — regulated paid draws (master spec §44-49, feature tree 122).
-- Daily/monthly draws with paid tickets; admin-configured future ticket prices
-- (draft -> approved -> effective, historical rows never mutated); immutable
-- ticket price records; eligibility locking; a frozen participant dataset; an
-- auditable selection process; the unique-user winner rule; prize settlement on
-- the double-entry ledger (prize allocation % explicit per draw); geographic/age
-- controls; and a full governance audit trail. The whole system is disable-able
-- per draw (and per-deployment config).
--
-- Governance: paid entry + random chance + prizes can be regulated. Production
-- activation requires legal classification, licensing, jurisdiction/age rules,
-- published terms, and tax/compliance review. This schema provides the
-- configurable machinery and defaults to conservative gates (KYC verified +
-- allowed countries + min age) but never claims a jurisdiction-approved product.

CREATE TABLE IF NOT EXISTS lucky_draws (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title               TEXT NOT NULL,
  frequency           TEXT NOT NULL DEFAULT 'daily' CHECK (frequency IN ('daily','monthly')),
  sales_open_at       TIMESTAMPTZ NOT NULL,
  sales_close_at      TIMESTAMPTZ NOT NULL,
  status              TEXT NOT NULL DEFAULT 'scheduled'
                      CHECK (status IN ('scheduled','open','sales_closed','selecting','settled','canceled')),
  ticket_price_usd    NUMERIC(38,18) NOT NULL DEFAULT 1.00,
  prize_alloc_pct     NUMERIC(8,4) NOT NULL DEFAULT 85.00,
  operator_fee_pct    NUMERIC(8,4) NOT NULL DEFAULT 10.00,
  max_tickets_per_user INT NOT NULL DEFAULT 50,
  unique_winner       BOOLEAN NOT NULL DEFAULT TRUE,
  min_age             INT NOT NULL DEFAULT 18,
  allowed_countries   TEXT[] NOT NULL DEFAULT '{}',
  winner_count        INT NOT NULL DEFAULT 1,
  disabled            BOOLEAN NOT NULL DEFAULT FALSE,
  prize_pool          NUMERIC(38,18) NOT NULL DEFAULT 0,
  tickets_sold        INT NOT NULL DEFAULT 0,
  created_by          UUID,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  sales_closed_at     TIMESTAMPTZ,
  dataset_frozen_at   TIMESTAMPTZ,
  selected_at         TIMESTAMPTZ,
  settled_at          TIMESTAMPTZ
);

-- Immutable ticket price history: draft -> approved -> superseded. Historical
-- rows are never mutated; only the current approved price is effective.
CREATE TABLE IF NOT EXISTS lucky_draw_prices (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  draw_id       UUID NOT NULL REFERENCES lucky_draws(id) ON DELETE CASCADE,
  price_usd     NUMERIC(38,18) NOT NULL,
  status        TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','superseded')),
  requested_by  UUID,
  approved_by   UUID,
  effective_at  TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Immutable ticket sale records. ticket_number is unique per draw.
CREATE TABLE IF NOT EXISTS lucky_draw_tickets (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  draw_id       UUID NOT NULL REFERENCES lucky_draws(id) ON DELETE CASCADE,
  account_id    UUID NOT NULL,
  ticket_number TEXT NOT NULL,
  price_usd     NUMERIC(38,18) NOT NULL,
  payment_ref   UUID,
  eligibility   TEXT NOT NULL DEFAULT 'eligible' CHECK (eligibility IN ('eligible','ineligible')),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (draw_id, ticket_number)
);
CREATE INDEX IF NOT EXISTS idx_lucky_draw_tickets_draw ON lucky_draw_tickets(draw_id);
CREATE INDEX IF NOT EXISTS idx_lucky_draw_tickets_account ON lucky_draw_tickets(account_id);

-- Winner records. The unique-user winner rule is enforced by the
-- (draw_id, account_id) uniqueness constraint.
CREATE TABLE IF NOT EXISTS lucky_draw_winners (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  draw_id     UUID NOT NULL REFERENCES lucky_draws(id) ON DELETE CASCADE,
  account_id  UUID NOT NULL,
  ticket_id   UUID REFERENCES lucky_draw_tickets(id),
  prize_usd   NUMERIC(38,18) NOT NULL,
  status      TEXT NOT NULL DEFAULT 'selected' CHECK (status IN ('selected','settled')),
  payout_tx   UUID,
  settled_at  TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (draw_id, account_id)
);

-- Governance audit trail for every state transition and admin action.
CREATE TABLE IF NOT EXISTS lucky_draw_audit (
  id          BIGSERIAL PRIMARY KEY,
  draw_id     UUID NOT NULL REFERENCES lucky_draws(id) ON DELETE CASCADE,
  actor_id    UUID,
  action      TEXT NOT NULL,
  detail      JSONB NOT NULL DEFAULT '{}',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_lucky_draw_audit_draw ON lucky_draw_audit(draw_id, created_at DESC);

-- Sequential ticket numbers.
CREATE SEQUENCE IF NOT EXISTS lucky_draw_ticket_seq;

-- finance role gains LuckyDraw management; superadmin '*' always applies.
UPDATE admin_role_defs
  SET permissions = array_append(permissions, 'luckydraw.manage')
  WHERE name = 'finance' AND NOT ('luckydraw.manage' = ANY(permissions));