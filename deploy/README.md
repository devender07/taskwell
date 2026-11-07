# Deploy

One Ubuntu 24.04 server runs the three Go services (systemd), Postgres (Docker) and Caddy (HTTPS). The frontends are
Cloudflare Workers serving static assets. Every script runs from your machine and takes `user@host` (default `devender@ftp.fotospace.co.in`).

| Host | Points at | Serves |
| --- | --- | --- |
| `taskwell.dev` | Cloudflare Pages | user app (`frontend/user`) |
| `admin.taskwell.dev` | Cloudflare Pages | admin app (`frontend/admin`) |
| `api.taskwell.dev` | server, A record | user API, `127.0.0.1:8090` |
| `admin-api.taskwell.dev` | server, A record | admin API, `127.0.0.1:8080` |
| `mcp.taskwell.dev` | server, A record | MCP server, `127.0.0.1:8095` |

## Before you start

- **The server:** a user with sudo and your SSH key (`ssh-copy-id devender@<host>`).
- **DNS:** the three A records, set to DNS only (grey cloud) until Caddy has its certificates.
- **Env files:** `deploy/systemd/{user,admin,mcp}.env` (git-ignored, the secrets are real). Back them up: losing a key makes what it encrypted unreadable. `MFA_ENCRYPTION_KEY` must be the same in `user.env` and `admin.env`.
- **System config:** `deploy/config/system.json`, copied from `system.json.example`. Set `app_base_url` (it must start with `https://`) and the SMTP, Google and S3 values; an empty one turns that feature off. For Google, the redirect URI is `https://api.taskwell.dev/api/auth/google/callback`.

## New server, in order

```sh
deploy/server-setup.sh                       # updates, firewall, Docker, Caddy, Postgres, backups
deploy/config.sh                             # units, /etc/taskwell/*.env, Caddyfile
deploy/build.sh prod user backend  && deploy/send.sh prod user backend    # applies the migrations
deploy/system-config.sh                      # system.json into system_config
ssh -t devender@<host> sudo systemctl restart taskwell-user
deploy/build.sh prod admin backend && deploy/send.sh prod admin backend
deploy/build.sh prod mcp backend   && deploy/send.sh prod mcp backend
scp deploy/create-admin.sh devender@<host>:/tmp/ && ssh -t devender@<host> 'sudo sh /tmp/create-admin.sh you@example.com "Your name"'
```

The user API won't start until `app_base_url` is `https://`. That is why `system-config.sh` runs after the first user
deploy (which creates the tables) and is followed by a restart. `create-admin.sh` prints the authenticator setup key and
recovery codes once, so save them.

Frontends: `deploy/frontend.sh user` (or `admin`) builds into `dist/prod/<app>/web` and deploys it to the app's
Cloudflare Worker (assets only, `frontend/<app>/wrangler.toml`: `taskwell` at taskwell.dev, `admin-taskwell` at
admin.taskwell.dev). Log in once with `npx wrangler@4 login`.

## Check

```sh
curl -s -o /dev/null -w '%{http_code}\n' https://api.taskwell.dev/healthz        # 200
curl -s -o /dev/null -w '%{http_code}\n' https://admin-api.taskwell.dev/healthz  # 200
curl -s -o /dev/null -w '%{http_code}\n' https://mcp.taskwell.dev/healthz        # 200
ssh devender@<host> 'systemctl is-active taskwell-user taskwell-admin taskwell-mcp caddy docker'
ssh -t devender@<host> 'sudo journalctl -u taskwell-user -n 50 --no-pager'          # when one isn't active
```

## Updates

The same `build.sh` and `send.sh` pair. Sending the user app runs new migrations before it starts. Change env files or
the Caddyfile, then run `deploy/config.sh`; it restarts only what changed.

## File CDN and direct uploads

Browsers upload straight to the R2 bucket with presigned PUTs, and files are served by a Cloudflare Worker at
`files.taskwell.dev` from Cloudflare's edge cache (docs/cdn-direct-uploads-plan.md). Once:

1. **`s3_endpoint` without the bucket:** `https://<account id>.r2.cloudflarestorage.com` (the app adds `/taskwell`).
2. **Bucket CORS**, so browsers may PUT: the user API sets it on startup when its R2 token may; if the log says it
   couldn't, set it in the R2 dashboard (bucket → Settings → CORS policy):
   `[{"AllowedOrigins":["https://taskwell.dev"],"AllowedMethods":["PUT","GET"],"AllowedHeaders":["*"],"MaxAgeSeconds":3600}]`
3. **The Worker**, from `cdn/` (logged in with `npx wrangler@4 login`):
   ```sh
   openssl rand -base64 48                        # the shared secret
   npx wrangler@4 secret put FILES_SECRET         # paste it
   npx wrangler@4 deploy                          # also creates the files.taskwell.dev custom domain
   ```
4. **System config** (admin app → Config): `files_cdn_url` = `https://files.taskwell.dev`, `files_cdn_secret` = the
   same secret. Until both are set, files are served by presigned R2 links as before.

Check: open an image in the app; in the browser's network panel it should redirect from `api.taskwell.dev/api/files/…`
to `files.taskwell.dev/…` and load with `200`.
Worker logs: `npx wrangler@4 tail` from `cdn/`. Worker check: `node cdn/worker.check.mjs`.
