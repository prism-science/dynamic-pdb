# syntax=docker/dockerfile:1
# Flyway image with SQL bundled. The container expects FLYWAY_URL,
# FLYWAY_USER, FLYWAY_PASSWORD at runtime (provided by the K8s Job from
# the `extdb` secret). LOCATIONS/SCHEMAS are baked in.

# Base-image registry. The default is Harbor's cache of Docker Hub; CI builds pass the
# upstream registry.
ARG DOCKERHUB_REGISTRY=harbor.astera.sh/dockerhub-cache

FROM ${DOCKERHUB_REGISTRY}/flyway/flyway:11
WORKDIR /flyway
COPY db /flyway/sql/db
COPY conf/flyway.toml /flyway/conf/flyway.toml
ENV FLYWAY_OUT_OF_ORDER=true
ENV FLYWAY_SCHEMAS=public
ENV FLYWAY_LOCATIONS=filesystem:/flyway/sql/db/public/structure,filesystem:/flyway/sql/db/public/functions,filesystem:/flyway/sql/db/public/data/common,filesystem:/flyway/sql/db/public/data/prod
ENTRYPOINT ["flyway"]
CMD ["migrate"]
