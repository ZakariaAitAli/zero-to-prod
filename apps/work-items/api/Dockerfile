# syntax=docker/dockerfile:1

FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/work-items-api \
    ./api

FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8 AS runtime

RUN addgroup -S app \
    && adduser -S -G app app

WORKDIR /app

COPY --from=build --chown=app:app /out/work-items-api /app/work-items-api

ENV PORT=8080

EXPOSE 8080

USER app

HEALTHCHECK --interval=10s --timeout=3s --start-period=3s --retries=3 \
    CMD wget -q --spider "http://127.0.0.1:${PORT}/health" || exit 1

ENTRYPOINT ["/app/work-items-api"]
