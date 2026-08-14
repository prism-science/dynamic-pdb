# syntax=docker/dockerfile:1
FROM harbor.astera.sh/dockerhub-cache/golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN GOWORK=off go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath \
    -ldflags="-s -w -X dynamic-pdb/cli/internal/version.Version=${VERSION} -X dynamic-pdb/cli/internal/version.Commit=${COMMIT} -X dynamic-pdb/cli/internal/version.Date=${DATE}" \
    -o /dynamic-pdb ./cmd/dynamic-pdb

FROM harbor.astera.sh/gcr-cache/distroless/static-debian13:nonroot
COPY --from=builder /dynamic-pdb /dynamic-pdb
USER nonroot:nonroot
ENTRYPOINT ["/dynamic-pdb"]
