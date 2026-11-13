# syntax=docker/dockerfile:1

# Build context: repository root. Inputs are limited by Dockerfile.dockerignore.

FROM node:22.23.3-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402 AS web

WORKDIR /web

RUN corepack enable

COPY apps/work-items/web/package.json apps/work-items/web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY apps/work-items/web/ ./
RUN pnpm build

FROM nginx:1.30.6-alpine@sha256:901e593c95dc85f5a729f5765516321366ef4ac401821431d408897e95287d0a AS runtime

RUN rm -f /etc/nginx/conf.d/default.conf

COPY infra/security-lab/ingress/nginx.conf /etc/nginx/nginx.conf
COPY infra/security-lab/ingress/keycloak-upstream.conf /etc/nginx/keycloak-upstream.conf
COPY infra/security-lab/ingress/api-upstream.conf /etc/nginx/api-upstream.conf
COPY infra/security-lab/ingress/oauth2-proxy-upstream.conf /etc/nginx/oauth2-proxy-upstream.conf
COPY --from=web /web/dist/ /usr/share/nginx/html/

# Runs as the image's unprivileged nginx user; see compose.yaml.
EXPOSE 8443
