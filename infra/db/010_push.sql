-- 010_push.sql — push notifications: Web Push (RFC 8291/8292) subscriptions,
-- native device tokens (FCM/APNs), and the outbound push queue.
BEGIN;

-- One row per subscribed device/browser. For Web Push, endpoint is the push
-- service URL and p256dh/auth are the subscription keys. For native devices,
-- endpoint is the provider (fcm|apns) token and platform says which gateway.
CREATE TABLE IF NOT EXISTS push_subscriptions (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform    TEXT NOT NULL CHECK (platform IN ('web','android','ios','desktop')),
  endpoint    TEXT NOT NULL,            -- web: push endpoint URL; native: device token
  p256dh      TEXT DEFAULT '',          -- web push public key (base64url)
  auth_secret TEXT DEFAULT '',          -- web push auth secret (base64url)
  user_agent  TEXT DEFAULT '',
  created_at  TIMESTAMPTZ DEFAULT now(),
  last_used_at TIMESTAMPTZ DEFAULT now(),
  UNIQUE (user_id, endpoint)
);
CREATE INDEX IF NOT EXISTS idx_push_subs_user ON push_subscriptions(user_id);

-- Outbound push queue, drained by the push worker (SKIP LOCKED, multi-node safe).
CREATE TABLE IF NOT EXISTS push_queue (
  id          BIGSERIAL PRIMARY KEY,
  user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind        TEXT NOT NULL,            -- message, mention, like, follow, ...
  title       TEXT NOT NULL DEFAULT '',
  body        TEXT NOT NULL DEFAULT '',
  data        JSONB NOT NULL DEFAULT '{}',
  status      TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sent','failed')),
  attempts    INT NOT NULL DEFAULT 0,
  created_at  TIMESTAMPTZ DEFAULT now(),
  sent_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_push_queue_status ON push_queue(status) WHERE status = 'queued';

COMMIT;
