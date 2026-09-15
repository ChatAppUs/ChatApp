-- Bind internet-backed mesh devices to the authenticated subject that registered them.
-- Guest subjects are opaque strings; account subjects are UUID strings. This
-- prevents one authenticated caller from polling or mutating another device's
-- queue merely by guessing its public device key.
ALTER TABLE mesh_devices
  ADD COLUMN IF NOT EXISTS owner_subject TEXT;

UPDATE mesh_devices
SET owner_subject = user_id::text
WHERE owner_subject IS NULL AND user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_mesh_devices_owner_subject
  ON mesh_devices(owner_subject);
UPDATE mesh_devices SET owner_subject = 'legacy_unowned' WHERE owner_subject IS NULL;
