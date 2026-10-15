# NetPulse backend

Go API + PostgreSQL for HTTP/TCP probes, incidents, user authentication, remote Linux agents, and WebSocket metric ingestion.

## Requirements

- Go 1.25+
- PostgreSQL 17+
- Linux for `cmd/agent` (reads `/proc` and uses `statfs`)

## Run locally

```bash
cp .env.example .env
# Generate a real secret and add it to .env:
sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(openssl rand -hex 32)|" .env
set -a; source .env; set +a

docker compose up -d postgres
psql "$DATABASE_URL" -f migrations/001_init.sql
psql "$DATABASE_URL" -f migrations/002_auth_agents.sql
go run ./cmd/api
```

If `001_init.sql` has already been applied, apply only `002_auth_agents.sql`. The API requires `JWT_SECRET` to be at least 32 bytes. The `.env` file is not loaded automatically by Go, so export it as shown above or use your preferred env loader.

## Authentication

Register (password must be at least 12 bytes):

```bash
curl -sS http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"change-this-to-a-long-password"}'
```

The response contains a short-lived 15-minute access token and a 30-day refresh token. Keep the refresh token secret; store only its hash server-side. Refresh tokens rotate on every refresh and cannot be reused.

```bash
curl -sS http://localhost:8080/api/v1/auth/refresh \
  -H 'Content-Type: application/json' \
  -d '{"refresh_token":"YOUR_REFRESH_TOKEN"}'
```

Protected API calls require `Authorization: Bearer ACCESS_TOKEN`. Nodes and incidents are scoped to the authenticated user. Existing nodes created before migration `002` have no owner and will not appear for newly registered users; recreate them under the appropriate account.

## Remote agent enrollment

### 1. Create an enrollment token

```bash
curl -sS http://localhost:8080/api/v1/agents \
  -H 'Authorization: Bearer ACCESS_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{"name":"production-web-01"}'
```

Copy the `enrollment_token` from the response. It expires after 15 minutes and can be used once.

### 2. Exchange it on the remote Linux server

```bash
curl -sS http://API_HOST:8080/api/v1/agents/enroll \
  -H 'Content-Type: application/json' \
  -d '{"enrollment_token":"ONE_TIME_ENROLLMENT_TOKEN","hostname":"production-web-01"}'
```

The response includes `agent_id` and `agent_secret`. The secret is returned once; store it securely. Do not put it in source control or command history on a shared machine. For a local secret file, use owner-only permissions (`chmod 600`).

### 3. Build and run the Linux agent

Build on a machine with Go 1.25+ for the target architecture:

```bash
go build -o netpulse-agent ./cmd/agent
```

Run it on the remote host. Use `wss://` behind a TLS-enabled reverse proxy in production; `ws://` is only for trusted local development networks.

```bash
export NETPULSE_WS_URL='wss://netpulse.example.com/api/v1/agents/ws'
export NETPULSE_AGENT_SECRET='AGENT_SECRET_FROM_ENROLLMENT'
export NETPULSE_INTERVAL='10s'
./netpulse-agent
```

The agent keeps a WebSocket connection open, sends CPU, memory, root-filesystem, and network throughput metrics, and reconnects after connection errors. The server validates and limits incoming messages, stores the metrics, and marks agents offline after 45 seconds without a heartbeat/metric update. Revoking an agent immediately blocks subsequent metric writes and future connections.

## API routes

### Auth

- `POST /api/v1/auth/register`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`

### Existing probe monitoring (access token required)

- `GET /api/v1/nodes`
- `POST /api/v1/nodes`
- `GET /api/v1/nodes/{id}`
- `DELETE /api/v1/nodes/{id}`
- `GET /api/v1/nodes/{id}/metrics?limit=100`
- `GET /api/v1/incidents?status=open&limit=100`

### Agents

- `POST /api/v1/agents` — create a pending agent and one-time enrollment token (access token required)
- `POST /api/v1/agents/enroll` — exchange the one-time token for the agent secret
- `GET /api/v1/agents` — list your agents (access token required)
- `GET /api/v1/agents/{id}/metrics?limit=100` — recent metrics for your agent (access token required)
- `DELETE /api/v1/agents/{id}` — revoke an agent (access token required)
- `GET /api/v1/agents/ws` — agent WebSocket; authenticate with `Authorization: Bearer AGENT_SECRET`

A metrics WebSocket message has this shape:

```json
{
  "type": "metrics",
  "timestamp": "2026-10-14T19:00:00Z",
  "hostname": "production-web-01",
  "data": {
    "cpu_percent": 24.6,
    "memory_used_bytes": 4294967296,
    "memory_total_bytes": 8589934592,
    "disk_used_bytes": 21474836480,
    "disk_total_bytes": 53687091200,
    "network_rx_bytes_per_sec": 102400,
    "network_tx_bytes_per_sec": 51200
  }
}
```

## Operational notes

- `GET /healthz` is liveness; `GET /readyz` checks PostgreSQL.
- Apply both SQL migrations before starting the API.
- Configure TLS, firewall rules, backups, and rate limiting before exposing the API publicly. Authentication endpoints currently need a reverse-proxy or application-level rate limit for production use.
- The agent supports Linux and reports aggregate network traffic excluding loopback, root filesystem usage, CPU usage, and memory usage.

## Docker deployment

For the portable full-stack deployment, use the Compose file at the repository root:

```bash
cd ..
docker compose up --build -d
```

The root Compose stack includes PostgreSQL, the API, the frontend, and an isolated testserver controller. The API does not call the Docker CLI or mount the host Docker socket. See the root `README.md` for setup, ports, migrations, and security notes.
