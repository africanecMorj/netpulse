CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
    token_hash TEXT PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_nodes_user_created ON nodes(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    hostname TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'online', 'offline', 'revoked')),
    secret_hash TEXT,
    secret_revoked_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agents_user_created ON agents(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agents_secret_hash ON agents(secret_hash) WHERE secret_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS agent_enrollments (
    id UUID PRIMARY KEY,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agent_enrollments_expiry ON agent_enrollments(expires_at);

CREATE TABLE IF NOT EXISTS agent_metric_samples (
    id BIGSERIAL PRIMARY KEY,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    collected_at TIMESTAMPTZ NOT NULL,
    cpu_percent DOUBLE PRECISION NOT NULL CHECK (cpu_percent BETWEEN 0 AND 100),
    memory_used_bytes BIGINT NOT NULL CHECK (memory_used_bytes >= 0),
    memory_total_bytes BIGINT NOT NULL CHECK (memory_total_bytes >= 0),
    disk_used_bytes BIGINT NOT NULL CHECK (disk_used_bytes >= 0),
    disk_total_bytes BIGINT NOT NULL CHECK (disk_total_bytes >= 0),
    network_rx_bytes_per_sec BIGINT NOT NULL CHECK (network_rx_bytes_per_sec >= 0),
    network_tx_bytes_per_sec BIGINT NOT NULL CHECK (network_tx_bytes_per_sec >= 0)
);
CREATE INDEX IF NOT EXISTS idx_agent_metrics_agent_time ON agent_metric_samples(agent_id, collected_at DESC);
