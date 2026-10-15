CREATE TABLE IF NOT EXISTS nodes (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    target TEXT NOT NULL CHECK (length(trim(target)) BETWEEN 1 AND 2048),
    probe_type TEXT NOT NULL CHECK (probe_type IN ('http', 'tcp')),
    interval_seconds INTEGER NOT NULL DEFAULT 10 CHECK (interval_seconds BETWEEN 5 AND 3600),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown', 'healthy', 'degraded', 'unhealthy')),
    last_checked_at TIMESTAMPTZ,
    last_latency_ms DOUBLE PRECISION,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_nodes_enabled ON nodes(enabled);

CREATE TABLE IF NOT EXISTS metric_samples (
    id BIGSERIAL PRIMARY KEY,
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    checked_at TIMESTAMPTZ NOT NULL,
    success BOOLEAN NOT NULL,
    latency_ms DOUBLE PRECISION,
    error TEXT
);
CREATE INDEX IF NOT EXISTS idx_metric_samples_node_time ON metric_samples(node_id, checked_at DESC);

CREATE TABLE IF NOT EXISTS incidents (
    id UUID PRIMARY KEY,
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('open', 'resolved')),
    severity TEXT NOT NULL CHECK (severity IN ('warning', 'critical')),
    title TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '',
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_incidents_opened ON incidents(status, opened_at DESC);
