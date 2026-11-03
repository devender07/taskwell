# MCP OAuth plan

Scope: connecting an MCP client (Claude Code, Claude Desktop, claude.ai) to Tether without copying a token. The client
finds Tether's OAuth server from the MCP endpoint, registers itself, opens the user's browser on a Tether consent page
(signing in first if needed), and receives a token. That token is an ordinary personal API token (`tth_…`), so it
shows in Settings → API tokens and can be revoked there, and the MCP server keeps forwarding it to the API unchanged.
Home gets a card with the MCP URL and the `claude mcp add` command.

Follows the MCP authorization spec: OAuth 2.1 authorization code with PKCE (S256 only), RFC 8414 authorization
server metadata, RFC 9728 protected resource metadata, RFC 7591 dynamic client registration. Public clients only
(no client secret).

Out of scope: refresh tokens and token expiry (personal tokens don't expire; revoke in Settings), scopes beyond one
`tether` scope (the token acts as the user, like a personal token), consent memory (every authorization asks), admin
app, client management UI.

## Flow

1. Client calls `POST {app}/mcp` without a token. MCP server answers `401` with
   `WWW-Authenticate: Bearer resource_metadata="{app}/.well-known/oauth-protected-resource/mcp"`.
2. Client reads that document, then `{app}/.well-known/oauth-authorization-server`.
3. Client registers at `POST {app}/api/oauth/register`, gets a `client_id`.
4. Client opens the browser at `{app}/oauth/authorize?response_type=code&client_id=…&redirect_uri=http://localhost:PORT/callback&code_challenge=…&code_challenge_method=S256&state=…`.
5. The SPA page signs the user in if needed (back to the same URL), shows the consent card, and on Allow posts to the
   API, which returns the redirect URL with `code` and `state`. The page navigates there.
6. Client exchanges the code at `POST {app}/api/oauth/token` with the `code_verifier`, gets `access_token` (a new
   personal API token named after the client), and calls `/mcp` with `Authorization: Bearer tth_…`.

`{app}` is `system_config` `app_base_url` (user service) and `APP_URL` (MCP server); the same origin serves the SPA,
`/api`, `/mcp` and `/.well-known/…` (Caddy in production, Vite's proxy in dev).

## Schema (migration `010_oauth.sql`)

```sql
create table oauth_clients (
  id uuid primary key default gen_random_uuid(),
  name text not null,                 -- client_name, max 100 chars; "MCP client" when absent
  redirect_uris text[] not null,      -- 1-5 entries
  created_at timestamptz not null default now()
);
create table oauth_codes (
  code_hash bytea primary key,        -- secret.Hash of the code
  client_id uuid not null references oauth_clients on delete cascade,
  user_id uuid not null references users on delete cascade,
  redirect_uri text not null,
  code_challenge text not null,       -- S256
  expires_at timestamptz not null     -- now() + 10 minutes; deleted when redeemed
);
```

The sweep deletes expired codes, and clients older than 30 days with no codes (registrations are free and unauthenticated).

No `system_config` keys.

## API contract (user service)

Errors on the `/api/oauth/*` JSON endpoints used by the SPA follow the app's usual `{"error"}` / `{"errors"}` shape.
The register and token endpoints, which clients call, use RFC error JSON: `{"error": "<code>", "error_description": "…"}`.

| Method & path | Auth | Request | Response |
|---|---|---|---|
| `GET /.well-known/oauth-authorization-server` | none | | `200` `{issuer: app, authorization_endpoint: app+"/oauth/authorize", token_endpoint: app+"/api/oauth/token", registration_endpoint: app+"/api/oauth/register", response_types_supported: ["code"], grant_types_supported: ["authorization_code"], code_challenge_methods_supported: ["S256"], token_endpoint_auth_methods_supported: ["none"], scopes_supported: ["tether"]}` |
| `POST /api/oauth/register` | none, rate limited per IP (10/hour) | JSON `{client_name?, redirect_uris: [..]}` | `201` `{client_id, client_name, redirect_uris, token_endpoint_auth_method: "none", grant_types: ["authorization_code"], response_types: ["code"]}`; `400 invalid_redirect_uri` / `invalid_client_metadata`; `429` |
| `GET /api/oauth/authorize?client_id&redirect_uri&response_type&code_challenge&code_challenge_method&state&scope` | signed in | query as in step 4 | `200` `{client_name, redirect_uri}`; `400 {"error": "…"}` (unknown client, redirect URI not registered, response_type not code, missing challenge, method not S256) |
| `POST /api/oauth/authorize` | signed in | FormData: the same fields plus `allow` (`true`/`false`) | `200` `{redirect}`: `redirect_uri?code=…&state=…` when allowed, `redirect_uri?error=access_denied&state=…` when not; `400` as above |
| `POST /api/oauth/token` | none | form-encoded `grant_type=authorization_code, code, redirect_uri, client_id, code_verifier` | `200` `{access_token, token_type: "Bearer", scope: "tether"}` (`Cache-Control: no-store`); `400 invalid_grant` (unknown, expired or used code, redirect/client mismatch, PKCE failure), `invalid_request`, `unsupported_grant_type`; `400 invalid_grant` with description "Too many API tokens. Revoke one in Settings → API tokens." at the token cap |

Redirect URIs: `http://localhost`, `http://127.0.0.1` or `http://[::1]` with any port and path, or any `https://` URL.
No fragments. Must match a registered one exactly.

Redeeming a code deletes it in the same transaction that creates the token, so a code works once.

## MCP server

- `POST /mcp` (and any `/mcp` request) without `Authorization: Bearer …` → `401` with the `WWW-Authenticate` header
  above and no body work. A rejected token (the API answers 401) keeps today's behaviour.
- `GET /.well-known/oauth-protected-resource/mcp` and `GET /.well-known/oauth-protected-resource` → `200`
  `{resource: APP_URL+"/mcp", authorization_servers: [APP_URL], bearer_methods_supported: ["header"], scopes_supported: ["tether"]}`.

## Routing

- Caddy (`deploy/Caddyfile`, app host): `/.well-known/oauth-protected-resource*` → mcp:8095,
  `/.well-known/oauth-authorization-server*` → user:8090. `/oauth/authorize` is an SPA route (falls through to index.html).
- Vite dev (`frontend/user/vite.config.ts`): the same two `/.well-known/…` proxies.

## Frontend

- Route `/oauth/authorize` (in `routes/(auth)/`, outside a workspace). Not signed in → `/signin` with a redirect back
  to this exact URL. Loader calls `GET /api/oauth/authorize` with the query. Card: heading "Connect {client_name} to
  Tether?", body "It will act as you: see and change the issues, comments and chats you can. It sends you back to
  {redirect host}." Buttons "Allow" (primary) and "Cancel" (ghost). Both post `POST /api/oauth/authorize` and then
  `window.location.assign(redirect)`; afterwards the page says "You can close this tab." An API error shows in the card
  with no buttons.
- Settings → API tokens: OAuth tokens already appear (named after the client). No change needed beyond what the list
  shows today.
- Home: a "Connect Claude" card with the MCP URL (`{location.origin}/mcp`) and the command
  `claude mcp add --transport http tether {origin}/mcp`, each with a copy button, and a one-line hint that the first
  use opens the browser to sign in.
