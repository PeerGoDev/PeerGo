# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS build

WORKDIR /src
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json ./apps/web/package.json
RUN pnpm install --frozen-lockfile
COPY apps/web ./apps/web
COPY contracts/openapi ./contracts/openapi
RUN pnpm --filter @peergo/web build

FROM nginx:1.29-alpine
COPY deploy/docker/web.nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/apps/web/build/client /usr/share/nginx/html
EXPOSE 8080
