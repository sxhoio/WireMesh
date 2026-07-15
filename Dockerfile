# syntax=docker/dockerfile:1.7

ARG NODE_VERSION=24-alpine
ARG GO_VERSION=1.26.2-alpine

FROM node:${NODE_VERSION} AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:${GO_VERSION} AS server-build
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

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=server-build --chown=nonroot:nonroot /out/wiremesh-server /app/wiremesh-server
COPY --from=frontend-build --chown=nonroot:nonroot /src/frontend/dist/ /app/web/
COPY --from=server-build --chown=nonroot:nonroot /out/data/ /data/
ENV WIREMESH_ADDR=:8080 \
    WIREMESH_WEB_DIR=/app/web \
    WIREMESH_DATABASE_DRIVER=sqlite \
    WIREMESH_DATABASE_DSN="file:/data/wiremesh.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/wiremesh-server"]
