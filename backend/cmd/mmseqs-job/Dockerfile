# syntax=docker/dockerfile:1
# Base images. The deploy build uses these defaults (Harbor's cache of Docker Hub). The
# integration test overrides them where Harbor is unreachable, such as GitHub-hosted runners.
ARG GO_IMAGE=harbor.astera.sh/dockerhub-cache/golang:1.26-alpine
ARG RUNTIME_IMAGE=harbor.astera.sh/dockerhub-cache/debian:bookworm-slim

FROM ${GO_IMAGE} AS builder
WORKDIR /src/backend

COPY go.mod go.sum ./
COPY --from=lib go.mod go.sum /src/lib/
RUN GOWORK=off go mod download

COPY . .
COPY --from=lib . /src/lib/
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags='-s -w' -o /mmseqs-job ./cmd/mmseqs-job

FROM ${RUNTIME_IMAGE}
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        mmseqs2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /mmseqs-job /app/mmseqs-job
COPY config /app/config
ENTRYPOINT ["/app/mmseqs-job"]
