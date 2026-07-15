# WireMesh

WireMesh is a Go control plane and Vue console for managing multi-tenant WireGuard networks. It creates versioned desired configuration from a topology, delivers it to enrolled agents, and records change history.

## Included vertical slice

- Tenant-scoped users and RBAC (`viewer`, `operator`, `admin`) with local login. The default development account is `admin@wiremesh.local` / `wiremesh-dev`.
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

Open `http://localhost:5173` and use the development account above. Create a project, then a network, then nodes, and publish a configuration version.

## Docker

The multi-stage image caches npm and Go build inputs separately, compiles a static stripped server, and runs it as a non-root user in a distroless image. The Go process serves both the API and the compiled Vue console.

```powershell
docker build -t wiremesh:local .
docker run --rm -p 8080:8080 -e WIREMESH_MASTER_KEY=replace-with-a-secret wiremesh:local
```

Open `http://localhost:8080`. For production, inject `WIREMESH_MASTER_KEY` from a secret manager and mount the TLS certificate/key files referenced by `WIREMESH_TLS_CERT_FILE` and `WIREMESH_TLS_KEY_FILE`.

To exercise enrollment, create an Agent token in the Nodes view and run:

```powershell
go run ./cmd/wiremesh-agent -server http://localhost:8080 -enroll-token <token> -name edge-01
```

## Production boundaries

The current `MemoryStore` is deliberately a local-development implementation of the persistence interface. A PostgreSQL store must replace it before any durable deployment. Setting `WIREMESH_TLS_CERT_FILE` and `WIREMESH_TLS_KEY_FILE` starts HTTPS and verifies presented Agent certificates; plain HTTP retains an `X-Agent-ID` development adapter and must not be exposed. The certificate authority is currently generated at process start, so CA persistence and KMS integration are also required before production use.
