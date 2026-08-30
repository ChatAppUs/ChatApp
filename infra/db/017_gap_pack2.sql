-- 017: gap-closure pack — account safety (trusted contacts recovery, legacy
-- contact, multiple profiles), messaging (chat polls, video notes, live
-- location, pay-in-chat), story composer metadata, reel remix, call
-- recordings + screen-share state, community notes, media moderation,
-- email digests, sanctions screening, own-node chain watcher, P2P-derived rates.

-- ---------- Accounts ----------
CREATE TABLE IF NOT EXISTS trusted_contacts (
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  contact_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, contact_id)
);

-- One-time recovery shares shown to a trusted contact, redeemable by the
-- account owner. N-of-M (2 of 3+) shares unlock a password-reset token.
CREATE TABLE IF NOT EXISTS recovery_shares (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  contact_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash   TEXT NOT NULL,
  expires_at  TIMESTAMPTZ NOT NULL,
  revealed_at TIMESTAMPTZ,
  used_at     TIMESTAMPTZ,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_recovery_shares_user ON recovery_shares(user_id);

CREATE TABLE IF NOT EXISTS legacy_contacts (
  user_id    UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  contact_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now()
);

ALTER TABLE users
  ADD COLUMN IF NOT EXISTS memorialized_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS digest_enabled BOOLEAN NOT NULL DEFAULT TRUE;

-- Multiple profiles per account (personas); the user keeps one identity,
-- each profile carries its own display name/bio/avatar shown on posts.
CREATE TABLE IF NOT EXISTS user_profiles (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  bio        TEXT NOT NULL DEFAULT '',
  avatar_url TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (user_id, name)
);
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS active_profile_id UUID REFERENCES user_profiles(id) ON DELETE SET NULL;

-- ---------- Messaging ----------
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'text',
  ADD COLUMN IF NOT EXISTS payment_id UUID;

CREATE TABLE IF NOT EXISTS message_polls (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  question   TEXT NOT NULL,
  multi      BOOLEAN NOT NULL DEFAULT FALSE,
  closes_at  TIMESTAMPTZ,
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE IF NOT EXISTS message_poll_options (
  id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  poll_id UUID NOT NULL REFERENCES message_polls(id) ON DELETE CASCADE,
  label   TEXT NOT NULL,
  position INT NOT NULL
);
CREATE TABLE IF NOT EXISTS message_poll_votes (
  poll_id   UUID NOT NULL REFERENCES message_polls(id) ON DELETE CASCADE,
  option_id UUID NOT NULL REFERENCES message_poll_options(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (poll_id, option_id, user_id)
);

CREATE TABLE IF NOT EXISTS live_locations (
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  lat             DOUBLE PRECISION NOT NULL,
  lng             DOUBLE PRECISION NOT NULL,
  expires_at      TIMESTAMPTZ NOT NULL,
  updated_at      TIMESTAMPTZ DEFAULT now(),
  PRIMARY KEY (user_id, conversation_id)
);

-- ---------- Stories / reels ----------
ALTER TABLE posts
  ADD COLUMN IF NOT EXISTS story_background TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS story_stickers   JSONB,
  ADD COLUMN IF NOT EXISTS story_music      JSONB,
  ADD COLUMN IF NOT EXISTS remix_of         UUID REFERENCES posts(id) ON DELETE SET NULL;

-- ---------- Calls ----------
CREATE TABLE IF NOT EXISTS call_recordings (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  room_id     TEXT NOT NULL,
  owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  media_url   TEXT NOT NULL,
  duration_s  INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_call_recordings_room ON call_recordings(room_id);

-- ---------- Safety: community notes + media moderation ----------
CREATE TABLE IF NOT EXISTS community_notes (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (post_id, author_id)
);
CREATE TABLE IF NOT EXISTS community_note_votes (
  note_id  UUID NOT NULL REFERENCES community_notes(id) ON DELETE CASCADE,
  user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  helpful  BOOLEAN NOT NULL,
  PRIMARY KEY (note_id, user_id)
);

-- Perceptual/exact hashes of media that moderation has blocked; new uploads
-- are screened against this list by the ML service.
CREATE TABLE IF NOT EXISTS blocked_media_hashes (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  sha256     TEXT NOT NULL DEFAULT '',
  dhash      TEXT NOT NULL DEFAULT '',
  reason     TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_blocked_sha ON blocked_media_hashes(sha256) WHERE sha256 <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_blocked_dhash ON blocked_media_hashes(dhash) WHERE dhash <> '';

CREATE TABLE IF NOT EXISTS media_moderation (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  media_url  TEXT NOT NULL,
  sha256     TEXT NOT NULL DEFAULT '',
  dhash      TEXT NOT NULL DEFAULT '',
  decision   TEXT NOT NULL CHECK (decision IN ('allow','review','block')),
  reason     TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now()
);

-- ---------- Notifications: email digest ----------
CREATE TABLE IF NOT EXISTS email_digests (
  user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  sent_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------- KYC: sanctions screening ----------
CREATE TABLE IF NOT EXISTS sanctions_entries (
  id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source    TEXT NOT NULL,             -- ofac | eu | un | manual
  name      TEXT NOT NULL,
  name_norm TEXT NOT NULL,             -- lowercase, alnum-only
  program   TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (source, name_norm)
);
CREATE INDEX IF NOT EXISTS idx_sanctions_norm ON sanctions_entries(name_norm);

ALTER TABLE kyc_submissions
  ADD COLUMN IF NOT EXISTS screening_hits INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS screened_at    TIMESTAMPTZ;

-- ---------- Own-node chain watcher ----------
CREATE TABLE IF NOT EXISTS chain_watcher_state (
  chain      TEXT PRIMARY KEY,
  cursor     TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chain_deposits (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  chain        TEXT NOT NULL,
  tx_hash      TEXT NOT NULL,
  address      TEXT NOT NULL,
  asset        TEXT NOT NULL,
  amount       NUMERIC(38,18) NOT NULL,
  user_id      UUID REFERENCES users(id) ON DELETE SET NULL,
  credited     BOOLEAN NOT NULL DEFAULT FALSE,
  detected_at  TIMESTAMPTZ DEFAULT now(),
  UNIQUE (chain, tx_hash, address)
);

-- convert_rates can opt into auto-derivation from the P2P order book.
ALTER TABLE convert_rates
  ADD COLUMN IF NOT EXISTS auto_from_p2p BOOLEAN NOT NULL DEFAULT FALSE;
