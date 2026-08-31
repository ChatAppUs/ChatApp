-- Gap pack 5: own KYC auto-verification pipeline, true ad revenue-share
-- accounting, and persistent drop-in call rooms (Messenger Rooms parity).
-- Pay-in-chat reuses messages.kind/entities (019) — no schema change needed.

-- ---------- Own KYC pipeline ----------
-- The submission now persists the document/selfie it was filed with and the
-- ML service's automated verification result (score + per-check breakdown).
-- Auto-verification only fires when the ML score clears the threshold AND
-- sanctions screening came back clean; anything else stays pending for an
-- admin reviewer.
ALTER TABLE kyc_submissions
  ADD COLUMN IF NOT EXISTS full_name     TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS country       TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS doc_type      TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS doc_number    TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS doc_image_url TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS selfie_url    TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS auto_score    NUMERIC(5,4),
  ADD COLUMN IF NOT EXISTS auto_checks   JSONB;

-- ---------- True ad revenue share ----------
-- An impression/click can be attributed to the creator content it was
-- served against. handleServeAd credits the placement post's author the
-- creator share of the impression cost from the platform treasury in the
-- same transaction that records the event and decrements the budget.
ALTER TABLE ad_events
  ADD COLUMN IF NOT EXISTS placement_post_id UUID REFERENCES posts(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_ad_events_placement ON ad_events(placement_post_id)
  WHERE placement_post_id IS NOT NULL;

-- ---------- Persistent drop-in rooms ----------
-- Messenger Rooms parity: a stable, shareable link that any authenticated
-- user can join — no conversation membership or per-user invite required.
-- The SFU room ("dropin-<slug>") is registered lazily on the first join and
-- stays addressable until the host ends the room.
CREATE TABLE IF NOT EXISTS dropin_rooms (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug       TEXT NOT NULL UNIQUE,
  title      TEXT NOT NULL DEFAULT '',
  host_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ended_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_dropin_rooms_host ON dropin_rooms(host_id, created_at DESC);
