# Audio Speech Vault

Audio Speech Vault is currently a Day 1 Go foundation: an HTTP service, PostgreSQL connection, and schema migrations. Authentication, uploads, audio algorithms, and the dashboard are intentionally outside this first slice.

## Required Software

- Go 1.25 or newer
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

Start PostgreSQL and the Go service:

```powershell
docker compose -f docker-compose.dev.yml up --build
```

The application runs at `http://localhost:8080`.

## Migrations

The Docker Compose app service runs migrations automatically before starting the server.

To run migrations manually against the local PostgreSQL container:

```powershell
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

Run unit tests:

```powershell
go test ./...
```

Run the same check command used by CI:

```powershell
./scripts/check.ps1
```

The check script runs:

```powershell
go fmt ./...
go vet ./...
go test ./...
go build ./...
```

## Health Checks

Liveness:

```text
http://localhost:8080/health/live
```

Readiness:

```text
http://localhost:8080/health/ready
```

`/health/live` returns `200` when the process can serve HTTP. `/health/ready` returns `200` only when PostgreSQL can be reached, and `503` when the database is unavailable.

## PostgreSQL Troubleshooting

- If the app cannot connect, confirm the database container is healthy with `docker compose -f docker-compose.dev.yml ps`.
- If port `5433` is already in use, set another `POSTGRES_PORT` in `.env` and update the host `DATABASE_URL` to match.
- If readiness returns `503`, check PostgreSQL logs with `docker compose -f docker-compose.dev.yml logs postgres`.
- If migrations fail after an interrupted run, check `go run ./cmd/migrate version` for a dirty migration state before retrying.
- If local data becomes disposable and you need a clean database, stop Compose and remove the named volume with `docker compose -f docker-compose.dev.yml down -v`.

## End-Of-Day Verification

```powershell
docker compose -f docker-compose.dev.yml up --build
go test ./...
```

Then verify:

- `http://localhost:8080/health/live` returns `200`.
- `http://localhost:8080/health/ready` returns `200`.
- PostgreSQL migrations have completed.
- Stopping PostgreSQL makes readiness return `503`.
- Restarting PostgreSQL restores readiness.
- Application shutdown is graceful.
- Existing Python prototype files and tests remain intact.
- No `.env`, MP3, report, metadata, or database file is staged in Git.
