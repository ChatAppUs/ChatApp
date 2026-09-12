-- Offline multi-hop device mesh (Briar-style store-and-forward).
-- A mesh packet is an encrypted, authenticated payload that may be relayed
-- through intermediate devices before reaching its destination. Relays carry
-- ciphertext only; they never decrypt message content. Delivery is
-- best-effort and delay-tolerant: packets persist in a bounded queue until a
-- route to the destination becomes available, then expire.

CREATE TABLE IF NOT EXISTS mesh_devices (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       UUID REFERENCES users(id) ON DELETE CASCADE,  -- NULL for pure relay nodes
  device_key    TEXT NOT NULL,          -- public identity (authenticated device id)
  transport     TEXT NOT NULL DEFAULT 'internet', -- bluetooth | ble | wifi_direct | local_wifi | internet
  last_seen_at  TIMESTAMPTZ DEFAULT now(),
  created_at    TIMESTAMPTZ DEFAULT now(),
  UNIQUE (device_key)
);
CREATE INDEX IF NOT EXISTS idx_mesh_devices_user ON mesh_devices(user_id);
CREATE INDEX IF NOT EXISTS idx_mesh_devices_seen ON mesh_devices(last_seen_at);

-- Store-and-forward packet queue. Relays hold encrypted packets until a route
-- to the destination appears, then forward. TTL bounds lifetime; seen_ids
-- deduplicate against loops.
CREATE TABLE IF NOT EXISTS mesh_packets (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  packet_id     TEXT NOT NULL,          -- sender-generated unique packet id (dedup)
  sender_device UUID NOT NULL REFERENCES mesh_devices(id),
  dest_device   UUID NOT NULL REFERENCES mesh_devices(id),
  payload       BYTEA NOT NULL,         -- encrypted ciphertext only
  ttl           INT NOT NULL DEFAULT 8, -- hop limit
  hops          INT NOT NULL DEFAULT 0,
  state         TEXT NOT NULL DEFAULT 'queued', -- queued | relaying | delivered | expired | failed
  created_at    TIMESTAMPTZ DEFAULT now(),
  delivered_at  TIMESTAMPTZ,
  expires_at    TIMESTAMPTZ DEFAULT now() + interval '7 days',
  UNIQUE (packet_id)
);
CREATE INDEX IF NOT EXISTS idx_mesh_packets_dest ON mesh_packets(dest_device, state);
CREATE INDEX IF NOT EXISTS idx_mesh_packets_pid ON mesh_packets(packet_id);

-- Recently-seen packet ids for duplicate suppression (bounded).
CREATE TABLE IF NOT EXISTS mesh_seen_packets (
  packet_id TEXT PRIMARY KEY,
  seen_at   TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mesh_seen_at ON mesh_seen_packets(seen_at);

-- Relay consent / resource policy per device.
CREATE TABLE IF NOT EXISTS mesh_relay_policy (
  device_id     UUID PRIMARY KEY REFERENCES mesh_devices(id) ON DELETE CASCADE,
  relay_enabled BOOLEAN NOT NULL DEFAULT false,
  relay_mode    TEXT NOT NULL DEFAULT 'off', -- off | active | charging | approved
  storage_quota BIGINT NOT NULL DEFAULT 10485760, -- 10 MiB default
  created_at    TIMESTAMPTZ DEFAULT now(),
  updated_at    TIMESTAMPTZ DEFAULT now()
);
