# syntax=docker/dockerfile:1.7

ARG NODE_VERSION=24-alpine
ARG GO_VERSION=1.26.2-alpine
ARG WIREMESH_VERSION=0.3.6

FROM node:${NODE_VERSION} AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:${GO_VERSION} AS server-build
ARG WIREMESH_VERSION=0.3.6
WORKDIR /src
RUN mkdir -p /out/data
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w -buildid=" \
    -o /out/wiremesh-server ./cmd/wiremesh-server
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath -ldflags="-s -w -buildid= -X main.agentVersion=${WIREMESH_VERSION}" \
    -o /out/wiremesh-agent-linux-amd64 ./cmd/wiremesh-agent && \
    CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build \
    -trimpath -ldflags="-s -w -buildid= -X main.agentVersion=${WIREMESH_VERSION}" \
    -o /out/wiremesh-agent-linux-arm64 ./cmd/wiremesh-agent

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
ARG WIREMESH_VERSION=0.3.6
WORKDIR /data
COPY --from=server-build --chown=nonroot:nonroot /out/wiremesh-server /app/wiremesh-server
COPY --from=server-build --chown=nonroot:nonroot /out/wiremesh-agent-linux-amd64 /app/wiremesh-agent-linux-amd64
COPY --from=server-build --chown=nonroot:nonroot /out/wiremesh-agent-linux-arm64 /app/wiremesh-agent-linux-arm64
COPY --from=frontend-build --chown=nonroot:nonroot /src/frontend/dist/ /app/web/
COPY --chown=nonroot:nonroot GeoLite2-City.mmdb /app/GeoLite2-City.mmdb
COPY --from=server-build --chown=nonroot:nonroot /out/data/ /data/
ENV WIREMESH_ADDR=:8080 \
    WIREMESH_WEB_DIR=/app/web \
    WIREMESH_AGENT_BINARY=/app/wiremesh-agent-{os}-{arch} \
    WIREMESH_AGENT_VERSION=${WIREMESH_VERSION} \
    WIREMESH_DATABASE_CONFIG=/data/wiremesh-database.json \
    WIREMESH_GEOIP_DB=/app/GeoLite2-City.mmdb
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/wiremesh-server"]
