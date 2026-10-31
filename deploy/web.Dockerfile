# Builds both SPAs and serves them with Caddy (deploy/Caddyfile): docker build -f deploy/web.Dockerfile .
FROM node:22-alpine AS build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
COPY frontend/shared/package.json shared/
COPY frontend/admin/package.json admin/
COPY frontend/user/package.json user/
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM caddy:2-alpine
COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /src/frontend/user/dist /srv/user
COPY --from=build /src/frontend/admin/dist /srv/admin
