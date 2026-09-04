# syntax=docker/dockerfile:1
FROM harbor.astera.sh/dockerhub-cache/golang:1.26-alpine AS builder
WORKDIR /src/backend

COPY go.mod go.sum ./
COPY --from=lib go.mod go.sum /src/lib/
RUN GOWORK=off go mod download

COPY . .
COPY --from=lib . /src/lib/
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags='-s -w' -o /mmseqs-job ./cmd/mmseqs-job

FROM harbor.astera.sh/dockerhub-cache/debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        mmseqs2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /mmseqs-job /app/mmseqs-job
COPY config /app/config
ENTRYPOINT ["/app/mmseqs-job"]
