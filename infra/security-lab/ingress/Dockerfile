# syntax=docker/dockerfile:1

# Build context: repository root. Inputs are limited by Dockerfile.dockerignore.

FROM node:22.23.3-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402 AS web

WORKDIR /web

RUN corepack enable

COPY apps/work-items/web/package.json apps/work-items/web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY apps/work-items/web/ ./
RUN pnpm build

FROM nginx:1.30.5-alpine@sha256:0985e772fb9f729e6fa0980da05fca5d9c468e870eed43071545afa9d2e27d94 AS runtime

RUN rm -f /etc/nginx/conf.d/default.conf

COPY infra/security-lab/ingress/nginx.conf /etc/nginx/nginx.conf
COPY --from=web /web/dist/ /usr/share/nginx/html/

# Runs as the image's unprivileged nginx user; see compose.yaml.
EXPOSE 8443
