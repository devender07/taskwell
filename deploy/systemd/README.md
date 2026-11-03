# Run the Go services with systemd

The user API, admin API and MCP server as plain binaries on the server, each a systemd service with its own env file.
Postgres runs in Docker beside them (`deploy/postgres/`), bound to `127.0.0.1:5432`. A reverse proxy (Caddy) in front
serves the frontends and proxies to these ports.

| Service | Binary | Port | Env file | Internal metrics |
| --- | --- | --- | --- | --- |
| `tether-user` | `/opt/tether/tether-user` | 8090 | `/etc/tether/user.env` | `127.0.0.1:9090` |
| `tether-admin` | `/opt/tether/tether-admin` | 8080 | `/etc/tether/admin.env` | `127.0.0.1:9091` |
| `tether-mcp` | `/opt/tether/tether-mcp` | 8095 | `/etc/tether/mcp.env` | — |

Keep 8090, 8080, 8095 and the metrics ports closed in the firewall; only the proxy talks to them.

## Files

| File | |
| --- | --- |
| `deploy.sh` | Run on your machine: builds the three binaries and sends everything below to the server's `/tmp/tether-deploy` |
| `install.sh` | Run on the server with sudo: installs, then restarts the services |
| `deploy-web.sh` | Run on your machine: builds both frontends and sends them to the server as one archive |
| `tether-*.service` | The systemd units |
| `user.env`, `admin.env`, `mcp.env` | The real env files (git-ignored), secrets already generated |
| `Caddyfile` | `/etc/caddy/Caddyfile`: HTTPS, the frontends from `/var/www/tether`, proxies to the services |
| `Caddyfile.ip` | The same over plain HTTP on the server IP, for before you have a domain |

## 1. Before the first deploy

In `user.env` and `admin.env`, replace `<POSTGRES_PASSWORD>` in `DATABASE_URL` with the password from
`/opt/tether-db/.env` on the server. In `mcp.env`, set `APP_URL` to your public app URL.

The secrets (`JWT_SECRET`, `TEST_SECRET_KEY`, `INTEGRATION_SECRET_KEY`, `ADMIN_JWT_SECRET`, `MFA_ENCRYPTION_KEY`) are
already generated, each a different `openssl rand -base64 48`. Back the three files up somewhere safe: losing a key
makes what it encrypted unreadable.

## 2. Send (on your machine)

```sh
deploy/systemd/deploy.sh                       # to devender@185.67.20.135
deploy/systemd/deploy.sh someone@other-host    # another server
GOARCH=arm64 deploy/systemd/deploy.sh          # an ARM server
```

It builds `backend/dist/tether-{user,admin,mcp}` (static Linux binaries, migrations embedded in `tether-user`) and
copies them with the units, env files and `install.sh` to `/tmp/tether-deploy` on the server.

## 3. Install (on the server)

```sh
ssh devender@185.67.20.135
sudo sh /tmp/tether-deploy/install.sh
```

It creates the `tether` system user if missing, installs the binaries to `/opt/tether`, the units to
`/etc/systemd/system`, and the env files to `/etc/tether` (mode 600) on the first install only, then enables and
restarts the services, `tether-user` first so it applies the migrations, and prints their status and the user API's
`/healthz` (expect `200`).

Later deploys keep `/etc/tether/*.env` as they are on the server. To replace them with this folder's copies:
`sudo sh /tmp/tether-deploy/install.sh --env`.

Check on the server:

```sh
systemctl status tether-user tether-admin tether-mcp
journalctl -u tether-user -f          # logs, same for tether-admin and tether-mcp
curl -s localhost:8090/healthz -o /dev/null -w '%{http_code}\n'
```

The admin service refuses to start until the user service has applied the migrations it needs, which is why
`tether-user` restarts first.

## 4. Caddy (HTTPS and the frontends)

Caddy runs on the server itself (not in Docker), gets HTTPS certificates, serves the built frontends and proxies to the
services above. Point both hostnames' DNS at the server first (in Cloudflare, grey cloud until the certificates exist).

Install (Debian / Ubuntu, Caddy's official repository):

```sh
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list
sudo apt update && sudo apt install -y caddy
sudo ufw allow 80,443/tcp && sudo ufw allow 443/udp     # if ufw is on
```

Frontends: build on your machine (`cd frontend && npm run build`) and send them:

```sh
deploy/systemd/deploy-web.sh
```

It builds both frontends on your machine (`npm run build` in `frontend/`), packs `user/dist` and `admin/dist` into
`backend/dist/tether-web.tar.gz` and copies it to the server's `/tmp`. The server needs no Node.js: the files are
static. Then on the server:

```sh
sudo rm -rf /var/www/tether/user /var/www/tether/admin
sudo mkdir -p /var/www/tether && sudo tar -xzf /tmp/tether-web.tar.gz -C /var/www/tether && rm /tmp/tether-web.tar.gz
```

Config: in this folder's `Caddyfile`, replace `tether.ai`, `admin.tether.ai` and `S3_ORIGIN` (the origin of
`s3_endpoint`, e.g. `<accountid>.r2.cloudflarestorage.com`), then:

```sh
scp deploy/systemd/Caddyfile devender@185.67.20.135:/tmp/
ssh devender@185.67.20.135
sudo cp /tmp/Caddyfile /etc/caddy/Caddyfile
sudo caddy validate --config /etc/caddy/Caddyfile && sudo systemctl reload caddy
journalctl -u caddy -f        # watch it get the certificates
```

Open `https://tether.ai` (landing page) and `https://admin.tether.ai`. A frontend update is `deploy/systemd/deploy-web.sh` plus the two
server lines again; Caddy needs no reload.

### No domain yet

`Caddyfile.ip` serves everything over plain HTTP on the server's IP: the app at `http://185.67.20.135`, the admin app
at `http://185.67.20.135:8081`. Use it instead of `Caddyfile` in the steps above, then:

```sh
sudo ufw allow 80/tcp && sudo ufw allow 8081/tcp
sudo ufw deny 8080/tcp && sudo ufw deny 8090/tcp && sudo ufw deny 8095/tcp   # the services listen on every interface
sudo docker exec -it tether-postgres psql -U tether -d tether \
  -c "update system_config set value = 'http://185.67.20.135' where key = 'app_base_url';"
```

`app_base_url` starting with `http://` makes the sign-in cookies work without HTTPS; `APP_URL` in `mcp.env` is the same
address. Without HTTPS, passwords travel unencrypted, Google sign-in won't work (Google requires HTTPS redirect URLs)
and Claude's MCP connection may refuse an `http://` server, so use this only to try things out. When you have a domain:
switch to `Caddyfile`, set `app_base_url` and `APP_URL` to `https://your-domain`, and reload Caddy.

## 5. First admin

Sign up in the app (email needs SMTP set in `system_config` first: `smtp_host`, `smtp_port`, `smtp_username`,
`smtp_password`, `smtp_from`, and `app_base_url`), then:

```sh
sudo systemd-run --pty --wait --uid=tether -p EnvironmentFile=/etc/tether/admin.env \
  /opt/tether/tether-admin -promote you@example.com
```

It prints an authenticator setup key and recovery codes, shown only once.

## Update

Run the same two steps: `deploy/systemd/deploy.sh` on your machine, then `sudo sh /tmp/tether-deploy/install.sh` on the
server. Restarting `tether-user` applies new migrations before it starts. Migrations only add (never drop or rename in
the same release), so the old binaries keep working until they restart.
