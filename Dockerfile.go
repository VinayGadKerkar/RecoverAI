FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM builder AS api-builder
RUN go build -o /bin/api ./cmd/api

FROM builder AS worker-builder
RUN go build -o /bin/worker ./cmd/worker

FROM builder AS mock-ai-builder
RUN go build -o /bin/mock-ai ./cmd/mock-ai

FROM alpine:3.20 AS api
COPY --from=api-builder /bin/api /bin/api
EXPOSE 8080
ENTRYPOINT ["/bin/api"]

FROM alpine:3.20 AS worker
COPY --from=worker-builder /bin/worker /bin/worker
ENTRYPOINT ["/bin/worker"]

FROM alpine:3.20 AS mock-ai
RUN apk add --no-cache curl
COPY --from=mock-ai-builder /bin/mock-ai /bin/mock-ai
EXPOSE 8001
ENTRYPOINT ["/bin/mock-ai"]
