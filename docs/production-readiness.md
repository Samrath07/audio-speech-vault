# Production Readiness

This application processes research audio and must be deployed as a private service. The development Compose file is not a production deployment manifest.

## Required Platform Controls

- Terminate TLS 1.2 or newer at the government-cloud ingress and redirect HTTP to HTTPS.
- Store `DATABASE_URL`, SMTP credentials, and future identity-provider credentials in the platform secret manager. Never bake them into images, manifests, or repository files.
- Run the API and PostgreSQL on private networks. Expose only the ingress endpoint.
- Use a dedicated PostgreSQL role with only the privileges required by this database.
- Encrypt database volumes, backups, uploaded audio, metadata, and reports at rest.
- Restrict access to application logs and configure retention according to the approved data-classification policy.
- Scan application and base images before deployment and fail releases on unresolved critical vulnerabilities.
- Pin production images by digest and deploy immutable images through reviewed CI/CD.

## Identity And Access

The current POC uses local accounts and role-based sessions. Before production, integrate the approved OIDC identity provider, validate issuer and audience, use PKCE, map immutable subject identifiers to users, and keep project membership authorization inside the application. Superadmin assignment must remain an explicit audited action and must not be inferred from untrusted token claims.

Session cookies must remain `HttpOnly`, `Secure`, and `SameSite=Lax` or stricter. Configure `FRONTEND_ORIGIN` to the exact external HTTPS origin. Keep CSRF validation enabled for every state-changing browser request.

## Audio And Research Data

- Agree project-specific retention periods with the data owner before ingestion.
- Define separate retention for source audio, derived metadata, human annotations, audit events, exports, and backups.
- Use archive-first deletion with a documented recovery period. Permanent deletion must remove the database rows, source audio, metadata, reports, and backup copies when their retention window expires.
- Require a second authorized operator for bulk permanent deletion in production.
- Record deletion and export activity in an append-only audit destination outside the application database.
- Validate backup restoration regularly. A backup that has not been restored in a test is not considered verified.

## Observability

Collect structured application logs, request rates, latency, HTTP error counts, database pool health, analysis queue depth, job duration, failure codes, disk consumption, and backup status. Alert on sustained readiness failures, growing queue depth, repeated job failures, low storage capacity, and unexpected export volume. Do not include passwords, session identifiers, reset tokens, raw email links, annotation contents, or audio data in logs.

## Release Gates

1. Run `./scripts/check.ps1` from a clean checkout.
2. Run integration tests against an isolated migrated PostgreSQL database.
3. Run dependency, container, secret, and static-analysis scans.
4. Verify migrations and rollback behavior against a production-like copy.
5. Verify `/health/live` and `/health/ready` through the ingress.
6. Exercise login, authorization boundaries, upload, analysis, annotation, review, and export with each role.
7. Verify graceful shutdown while an analysis job is active.
8. Restore the latest encrypted backup into an isolated environment and validate record and artifact counts.
9. Confirm `.env`, audio, metadata, reports, exports, and database files are absent from the Git index and build context.

## Remaining External Decisions

The deployment owner must provide the approved Finnish government cloud service, OIDC issuer, ingress/WAF policy, managed PostgreSQL offering, object storage, key-management service, monitoring stack, backup retention, data classification, region constraints, and incident-response contacts. These values must not be guessed or committed as defaults.
