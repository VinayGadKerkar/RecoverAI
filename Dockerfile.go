# ─── Base builder ───────────────────────────────────────────────────────────────
# confluent-kafka-go ships librdkafka pre-compiled against glibc (librdkafka_glibc_linux_amd64.a).
# Alpine uses musl — incompatible. Use the Debian-based golang image instead.
FROM golang:1.23 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# ─── Per-binary build stages ────────────────────────────────────────────────────

FROM builder AS api-builder
RUN CGO_ENABLED=1 GOOS=linux go build -o /bin/api ./cmd/api

FROM builder AS worker-builder
RUN CGO_ENABLED=1 GOOS=linux go build -o /bin/worker ./cmd/worker

# mock-ai has no CGO dependency
FROM builder AS mock-ai-builder
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/mock-ai ./cmd/mock-ai

# ─── Minimal runtime images ──────────────────────────────────────────────────────
# Debian slim — compatible with the glibc-linked librdkafka inside the binaries.

FROM debian:bookworm-slim AS api
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY --from=api-builder /bin/api /bin/api
EXPOSE 8080
ENTRYPOINT ["/bin/api"]

FROM debian:bookworm-slim AS worker
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY --from=worker-builder /bin/worker /bin/worker
ENTRYPOINT ["/bin/worker"]

FROM debian:bookworm-slim AS mock-ai
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
COPY --from=mock-ai-builder /bin/mock-ai /bin/mock-ai
EXPOSE 8001
ENTRYPOINT ["/bin/mock-ai"]
