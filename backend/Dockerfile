# syntax=docker/dockerfile:1
FROM harbor.astera.sh/dockerhub-cache/golang:1.26-alpine AS builder
WORKDIR /src

COPY tools/go.mod tools/go.sum ./tools/
RUN cd tools && GOWORK=off go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen

COPY go.mod go.sum ./
RUN GOWORK=off go mod download

COPY . .
RUN GOWORK=off go generate ./...
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags='-s -w' -o /server ./cmd/server

FROM harbor.astera.sh/gcr-cache/distroless/static-debian13:nonroot
WORKDIR /app
COPY --from=builder /server /app/server
COPY --from=builder /src/config /app/config
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
