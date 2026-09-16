-- Gap pack 10: closes the code-verifiable gaps surfaced by
-- ChatApp_Competitor_Comparison.md that had no implementation at all:
--   * global search with ranked posts/people/topics/hashtags (audience & discovery)
--   * contact import/discovery with server-side peppered phone hashing (imo)
--   * moderation appeals (public-conversation "appeals" gate)
--   * copyright / DMCA takedown notices + counter-notices (business & safety)
--   * legal-request register for law-enforcement/government intake (business & safety)
--   * age gating: date of birth + safety mode (age/safety controls)
--   * support tickets with staff replies (creator/customer support)

-- ---- Age gating ----
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS date_of_birth DATE,
  ADD COLUMN IF NOT EXISTS safety_mode TEXT NOT NULL DEFAULT 'standard'
    CHECK (safety_mode IN ('standard','minor'));

-- ---- Contact discovery: privacy-preserving phone matching ----
-- The server stores pepper(E164) digests, never raw numbers. The first import
-- backfills digests for every registered user that has a phone number on file.
CREATE TABLE IF NOT EXISTS contact_discovery_hashes (
  id         BIGSERIAL PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  phone_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, phone_hash)
);
CREATE INDEX IF NOT EXISTS idx_contact_discovery_hash
  ON contact_discovery_hashes (phone_hash);

-- ---- Moderation appeals ----
-- A user (or the author of sanctioned content) can appeal a report decision or
-- any enforcement against a target. Admins approve/reject with a note.
CREATE TABLE IF NOT EXISTS moderation_appeals (
  id            BIGSERIAL PRIMARY KEY,
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_type   TEXT NOT NULL CHECK (target_type IN ('user','post','comment','message','ad')),
  target_id     UUID NOT NULL,
  reason        TEXT NOT NULL,
  status        TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','upheld','rejected')),
  decided_by    UUID REFERENCES users(id) ON DELETE SET NULL,
  decision_note TEXT NOT NULL DEFAULT '',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  decided_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_appeals_status ON moderation_appeals (status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_appeals_user ON moderation_appeals (user_id, created_at DESC);

-- ---- Copyright / DMCA ----
CREATE TABLE IF NOT EXISTS copyright_notices (
  id           BIGSERIAL PRIMARY KEY,
  reporter_id  UUID REFERENCES users(id) ON DELETE SET NULL,
  content_type TEXT NOT NULL CHECK (content_type IN ('post','comment','message','reel')),
  content_id   UUID NOT NULL,
  original_url TEXT NOT NULL,
  description  TEXT NOT NULL,
  good_faith   BOOLEAN NOT NULL DEFAULT FALSE,
  under_penalty BOOLEAN NOT NULL DEFAULT FALSE,
  status       TEXT NOT NULL DEFAULT 'open'
    CHECK (status IN ('open','takedown','restored','rejected','countered')),
  resolution   TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_copyright_status ON copyright_notices (status, created_at DESC);

CREATE TABLE IF NOT EXISTS copyright_counters (
  id                 BIGSERIAL PRIMARY KEY,
  notice_id          BIGINT NOT NULL REFERENCES copyright_notices(id) ON DELETE CASCADE,
  user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  statement          TEXT NOT NULL,
  consent_jurisdiction BOOLEAN NOT NULL DEFAULT FALSE,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---- Legal requests register ----
-- Law-enforcement / government intake: every request, its scope, and its
-- disposition is recorded here for audit. Only admins can read or write.
CREATE TABLE IF NOT EXISTS legal_requests (
  id              BIGSERIAL PRIMARY KEY,
  authority       TEXT NOT NULL,
  request_ref     TEXT NOT NULL,
  kind            TEXT NOT NULL CHECK (kind IN ('preservation','disclosure','removal','other')),
  subject_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  scope           TEXT NOT NULL DEFAULT '',
  status          TEXT NOT NULL DEFAULT 'received'
    CHECK (status IN ('received','acknowledged','fulfilled','partially_fulfilled','rejected')),
  handled_by      UUID REFERENCES users(id) ON DELETE SET NULL,
  notes           TEXT NOT NULL DEFAULT '',
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (authority, request_ref)
);

-- ---- Support tickets ----
CREATE TABLE IF NOT EXISTS support_tickets (
  id          BIGSERIAL PRIMARY KEY,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  category    TEXT NOT NULL DEFAULT 'general'
    CHECK (category IN ('general','creator','billing','safety','bug','feature')),
  subject     TEXT NOT NULL,
  body        TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','answered','resolved','closed')),
  resolution  TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_support_status ON support_tickets (status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_support_user ON support_tickets (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS support_replies (
  id        BIGSERIAL PRIMARY KEY,
  ticket_id BIGINT NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
  author_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  is_staff  BOOLEAN NOT NULL DEFAULT FALSE,
  body      TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_support_replies_ticket ON support_replies (ticket_id, created_at);
