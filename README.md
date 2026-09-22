# Audio Speech Vault

Audio Speech Vault currently contains a Go API foundation, PostgreSQL migrations, and a React dashboard shell. Authentication, uploads, analysis, and research workflows are planned but not implemented yet.

## Repository Layout

- `backend/`: Go server, migrations, and backend Dockerfile
- `frontend/`: React, TypeScript, and Vite application
- `docker-compose.dev.yml`: local three-service stack
- `scripts/check.ps1`: shared local and CI checks

## Required Software

- Go 1.25 or newer
- Node.js 24 or newer with npm for running the frontend outside Docker
- Docker Desktop with Docker Compose
- PowerShell 7 or newer for the shared local check script
- Git

## Environment Setup

Copy `.env.example` to `.env`. The file is ignored by Git. Choose a unique local password, then set `POSTGRES_PASSWORD` and `DATABASE_URL` in `.env` to the same password. The URL must use `postgres` as its host and port `5432` for the app container. URL-encode any special characters in the password within `DATABASE_URL`.

```powershell
Copy-Item .env.example .env
```

For running Go commands directly on the host, set `DATABASE_URL` in your shell using the same credentials, but change the host to `localhost` and the port to `POSTGRES_PORT` (default `5433`):

```powershell
$env:DATABASE_URL = "<your local PostgreSQL connection URL>"
```

## Local Startup

Start PostgreSQL, the Go API, and the React development server:

```powershell
docker compose -f docker-compose.dev.yml up --build
```

Open the dashboard at `http://localhost:5173` (or `FRONTEND_PORT` from `.env`). The Go API listens at `http://localhost:8080` (or `HTTP_PORT` from `.env`). Vite proxies health requests to the API in Compose.

For separate host development, run `npm ci` and `npm run dev` from `frontend/`. Set `API_PROXY_TARGET` to the host API URL if the API does not use port `8080`.

## Migrations

The Docker Compose app service runs migrations automatically before starting the server. For manual migrations, change to `backend/` first; the migration command reads `migrations/` relative to the current directory.

To run migrations manually against the local PostgreSQL container:

```powershell
Set-Location backend
go run ./cmd/migrate up
```

Check the migration version:

```powershell
go run ./cmd/migrate version
```

Roll back one migration:

```powershell
go run ./cmd/migrate down
```

## Tests And Checks

Run backend unit tests:

```powershell
Set-Location backend
go test ./...
```

Run the frontend checks from `frontend/` with `npm ci` and `npm run check`.

Run the same check command used by CI from the repository root. It uses Docker for frontend checks when npm is not installed locally:

```powershell
./scripts/check.ps1
```

The check script runs Go formatting, vetting, tests, and build in `backend/`, then installs locked frontend dependencies and runs its TypeScript and Vite build checks.

```powershell
go fmt ./...
go vet ./...
go test ./...
go build ./...
npm ci
npm run check
```

## Health Checks

Liveness:

```text
http://localhost:5173/health/live
```

Readiness:

```text
http://localhost:5173/health/ready
```

These URLs pass through the Vite development proxy. The same paths are available directly on the API port. `/health/live` returns `200` when the process can serve HTTP. `/health/ready` returns `200` only when PostgreSQL can be reached, and `503` when the database is unavailable.

## PostgreSQL Troubleshooting

- If the app cannot connect, confirm the database container is healthy with `docker compose -f docker-compose.dev.yml ps`.
- If port `5433` is already in use, set another `POSTGRES_PORT` in `.env` and update the host `DATABASE_URL` to match.
- If readiness returns `503`, check PostgreSQL logs with `docker compose -f docker-compose.dev.yml logs postgres`.
- If migrations fail after an interrupted run, check `go run ./cmd/migrate version` for a dirty migration state before retrying.
- If local data becomes disposable and you need a clean database, stop Compose and remove the named volume with `docker compose -f docker-compose.dev.yml down -v`.

## End-Of-Day Verification

```powershell
docker compose -f docker-compose.dev.yml up --build
./scripts/check.ps1
```

Then verify:

- The dashboard opens at `http://localhost:5173`.
- `/health/live` returns `200` through the dashboard origin.
- `/health/ready` returns `200` through the dashboard origin.
- PostgreSQL migrations have completed.
- Stopping PostgreSQL makes readiness return `503`.
- Restarting PostgreSQL restores readiness.
- Application shutdown is graceful.
- Existing Python prototype files and tests remain intact.
- No `.env`, MP3, report, metadata, or database file is staged in Git.
