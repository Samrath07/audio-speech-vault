# Audio Speech Vault

Audio Speech Vault currently contains a Go API, PostgreSQL-backed authentication, and a React dashboard. Uploads, analysis, and research workflows are planned but not implemented yet.

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

Open the dashboard at `http://localhost:5173` (or `FRONTEND_PORT` from `.env`). The Go API listens at `http://localhost:8080` (or `HTTP_PORT` from `.env`). Vite proxies `/health` and `/api` requests to the API in Compose.

## First Superadmin

After the containers are running, create the first superadmin from an interactive terminal:

```powershell
docker compose -f docker-compose.dev.yml exec app audio-speech-vault-bootstrap-superadmin
```

Enter the email, display name, and a unique password when prompted. The password is hidden while typing and must be 12 to 72 bytes. Bootstrap works only while the users table is empty. Sign in at the dashboard URL. The superadmin can then create accounts for `admin`, `researcher`, `reviewer`, or another `superadmin`, change their roles, reset passwords, and deactivate accounts. Every active role can sign in and see the Overview and Account views; only superadmins see Users.

Sessions are stored in PostgreSQL. Changing a role, deactivating an account, or changing/resetting a password revokes that account's sessions. The last active superadmin cannot be demoted or deactivated. In UAT and production, the session cookie requires HTTPS; set `FRONTEND_ORIGIN` to the browser-facing origin when the dashboard and API use different internal hosts.

## Access Requests

Applicants use **Request institutional access** on the sign-in page and provide their institution, full name, institutional email, and requested role. Public email providers are rejected. The requested role is advisory: only a superadmin can assign the final role, and `superadmin` cannot be requested publicly.

The application sends an email-verification link before showing a request to superadmins. After approval, it sends a single-use password-setup link that expires after 24 hours. The raw verification and setup tokens are never stored in PostgreSQL.

Local development writes email previews to the ignored `data/mail-outbox/` directory. Open the newest `.eml` file to follow its link. Configure `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, and `SMTP_FROM` for real delivery. SMTP host and sender are required in UAT and production.

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

Authentication and access-request integration tests use separate migrated databases so they can run safely in parallel. Set `TEST_DATABASE_URL` to a database ending in `_auth_test` and `TEST_ACCESS_DATABASE_URL` to one ending in `_access_test`. Tests are skipped when their corresponding variable is absent.

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
