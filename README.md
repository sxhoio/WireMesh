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

> **Security**: the first-run wizard endpoints (`/api/v1/setup*`) are unauthenticated by design. If the instance may be reachable before initialization completes, set `WIREMESH_SETUP_TOKEN` — the wizard then requires the token in the `X-Setup-Token` header for database configuration and initial-admin creation. The wizard also refuses to connect to private/reserved/link-local database hosts (loopback allowed); set `WIREMESH_DATABASE_ALLOW_PRIVATE=1` only if you must point at an internal database.

WireMesh does not seed an administrator. `GET /api/v1/setup/status` reports whether any user exists (plus `setup_token_required`), and the onboarding page calls the one-time `POST /api/v1/setup` endpoint to create the initial tenant and administrator. The endpoint returns `409 Conflict` after the first user exists.

## Docker

The multi-stage image caches npm and Go build inputs separately, compiles a static stripped server, and runs it as a non-root user in a distroless image. The Go process serves both the API and the compiled Vue console.

```powershell
docker build -t wiremesh:local .
docker run --rm -p 8080:8080 -v wiremesh-data:/data -e WIREMESH_MASTER_KEY=replace-with-a-secret wiremesh:local
```

Open `http://localhost:8080`. The image starts the database-selection wizard and stores its encrypted database configuration and optional SQLite file in the `/data` volume. Server-side GeoIP is **optional** and provided by mounting a MaxMind database at `/data/GeoLite2-City.mmdb` (the image defaults `WIREMESH_GEOIP_DB` to that path, but the file is not baked in because MaxMind's license forbids redistribution). Mount `./data/GeoLite2-City.mmdb:/data/GeoLite2-City.mmdb:ro` when you want automatic location; without it, nodes fall back to Agent-reported coordinates and public-IP-only discovery. Override `WIREMESH_GEOIP_DB` when using an externally updated database. For production, inject `WIREMESH_MASTER_KEY` from a secret manager and mount the TLS certificate/key files referenced by `WIREMESH_TLS_CERT_FILE` and `WIREMESH_TLS_KEY_FILE`.

To exercise enrollment, create an Agent token in the Nodes view and run:

```powershell
go run ./cmd/wiremesh-agent -server http://localhost:8080 -enroll-token <token> -name edge-01
```

## Automatic node location

At startup the Agent resolves its real public IPv4 address once from `https://ipv4.ip.sb` (override with `WIREMESH_PUBLIC_IP_URL`) and attaches it to every subsequent report as the `X-Agent-Public-IP` header. It is not refreshed on a timer — only when the Agent process restarts — so the node makes a single outbound request at boot rather than a periodic pattern that could be mistaken for C2. The control plane prefers this self-reported address for GeoIP, so nodes are located from their own public IP even when NAT or a proxy egress would otherwise show a different source address. When the header is absent (older Agents, or discovery failed), the control plane falls back to the reporting connection's observed source address; heartbeats from older Agents are located the same way, so server upgrades can improve existing nodes without waiting for every Agent to be replaced.

Manual coordinates always take priority. Clearing a manual location returns the node to automatic Agent/GeoIP discovery. When WireMesh is behind a reverse proxy, preserve the client address with `X-Forwarded-For` or `X-Real-IP`; forwarded headers are only trusted when the direct connection comes from a private, loopback, or link-local address.

The same public IPv4 also fills the node's public Endpoint automatically: when a node has no manually configured endpoint, the control plane records it as `public-ip:listen-port` from the heartbeat, so peers can reach the node without an operator typing the address by hand. A manually set endpoint is never overwritten.

## Production boundaries

SQLite, MySQL, and PostgreSQL are supported through the same SQL repository and automatic schema creation. Setting `WIREMESH_TLS_CERT_FILE` and `WIREMESH_TLS_KEY_FILE` starts HTTPS and verifies presented Agent certificates. The certificate authority is persisted (encrypted with the master key) in `wiremesh-ca.json`, so Agent certificates survive server restarts; keep `WIREMESH_MASTER_KEY` in a KMS-backed secret for production.

> **Agent endpoint security**: Agent configuration endpoints (`/agent/v1/*`) refuse to run over plain HTTP by default — the `X-Agent-ID` header alone can impersonate a node and steal its WireGuard private key, so production must serve HTTPS with the strict mTLS mode (`RequireAgentClientCert`), or terminate TLS at a trusted reverse proxy and set `WIREMESH_TRUST_PROXY_AGENT_ID=true` while keeping the backend listener private. For local development only, set `WIREMESH_AGENT_INSECURE_HTTP=1` to re-enable the plain-HTTP adapter. Sessions are revoked persistently (database-backed blacklist), so disabling a user, downgrading a role, or force-logging-out a session takes effect immediately and survives server restarts.

> **Agent self-update integrity**: the one-click install script now refuses to proceed unless the downloaded binary's SHA-256 matches the server-provided value (`sha256sum` or `openssl` required). For defense in depth against a compromised control plane, configure update manifest signing: set `WIREMESH_UPDATE_SIGNING_KEY` on the server (PEM ECDSA P-256 private key) and pass the matching public key to each Agent via `--update-public-key`. When an Agent is configured with a public key, unsigned or invalidly signed update manifests are rejected. Generate a key pair with `openssl ecparam -name prime256v1 -genkey -noout -out update-sign.key && openssl ec -in update-sign.key -pubout -out update-sign.pub`.

> **Agent certificates**: enrolled client certificates are valid for one year and renew automatically 30 days before expiry (`POST /agent/v1/renew-cert`, mTLS-authenticated). Renewal overwrites the node's registered certificate fingerprint, so the previous certificate stops working immediately — rotation and revocation take effect without a CRL. Run Agents with `--mtls` so node identity comes from the certificate; if `--mtls` is requested but the enrolled material is missing, the Agent refuses to start instead of silently falling back to the `X-Agent-ID` header.

> **Credential & outbound hardening**: local passwords are hashed with bcrypt (cost 12), TOTP keys are 256-bit, and the master key is stretched with Argon2id before use (existing encrypted data stays readable through a legacy-key fallback). OIDC outbound calls (discovery, JWKS, token, userinfo) reject private/loopback targets to prevent SSRF — set `WIREMESH_SSO_ALLOW_PRIVATE=1` only for a local IdP. SSO `redirect_uri` is derived from the validated Host header and must match between login and callback. Notification messages are sanitized per channel (HTML-escaped for markup-capable chat channels, control characters stripped everywhere). SQLite files are created with 0600 permissions.

## Upgrading

Upgrades are in-place and forward-compatible; the server applies schema migrations automatically on startup. Version history is tracked in `CHANGELOG.md`; the control-plane line is `v0.5.x` and the Agent binary has its own version line (`cmd/wiremesh-agent/main.go`, currently `0.3.6`).

1. **Back up first**: download `GET /api/v1/settings/backup` (SQLite) or use your PostgreSQL/MySQL dump tooling. Restore is available through `POST /api/v1/settings/backup/restore` (SQLite) and is atomic — it validates the file, replaces the live database, and survives restarts.
2. **Keep the same `WIREMESH_MASTER_KEY`**: encrypted blobs (node private keys, agent CA, database configuration, notification secrets) are unwrapped with the master key. Changing it makes existing data undecryptable. Since v0.5.0 the master key is stretched with Argon2id; data encrypted by older versions remains readable through a legacy-key fallback, so no re-encryption pass is needed.
3. **Database configuration is preserved**: `wiremesh-database.json` (encrypted) is reused when `WIREMESH_DATABASE_CONFIG` points at the same path; the server opens the configured database and migrates schema automatically. Environment-variable DSNs (`WIREMESH_DATABASE_DRIVER`/`WIREMESH_DATABASE_DSN`) take precedence and disable the web wizard.
4. **Agents do not need to be reinstalled**: enrolled certificates survive restarts (CA persisted in `wiremesh-ca.json`) and renew automatically before expiry. Agent self-update (SHA-256 verified, optionally signature-verified) is driven from the console. Upgrade the control plane first, then update Agents at your own pace — the protocol is backward compatible for delivery state and heartbeats.
5. **Run with Go 1.26.6+** (or the matching Docker image): older toolchains contain fixed standard-library vulnerabilities. The `go.mod` directive enforces the minimum.
6. **Downgrade note**: schema migrations are additive, so going back to an older control plane version is generally safe; only the newest migration columns may be ignored by older binaries.
