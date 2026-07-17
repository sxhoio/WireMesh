# WireMesh

WireMesh is a Go control plane and Vue console for managing multi-tenant WireGuard networks. It creates versioned desired configuration from a topology, delivers it to enrolled agents, and records change history.

## Included vertical slice

- Tenant-scoped users and RBAC (`viewer`, `operator`, `admin`) with bcrypt-protected database login. No built-in administrator account or password is created.
- Projects, WireGuard networks, IPv4 address allocation, and Full Mesh, Hub-Spoke, or custom-peer topology compilation.
- Managed WireGuard key creation with encrypted private-key storage. `WIREMESH_MASTER_KEY` must come from a KMS-backed secret in production.
- One-time Agent enrollment tokens, issued Agent certificates, desired configuration versions, delivery acknowledgement, and audit records.
- A Vue operations console for projects, networks, nodes, configuration release, enrollment-token creation, delivery status, and audit history.

## Run locally

Start the API:

```powershell
go run ./cmd/wiremesh-server
```

Start the console in another terminal:

```powershell
Set-Location frontend
npm install
npm run dev
```

Open `http://localhost:5173`. On first run, the onboarding page asks you to choose SQLite, MySQL, or PostgreSQL, verifies the connection, creates the required tables, and then creates the initial administrator. Then sign in, create a project and network, add nodes, and publish a configuration version.

## Database

Without database environment variables, the first-run wizard defaults to SQLite and stores `wiremesh.db` beside `wiremesh-database.json`:

```powershell
go run ./cmd/wiremesh-server
```

PostgreSQL uses the same schema and automatic migrations:

```powershell
$env:WIREMESH_DATABASE_DRIVER = 'postgres'
$env:WIREMESH_DATABASE_DSN = 'postgres://wiremesh:password@localhost:5432/wiremesh?sslmode=disable'
go run ./cmd/wiremesh-server
```

`WIREMESH_DATABASE_DRIVER` accepts `sqlite`, `mysql`, or `postgres`. When `WIREMESH_DATABASE_DRIVER` / `WIREMESH_DATABASE_DSN` are omitted, the first-run web wizard lets an administrator choose SQLite, MySQL, or PostgreSQL, tests the connection, creates the schema, and stores the encrypted connection configuration in `wiremesh-database.json`. Set `WIREMESH_DATABASE_CONFIG` to change that bootstrap file path. Environment variables continue to take precedence and disable database changes from the web wizard.

WireMesh does not seed an administrator. `GET /api/v1/setup/status` reports whether any user exists, and the onboarding page calls the one-time `POST /api/v1/setup` endpoint to create the initial tenant and administrator. The endpoint returns `409 Conflict` after the first user exists.

## Docker

The multi-stage image caches npm and Go build inputs separately, compiles a static stripped server, and runs it as a non-root user in a distroless image. The Go process serves both the API and the compiled Vue console.

```powershell
docker build -t wiremesh:local .
docker run --rm -p 8080:8080 -v wiremesh-data:/data -e WIREMESH_MASTER_KEY=replace-with-a-secret wiremesh:local
```

Open `http://localhost:8080`. The image starts the database-selection wizard and stores its encrypted database configuration and optional SQLite file in the `/data` volume. For production, inject `WIREMESH_MASTER_KEY` from a secret manager and mount the TLS certificate/key files referenced by `WIREMESH_TLS_CERT_FILE` and `WIREMESH_TLS_KEY_FILE`.

To exercise enrollment, create an Agent token in the Nodes view and run:

```powershell
go run ./cmd/wiremesh-agent -server http://localhost:8080 -enroll-token <token> -name edge-01
```

## Production boundaries

SQLite, MySQL, and PostgreSQL are supported through the same SQL repository and automatic schema creation. Setting `WIREMESH_TLS_CERT_FILE` and `WIREMESH_TLS_KEY_FILE` starts HTTPS and verifies presented Agent certificates; plain HTTP retains an `X-Agent-ID` development adapter and must not be exposed. The certificate authority is currently generated at process start, so CA persistence and KMS integration are still required before production use.
