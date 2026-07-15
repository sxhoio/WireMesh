# WireMesh Repository Guide

This file applies to the entire repository.

## Project layout

- `cmd/wiremesh-server`: control-plane entrypoint and frontend static-file serving.
- `cmd/wiremesh-agent`: node Agent command-line client.
- `internal/control`: domain models, authentication, topology compilation, persistence, HTTP API, and tests.
- `frontend`: Vue 3 and TypeScript operations console built with Vite.

## Development commands

- Format changed Go files with `gofmt -w <files>`.
- Run backend tests with `go test ./...`.
- Install frontend dependencies with `npm ci` from `frontend/`.
- Validate the frontend with `npm run build` from `frontend/`.
- Run the control plane with `go run ./cmd/wiremesh-server`; SQLite is the default database.

Run the relevant backend and frontend checks before committing. Do not commit `wiremesh.db`, SQLite sidecar files, `frontend/node_modules`, or `frontend/dist`.

## Architecture rules

- Keep `internal/control.Store` implementations behaviorally consistent. Every persistence change must work with both SQLite and PostgreSQL.
- Keep migrations portable between SQLite and PostgreSQL. Use `SQLStore.query` for bind placeholders and avoid database-specific SQL unless it is isolated by driver.
- Scope all user-facing resource reads and writes by `tenant_id`. The frontend is not an authorization boundary.
- Keep SQLite compatible with `CGO_ENABLED=0`; do not replace the pure Go driver with a CGO-only dependency.
- Treat configuration revisions as immutable. Agent delivery acknowledgements update delivery state, not revision contents.
- Keep WireGuard private keys out of API resource responses and audit metadata. Persist them only through `EncryptedSecret`.
- Hash local passwords with bcrypt and return generic authentication errors. Never log passwords, bearer tokens, enrollment tokens, private keys, or database DSNs containing credentials.
- Production frontend requests must remain same-origin unless an explicit deployment configuration requires otherwise.

## Testing expectations

- Add tenant-isolation tests for new resource access paths.
- Add SQLite persistence tests for store changes and placeholder/driver tests for PostgreSQL-specific behavior.
- Cover topology changes for Full Mesh, Hub-Spoke, and custom-peer modes.
- For HTTP changes, test authentication requirements and response status codes as well as the successful path.
