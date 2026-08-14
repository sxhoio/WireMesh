<div align="center">

# WireMesh

**Multi-tenant WireGuard control plane & operations console**

A Go control plane and Vue console for managing multi-tenant WireGuard networks — versioned desired configuration from topology, mTLS agent enrollment, delivery acknowledgement, and full change history.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](go.mod)
[![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white)](frontend/package.json)
[![Vite](https://img.shields.io/badge/Vite-8-646cff?logo=vite&logoColor=white)](frontend/package.json)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](#license)
[![CI](https://img.shields.io/badge/CI-GitHub%20Actions-2088FF?logo=githubactions&logoColor=white)](.github/workflows/ci.yml)
[![golangci](https://img.shields.io/badge/checks-govet%20%7C%20govulncheck-30D5C8)](#development)

</div>

---

## ✨ Features

- **Tenant-scoped users & RBAC** — `viewer` / `operator` / `admin` with bcrypt-protected login, TOTP MFA, SSO (OIDC), and no built-in default credentials.
- **Topology compilation** — projects, networks, IPv4 allocation, and **Full Mesh / Hub-Spoke / custom-peer** modes with versioned config revisions.
- **mTLS agent lifecycle** — one-time enrollment tokens, issued client certificates, automatic renewal before expiry, and instant revocation on rotation (no CRL needed).
- **Secure key management** — WireGuard private keys encrypted with a master key (Argon2id-stretched), persisted CA, and encrypted database configuration.
- **Delivery & audit** — configuration delivery acknowledgement, per-tenant audit trail, alerting (offline / link-down / config-failed) with webhook, DingTalk, WeCom, Feishu, Telegram, and email channels.
- **GeoIP node location** — automatic city-level location via MaxMind, with manual override and public-IP discovery.
- **Operations console** — a Vue 3 dashboard with a world map, traffic charts, batch node operations, DNS management, access policies, and API tokens.

## 📸 Screenshots

> _Coming soon — console screenshots (map dashboard, node list, settings)._

## 📑 Table of Contents

- [Features](#features)
- [Quick Start](#quick-start)
- [Database](#database)
- [Docker Deployment](#docker-deployment)
- [Agent Onboarding](#agent-onboarding)
- [Automatic Node Location](#automatic-node-location)
- [Environment Variables](#environment-variables)
- [Security](#security)
- [Upgrading](#upgrading)
- [Architecture](#architecture)
- [Development](#development)
- [FAQ](#faq)
- [Contributing](#contributing)
- [License](#license)

---

## 🚀 Quick Start

### Prerequisites

- Go **1.26.6+** (older toolchains contain fixed standard-library vulnerabilities)
- Node.js **22+** (frontend development only)
- WireGuard tooling on agent hosts (`wg`, `wg-quick`, `ip`)

### Run the control plane

```bash
# generate a strong master key (KEEP IT; it encrypts all private data)
export WIREMESH_MASTER_KEY="$(openssl rand -base64 32)"

go run ./cmd/wiremesh-server
```

> On first run the onboarding wizard walks you through choosing **SQLite / MySQL / PostgreSQL**, creating the schema, and setting up the initial administrator. The wizard requires `X-Setup-Token`; if you did not set `WIREMESH_SETUP_TOKEN`, the server prints an auto-generated one to the log and saves it to `wiremesh-setup-token`.

### Run the console (development)

```bash
cd frontend
npm install
npm run dev
```

Open <http://localhost:5173> — it proxies API requests to `http://localhost:8080` by default.

### Onboarding flow

1. Visit the console and complete the first-run wizard (database + initial admin).
2. Create a **project**, then a **network** (CIDR, topology).
3. Add **nodes** and publish a configuration version.
4. Enroll Agents and watch them appear on the map.

---

## 🗄️ Database

WireMesh supports **SQLite** (default), **PostgreSQL**, and **MySQL** through the same SQL layer with automatic migrations.

### SQLite (default)

The first-run wizard defaults to SQLite; the file lives beside `wiremesh-database.json` (encrypted config):

```bash
go run ./cmd/wiremesh-server
```

### PostgreSQL

```bash
export WIREMESH_DATABASE_DRIVER=postgres
export WIREMESH_DATABASE_DSN='postgres://wiremesh:password@localhost:5432/wiremesh?sslmode=disable'
go run ./cmd/wiremesh-server
```

### MySQL

```bash
export WIREMESH_DATABASE_DRIVER=mysql
export WIREMESH_DATABASE_DSN='wiremesh:password@tcp(localhost:3306)/wiremesh'
go run ./cmd/wiremesh-server
```

> `WIREMESH_DATABASE_DRIVER` accepts `sqlite`, `mysql`, or `postgres`. When set (with `WIREMESH_DATABASE_DSN`), environment variables take precedence and the web wizard is disabled. Otherwise the wizard stores the encrypted connection configuration in `wiremesh-database.json` — set `WIREMESH_DATABASE_CONFIG` to relocate it.

---

## 🐳 Docker Deployment

The multi-stage image caches npm and Go build inputs, compiles a static stripped server, and runs it as a non-root user in a distroless image. The Go process serves both the API and the compiled Vue console.

```bash
docker build -t wiremesh:local .
docker run --rm -p 8080:8080 \
  -v wiremesh-data:/data \
  -e WIREMESH_MASTER_KEY="$(openssl rand -base64 32)" \
  wiremesh:local
```

Open <http://localhost:8080>.

> **Production notes**
> - Mount `./secrets/master.key` via Docker secrets / a secret manager — never inline the master key in compose files.
> - Mount TLS certificates via `WIREMESH_TLS_CERT_FILE` / `WIREMESH_TLS_KEY_FILE` for HTTPS + strict agent mTLS.
> - GeoIP is **optional**: mount a MaxMind database at `/data/GeoLite2-City.mmdb` (the image sets `WIREMESH_GEOIP_DB=/data/GeoLite2-City.mmdb`; the file is not baked in because MaxMind's license forbids redistribution). Without it, nodes fall back to Agent-reported coordinates.
> - See [docker-compose.example.yml](docker-compose.example.yml) for a TLS + secrets + GeoIP production reference.

---

## 🤖 Agent Onboarding

Create an enrollment token in the console (**节点列表 → 接入节点**) and run the one-line installer on the target host:

```bash
curl -fsSL 'https://wiremesh.example.com/agent/install.sh' | sudo bash -s -- --token '<TOKEN>' --name 'edge-01'
```

Or manually:

```bash
curl -fL 'https://wiremesh.example.com/agent/download?os=linux&arch=amd64' -o wiremesh-agent
chmod +x wiremesh-agent
sudo ./wiremesh-agent \
  --server 'https://wiremesh.example.com' \
  --enroll-token '<TOKEN>' \
  --name 'edge-01' \
  --mtls=true
```

### Console onboarding options

The **接入新节点** dialog pre-fills the generated command with per-environment options:

| Option | Default | Description |
|--------|---------|-------------|
| **mTLS client certificate** | HTTPS: on · HTTP: off | Agent authenticates with its enrolled certificate. The script accepts `mtls=true\|false` to pin the choice. |
| **Verify self-update signature** | on | When the server has `WIREMESH_UPDATE_SIGNING_KEY`, the script embeds the signing public key and the Agent enforces signature verification on every update manifest. |

The install script also accepts `--mtls` / `--no-mtls` and `--update-public-key` at runtime; console options simply pin the defaults.

---

## 🌍 Automatic Node Location

At startup the Agent resolves its real public IPv4 address **once** from `https://ipv4.ip.sb` (override with `WIREMESH_PUBLIC_IP_URL`) and attaches it to every report as the `X-Agent-Public-IP` header. The control plane prefers this self-reported address for GeoIP, so nodes are located from their own public IP even behind NAT.

- Manual coordinates always take priority.
- Forwarded headers (`X-Forwarded-For` / `X-Real-IP`) are only trusted when the direct connection comes from a private/loopback/link-local address.
- The same public IPv4 also fills the node's public Endpoint automatically (never overwriting a manually set one).

---

## ⚙️ Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `WIREMESH_MASTER_KEY` | ✅ | Root key that encrypts node private keys, the agent CA, database config, and signs session tokens. **Never change it after initialization.** |
| `WIREMESH_SETUP_TOKEN` | — | First-run wizard token. If unset, an auto-generated random token is printed to the log and saved to `wiremesh-setup-token`. |
| `WIREMESH_DATABASE_DRIVER` / `WIREMESH_DATABASE_DSN` | — | `sqlite` / `mysql` / `postgres` + connection string. When set, the web wizard is disabled. |
| `WIREMESH_DATABASE_CONFIG` | — | Path to the encrypted bootstrap config (default `wiremesh-database.json`). |
| `WIREMESH_TLS_CERT_FILE` / `WIREMESH_TLS_KEY_FILE` | — | Enables HTTPS and strict agent mTLS. |
| `WIREMESH_CA_FILE` | — | Agent CA persistence file (default `wiremesh-ca.json`). |
| `WIREMESH_TRUST_PROXY_AGENT_ID` | — | `true` when a trusted reverse proxy terminates TLS and keeps the backend private. |
| `WIREMESH_AGENT_INSECURE_HTTP` | — | `1` to enable the plain-HTTP agent adapter (local development **only**). |
| `WIREMESH_AGENT_BINARY` / `WIREMESH_AGENT_VERSION` | — | Agent update package metadata (enables remote self-update). |
| `WIREMESH_UPDATE_SIGNING_KEY` | — | PEM ECDSA P-256 private key; when set, update manifests are signed and Agents can enforce signature verification. |
| `WIREMESH_PUBLIC_URL` | — | Fixed public origin for the SSO `redirect_uri` (recommended; otherwise derived from a validated Host header). |
| `WIREMESH_COOKIE_SECURE` | — | `true` to force `Secure` on session cookies behind a TLS-terminating proxy. |
| `WIREMESH_GEOIP_DB` | — | Path to a MaxMind GeoIP database (optional). |
| `WIREMESH_PUBLIC_IP_URL` | — | Public IPv4 discovery endpoint (default `https://ipv4.ip.sb`). |
| `WIREMESH_CORS_ORIGIN` | — | Allowed origin for the console (default `http://localhost:5173`). |
| `WIREMESH_DATABASE_ALLOW_PRIVATE` | — | `1` to allow private/reserved database hosts (SSRF guard escape hatch). |
| `WIREMESH_SSO_ALLOW_PRIVATE` | — | `1` to allow private/loopback OIDC endpoints (local IdP only). |

---

## 🔐 Security

WireMesh applies defense in depth across transport, identity, data, and supply chain.

### Transport

- Agent endpoints (`/agent/v1/*`) **fail closed on plain HTTP** — production must serve HTTPS with strict mTLS (`RequireAgentClientCert`), or terminate TLS at a trusted reverse proxy (`WIREMESH_TRUST_PROXY_AGENT_ID=true`) while keeping the backend private.
- With `WIREMESH_TRUST_PROXY_AGENT_ID`, the `X-Agent-ID` header is only accepted from **private/loopback** sources.
- Session cookies: `HttpOnly` + `SameSite=Lax` (+ `Secure` on TLS or via `WIREMESH_COOKIE_SECURE`).

### Identity

- Local passwords: **bcrypt (cost 12)**; TOTP secrets are 256-bit; MFA setup/enable/disable require current-password re-verification.
- Session tokens: HMAC-signed; every request re-checks the user against the DB (role changes & deactivation take effect immediately); logout/change-password/disable revoke tokens **persistently** (survive restarts).
- SSO (OIDC): ID-token signature/issuer/audience/exp/nonce validation, **PKCE**, and a fixed `redirect_uri` source.
- Agent certificates: enrolled with one-time tokens, **renew automatically**, revoked instantly on rotation via fingerprint binding.

### Data

- WireGuard private keys and database configuration are **encrypted** with an Argon2id-stretched master key (legacy-SHA-256 fallback keeps old data readable).
- SQL is fully parameterized; all `/{id}` routes are tenant-scoped.
- Notification/OIDC outbound calls reject private/loopback targets (SSRF guard); database hosts are resolved to validated IPs before connecting.

### Supply chain

- Agent install script verifies the binary SHA-256; with a signing key configured, update manifests are **signature-verified** and Agents **fail closed** on unsigned updates.
- `govulncheck` runs in CI; the `go.mod` directive enforces Go 1.26.6+.

---

## 🔄 Upgrading

Upgrades are in-place and forward-compatible; the server applies schema migrations automatically on startup. Version history: [CHANGELOG.md](CHANGELOG.md) — control-plane line `v0.7.x`, Agent line `0.3.7`.

1. **Back up first** — `GET /api/v1/settings/backup` (SQLite) or your DB dump tooling. Restore is atomic and **requires password + MFA re-authentication**; it clears all in-memory sessions.
2. **Keep the same `WIREMESH_MASTER_KEY`** — encrypted blobs are unwrapped with it. Changing it makes existing data undecryptable (old data stays readable through the legacy-key fallback, no re-encryption needed).
3. **Database configuration is preserved** — `wiremesh-database.json` is reused; migrations run automatically.
4. **Agents do not need reinstall** — certificates survive restarts and renew before expiry; Agent self-update (SHA-256 + optional signature) is driven from the console.
5. **Run with Go 1.26.6+** — older toolchains contain fixed standard-library vulnerabilities.
6. **Downgrade note** — schema migrations are additive; older binaries ignore the newest columns.

---

## 🧱 Architecture

```
┌────────────────────────────┐       ┌─────────────────────────────┐
│        Vue 3 Console        │       │        Go control plane     │
│  (map, nodes, settings…)    │◄─────►│  HTTP API + Agent endpoints │
└────────────────────────────┘       └──────────────┬──────────────┘
                                                    │ mTLS / enrollment
                                          ┌─────────▼─────────┐
                                          │  WireGuard Agents  │
                                          │  (per-node mesh)   │
                                          └───────────────────┘
```

- **`cmd/wiremesh-server`** — control-plane entrypoint and frontend static-file serving.
- **`cmd/wiremesh-agent`** — node Agent command-line client.
- **`internal/control`** — domain models, auth, topology compilation, persistence, HTTP API, tests.
- **`frontend`** — Vue 3 + TypeScript operations console built with Vite.

---

## 🛠️ Development

```bash
# backend: format, vet, test
gofmt -w <changed-files>
go vet ./...
go test -count=1 ./...

# frontend: install, type-check & build
cd frontend
npm ci
npm run build
```

### CI

[.github/workflows/ci.yml](.github/workflows/ci.yml) runs `go vet`, `go test`, `go build`, `govulncheck`, and the frontend build + `npm audit` on every push/PR.

---

## ❓ FAQ

**Q: I see `WIREMESH_MASTER_KEY is required` at startup — what do I do?**
Set a strong key (`openssl rand -base64 32`) via the environment or `WIREMESH_MASTER_KEY_FILE`, and keep it unchanged for the life of the deployment.

**Q: `decrypt database configuration: cipher: message authentication failed`?**
The configured `WIREMESH_MASTER_KEY` does not match the one that encrypted `wiremesh-database.json`. Either restore the original key (keep data) or delete the stale config (fresh start).

**Q: My Agent is rejected with `agent identity was rejected`?**
Check the server log for the specific reason (certificate required / unknown identity / not registered). Common causes: TLS terminated before the backend (mTLS missing), or the node was deleted/rotated. Re-enroll with a fresh token after fixing the transport.

**Q: `agent endpoints require TLS`?**
The backend is on plain HTTP. Configure TLS, or use the trusted-proxy mode while keeping the backend private.

---

## 🤝 Contributing

Contributions are welcome! Please:

1. Open an issue to discuss significant changes.
2. Keep `internal/control.Store` implementations behaviorally consistent across SQLite / PostgreSQL / MySQL.
3. Add tests for new resource paths (tenant isolation) and run the full suite (`go vet`, `go test`, `npm run build`).

---

## 📄 License

WireMesh is released under the [MIT License](LICENSE).

_Note: A `LICENSE` file is not yet present in the repository — add one before publishing, or replace this section with your chosen license._
