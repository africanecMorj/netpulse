# NetPulse — full-stack Docker deployment

NetPulse is a Go API + React/Vite dashboard with PostgreSQL and an isolated test-agent workload.

## Requirements

- Docker Desktop on Windows/macOS, or Docker Engine + Compose plugin on Linux.
- No local Go, Node.js, PostgreSQL, or Docker CLI access from the backend container is required.

## Start everything

From the repository root:

```bash
docker compose up --build -d
```

Open **http://localhost:5173**. Register a user, sign in, open **Agents**, and choose **Launch test server**. The test workload is a prebuilt Compose service; the backend controls its agent process and bounded stress workload over a private Compose network. The backend does not mount or access the host Docker socket.

Check status and logs:

```bash
docker compose ps
docker compose logs -f backend testserver
```

Stop the stack:

```bash
docker compose down
```

PostgreSQL data persists in the `netpulse_pgdata` volume. To remove the stack **and permanently delete its database volume**, run:

```bash
docker compose down -v
```

## Configuration

The Compose file has development defaults so a fresh checkout can start with one command. **Do not expose these defaults to the internet.** Before sharing a deployment or making it reachable to untrusted networks, create a root `.env` file and set strong random secrets:

```bash
cp .env.example .env
openssl rand -hex 32
```

Use one generated hex value for `JWT_SECRET` and another for `NETPULSE_TEST_CONTROL_TOKEN`; choose a strong alphanumeric `POSTGRES_PASSWORD`. Keep `.env` private and never commit it. The example values are placeholders; replace them.

Available overrides include `FRONTEND_PORT`, `API_PORT`, `POSTGRES_PORT`, `POSTGRES_PASSWORD`, `JWT_SECRET`, and `NETPULSE_TEST_CONTROL_TOKEN`.

## Database migrations

For a new database volume, Compose mounts the SQL files into PostgreSQL's `/docker-entrypoint-initdb.d`, so migrations run automatically in filename order. PostgreSQL only runs initialization scripts when the data directory is empty. If you reuse an existing database volume, apply any missing migrations yourself before starting the backend.

## Test server controls

The `testserver` service is started with Compose and remains idle until you click **Launch test server**. The button enrolls a dedicated agent and asks the testserver controller to start the agent process. CPU and memory workloads are bounded to 5 minutes; memory stress is capped at 256 MiB. The container is limited to 2 CPUs, 1 GiB RAM, and 128 processes, drops Linux capabilities, and enables `no-new-privileges`.

The controller is only reachable on the internal Compose network and requires `NETPULSE_TEST_CONTROL_TOKEN`. Browser-facing control endpoints also require an authenticated NetPulse session. The test workload runs in a Linux container, including when Docker Desktop runs it on Windows or macOS.

## Development outside Docker

- Backend: `cd netpulse-backend`, export settings from `.env`, and run `go run ./cmd/api`.
- Frontend: `cd frontend`, run `npm ci && npm run dev`. Vite proxies `/api` to `http://127.0.0.1:8080`.
- The test-server controller is intended to run as the Compose `testserver` service. Local backend test controls require the same controller URL and control token to be configured.

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\stress-test.ps1
```