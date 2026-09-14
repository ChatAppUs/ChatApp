-- 039 — Feature flags + controlled experiments (§74/§75) and the QoE /
-- call-quality telemetry plane (§72/§73). Flags are evaluated with a stable
-- FNV-1a bucket per (user, flag) so a given user always lands in the same
-- variant; rollouts support percentage, region and platform gating.
BEGIN;

CREATE TABLE IF NOT EXISTS feature_flags (
  key         TEXT PRIMARY KEY,
  description TEXT NOT NULL DEFAULT '',
  enabled     BOOLEAN NOT NULL DEFAULT TRUE,
  rollout_pct INT NOT NULL DEFAULT 0 CHECK (rollout_pct BETWEEN 0 AND 100),
  regions     TEXT[] NOT NULL DEFAULT '{}',
  platforms   TEXT[] NOT NULL DEFAULT '{}',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS experiments (
  key         TEXT PRIMARY KEY,
  flag_key    TEXT NOT NULL REFERENCES feature_flags(key) ON DELETE CASCADE,
  description TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Video QoE events (§72): startup, buffering, failures, resolution/bitrate.
CREATE TABLE IF NOT EXISTS video_qoe_events (
  id               BIGSERIAL PRIMARY KEY,
  user_id          UUID REFERENCES users(id) ON DELETE SET NULL,
  video_id         TEXT NOT NULL DEFAULT '',
  startup_ms       INT,
  buffer_ms        INT,
  buffering_events INT,
  playback_failed  BOOLEAN NOT NULL DEFAULT FALSE,
  resolution       TEXT,
  bitrate_kbps     INT,
  completed        BOOLEAN NOT NULL DEFAULT FALSE,
  watch_ms         INT,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_qoe_created ON video_qoe_events(created_at DESC);

-- Call quality events (§73): packet loss, jitter, RTT, bitrate, frame rate.
CREATE TABLE IF NOT EXISTS call_quality_events (
  id              BIGSERIAL PRIMARY KEY,
  user_id         UUID REFERENCES users(id) ON DELETE SET NULL,
  room_id         TEXT NOT NULL DEFAULT '',
  packet_loss_pct REAL,
  jitter_ms       REAL,
  rtt_ms          REAL,
  bitrate_kbps    INT,
  frame_rate      INT,
  resolution      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_callqoe_created ON call_quality_events(created_at DESC);

-- The platform operator role definition for flag/telemetry management.
INSERT INTO admin_role_defs (name, description, permissions, built_in)
VALUES ('platform', 'Feature flags, experiments and media telemetry',
        ARRAY['platform.manage'], FALSE)
ON CONFLICT (name) DO NOTHING;

COMMIT;
