# syntax=docker/dockerfile:1
FROM harbor.astera.sh/dockerhub-cache/debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        mmseqs2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /data

ENTRYPOINT ["mmseqs"]
CMD ["--help"]
