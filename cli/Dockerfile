# syntax=docker/dockerfile:1
# Base-image registries. The defaults are Harbor's caches of Docker Hub and gcr.io; CI
# builds pass the upstream registries.
ARG DOCKERHUB_REGISTRY=harbor.astera.sh/dockerhub-cache
ARG GCR_REGISTRY=harbor.astera.sh/gcr-cache

FROM ${DOCKERHUB_REGISTRY}/golang:1.26-alpine AS builder
WORKDIR /src/cli

COPY go.mod go.sum ./
COPY --from=lib go.mod go.sum /src/lib/
RUN GOWORK=off go mod download

COPY . .
COPY --from=lib . /src/lib/
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath \
    -ldflags="-s -w -X dynamic-pdb/cli/internal/version.Version=${VERSION} -X dynamic-pdb/cli/internal/version.Commit=${COMMIT} -X dynamic-pdb/cli/internal/version.Date=${DATE}" \
    -o /dynamic-pdb ./cmd/dynamic-pdb

FROM ${GCR_REGISTRY}/distroless/static-debian13:nonroot
COPY --from=builder /dynamic-pdb /dynamic-pdb
USER nonroot:nonroot
ENTRYPOINT ["/dynamic-pdb"]
