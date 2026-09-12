-- Content-level trust & safety: per-account abuse marks on write paths.
-- The auth surface already has per-IP token-bucket rate limiting; this table
-- backs per-account duplicate/link-spam defense on posts, comments, messages.
CREATE TABLE IF NOT EXISTS content_abuse_marks (
  id         BIGSERIAL PRIMARY KEY,
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body_hash  TEXT NOT NULL,
  link_count INT  NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_content_abuse_user_time
  ON content_abuse_marks (user_id, created_at DESC);
