# syntax=docker/dockerfile:1

FROM golang:1.24.2-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/demo-api \
    ./cmd/api

FROM alpine:3.22 AS runtime

RUN addgroup -S app \
    && adduser -S -G app app

WORKDIR /app

COPY --from=build --chown=app:app /out/demo-api /app/demo-api

ENV PORT=8080

EXPOSE 8080

USER app

HEALTHCHECK --interval=10s --timeout=3s --start-period=3s --retries=3 \
    CMD wget -q --spider "http://127.0.0.1:${PORT}/health" || exit 1

ENTRYPOINT ["/app/demo-api"]
# Issue #43 CI classification experiment.
