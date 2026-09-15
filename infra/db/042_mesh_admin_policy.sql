-- Admin control of mesh features (§110).
--
-- A single-row operational policy table lets administrators configure the
-- offline mesh without gaining any ability to decrypt or rewrite mesh
-- messages. Allowed controls are limited to operational policy:
--   - feature enablement (mesh_enabled)
--   - version requirements (min_protocol_version)
--   - abuse limits (max_payload_bytes, max_ttl, relay_quota_bytes)
--   - network protocol deprecation (deprecated_transports)
--
-- Administrators never hold mesh keys and never see plaintext; this table
-- only gates admission, sizing and transport eligibility. Every change is
-- audit-logged by the API handler.

CREATE TABLE IF NOT EXISTS mesh_admin_policy (
  id                    SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1), -- singleton row
  mesh_enabled          BOOLEAN NOT NULL DEFAULT true,
  min_protocol_version  INT     NOT NULL DEFAULT 1,
  max_payload_bytes     BIGINT  NOT NULL DEFAULT 2097152,  -- 2 MiB (matches meshMaxPayloadBytes)
  max_ttl               INT     NOT NULL DEFAULT 16,       -- matches the send-path TTL ceiling
  relay_quota_bytes     BIGINT  NOT NULL DEFAULT 10485760, -- 10 MiB default relay quota
  deprecated_transports TEXT[]  NOT NULL DEFAULT '{}',
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by            UUID
);

-- Seed the singleton row so reads never hit an empty table.
INSERT INTO mesh_admin_policy (id)
VALUES (1)
ON CONFLICT (id) DO NOTHING;
