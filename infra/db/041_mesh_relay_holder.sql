-- Track the last authenticated relay that holds a store-and-forward packet.
-- This lets relay quotas apply to actual queued ciphertext rather than to a
-- caller-controlled device id.
ALTER TABLE mesh_packets
  ADD COLUMN IF NOT EXISTS relay_device UUID REFERENCES mesh_devices(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_mesh_packets_relay_state
  ON mesh_packets(relay_device, state);
