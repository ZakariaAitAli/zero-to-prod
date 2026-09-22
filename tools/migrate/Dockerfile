# syntax=docker/dockerfile:1

FROM golang:1.27.1-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build

ARG MIGRATE_VERSION=v4.20.1

RUN CGO_ENABLED=0 \
    GOBIN=/out \
    go install \
      -tags 'postgres' \
      -ldflags="-X main.Version=${MIGRATE_VERSION}" \
      github.com/golang-migrate/migrate/v4/cmd/migrate@${MIGRATE_VERSION}

FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8

COPY --from=build /out/migrate /usr/local/bin/migrate

ENTRYPOINT ["migrate"]
