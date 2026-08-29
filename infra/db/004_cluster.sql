-- 004_cluster.sql — cluster engine: node registry for global expansion
BEGIN;

CREATE TABLE IF NOT EXISTS cluster_nodes (
    node_id      TEXT PRIMARY KEY,
    region       TEXT NOT NULL,               -- e.g. us-east, eu-west, ap-south
    api_url      TEXT NOT NULL,               -- public base URL of this node
    media_url    TEXT NOT NULL DEFAULT '',
    weight       INT NOT NULL DEFAULT 100,    -- relative traffic weight
    capacity     INT NOT NULL DEFAULT 10000,  -- max concurrent connections
    load         INT NOT NULL DEFAULT 0,      -- current WS connections
    status       TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','draining','down')),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cluster_nodes_region ON cluster_nodes (region, status);

COMMIT;
