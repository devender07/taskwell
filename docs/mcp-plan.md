# MCP server plan

Scope: let AI clients (Claude Code, Cursor, VS Code, Claude Desktop) read and change Tether issues through the Model Context Protocol. Two parts:

1. **Personal API tokens** in the user service: a user creates a token in Settings, and any `/api` request carrying `Authorization: Bearer tth_…` is signed in as that user. Every existing permission check applies unchanged.
2. **MCP server** in a new Go module `backend/mcp/`: a stateless streamable-HTTP MCP endpoint at `/mcp` that forwards the caller's bearer token to the user API. It holds no database connection and no business logic; every tool is one or a few REST calls.

Out of scope: OAuth (needed only for the claude.ai connector directory; add when we want a listing), token scopes or expiry, per-token rate limits, stdio transport, MCP resources/prompts, tools for chat, cycles, AI testing, settings or admin.

## Schema

Migration `backend/user/internal/db/migrations/035_api_tokens.sql`:

```sql
create table api_tokens (
  id           uuid primary key default uuidv7(),
  user_id      uuid not null references users on delete cascade,
  name         text not null check (char_length(name) between 1 and 60),
  token_hash   bytea not null unique,           -- sha256 of the full token
  last_used_at timestamptz,
  created_at   timestamptz not null default now()
);
create index on api_tokens (user_id);
```

Token format: `tth_` + 32 random bytes, base64url without padding. Only the sha256 is stored; the token is shown once.

No `system_config` keys.

## Auth rules (user service)

- `auth.Handler.Authenticate`: when the request has `Authorization: Bearer tth_…`, look up `sha256(token)` joined to `users` with `suspended_at is null`; return that user ID, or `""` (→ 401) when unknown, revoked or suspended. A bearer header that isn't `tth_…` is `""` too. No cookie fallback once a bearer header is present.
- `last_used_at` is updated at most once a minute per token (`… where last_used_at is null or last_used_at < now() - interval '1 minute'`).
- `ponytail:` one indexed lookup per bearer request; cache like `alive` if MCP traffic shows up in DB load.
- The token endpoints below and everything under `/api/auth/` and `/api/me/password` require a cookie session: a bearer request gets 403 `{"error":"Use the Tether app for this."}`. A leaked token can't mint more tokens or change the password.
- `CheckOrigin` needs no change: non-browser requests carry neither `Origin` nor `Sec-Fetch-Site` and already pass.
- Password change / reset keeps tokens (they're revoked individually). Suspension blocks them via the join.
- At most 25 tokens per user.

## API contract (user service)

FormData in, JSON out, same error shapes as everywhere else.

Token JSON: `{"id","name","created_at","last_used_at": string|null}`

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `GET /api/me/tokens` | — | `200 [token…]` newest first | 401; 403 bearer |
| `POST /api/me/tokens` | `name` | `201 {"token": token, "secret": "tth_…"}` | 400 `{"errors":{"name": msg}}`; 401; 403 bearer; 409 `{"error":"You have 25 tokens. Revoke one first."}` |
| `DELETE /api/me/tokens/{id}` | — | `204` | 401; 403 bearer; 404 `{"error":"Token not found."}` (also for another user's id) |

Messages for `name`: `Name the token.` · `Use 60 characters or fewer.`

## MCP server (`backend/mcp/`)

Module `github.com/devender07/tether/backend/mcp`, added to `backend/go.work`. Entry `cmd/mcp/main.go`, logic in `internal/server/` (`server.go` registers tools, `api.go` is the REST client, `text.go` converts descriptions). Dependency: `github.com/modelcontextprotocol/go-sdk` (approved).

Config (env): `PORT` (default `8095`), `TETHER_API_URL` (default `http://localhost:8090`), `APP_ENV`. Logs to stdout with `slog`.

- `POST/GET /mcp`: the SDK's streamable HTTP handler, `Stateless: true`. A request without `Authorization: Bearer tth_…` gets 401 before reaching the SDK. The server per request is built with an API client that carries that request's header, so tools never see other users' tokens.
- `GET /healthz`: 200.
- REST errors become tool results with `IsError: true` and the API's message text (`error`, or the `errors` map joined as `field: message`). 401 from the API → "Your Tether token is invalid or revoked."
- Descriptions and comments in: plain text, split on blank lines into Tiptap paragraph nodes (`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":…}]}]}`). `ponytail:` no markdown; parse it if models send formatting that matters.
- Descriptions and comments out: plain text (a ~25-line port of `doc.PlainText`, since `user/internal` can't be imported).
- Tool outputs are compact JSON (drop ids the model doesn't need except where a later call needs them).

Tools (every one but `list_workspaces` takes `workspace`, the slug). Names, not ids, in inputs: the server resolves status names, label names and assignee (email or name, case-insensitive) through the workspace's statuses / labels / members endpoints, and answers with the valid choices when one doesn't match.

| Tool | Inputs | Calls |
|---|---|---|
| `list_workspaces` | — | `GET /api/workspaces` |
| `list_projects` | `workspace` | `GET …/projects` |
| `workspace_options` | `workspace` | statuses, labels, members (names only) |
| `search_issues` | `workspace`, `query`, `project?` | `GET …/issues?q=&project=` |
| `list_issues` | `workspace`, `project`, `status?`, `assignee?`, `label?`, `priority?` | `GET …/projects/{key}/issues`; filters applied by the API if it supports them, else in the MCP server |
| `my_issues` | `workspace` | `GET …/my-issues` |
| `get_issue` | `workspace`, `identifier` (`ENG-142`) | `GET …/issues/{identifier}` + first page of `…/timeline` (comments as text) |
| `create_issue` | `workspace`, `project`, `title`, `description?`, `status?`, `assignee?`, `priority?` (`none/urgent/high/medium/low` → 0–4 as the API defines), `due_date?` (`YYYY-MM-DD`), `labels?` ([]string) | `POST …/projects/{key}/issues` |
| `update_issue` | `workspace`, `identifier`, same optional fields as create (empty string clears assignee / due date) | `POST …/issues/{identifier}` (only fields given are sent) |
| `add_comment` | `workspace`, `identifier`, `body` | `POST …/issues/{identifier}/comments` |

Tool results include the issue's web URL (`{app_base_url}/{workspace}/issue/{identifier}`) where there is one; the MCP server takes `APP_URL` env (default `http://localhost:5173`) for that.

Deploy: `deploy/backend.Dockerfile` builds it with `SVC=mcp` if the Dockerfile allows, else the smallest change that does; `deploy/docker-compose.prod.yml` gets an `mcp` service (`TETHER_API_URL=http://user:8090`); `deploy/Caddyfile` routes `/mcp` on `APP_HOST` to `mcp:8095` with `flush_interval -1`. Dev: Vite proxies `/mcp` to `http://localhost:8095`.

## Frontend (user app)

`api.ts`: `ApiToken`, `listTokens`, `createToken`, `deleteToken`.

Route `/{workspace}/settings/api-tokens` (`routes/_app/$workspace/_main/settings/api-tokens.tsx`, parts in `components/settings/`), linked in the settings sidebar after "Notifications" as "API tokens".

| Where | Behaviour |
|---|---|
| Page | Title "API tokens", one line: "Tokens let AI tools like Claude Code act as you in Tether. Anyone with a token can do what you can." List rows: name, "Created Nov 2", "Last used 3 hours ago" / "Never used", "Revoke" button. Empty: "No tokens yet." "New token" button (pink CTA). |
| New token dialog | Name input (placeholder `Claude Code on my laptop`), "Create token". On success the dialog shows the secret in a mono read-only field with "Copy", a warning "Copy it now. You won't see it again.", and a setup snippet with copy: `claude mcp add --transport http tether {origin}/mcp --header "Authorization: Bearer {secret}"`. "Done" closes. |
| Revoke | Confirm "Revoke {name}? Tools using it stop working." → delete. |

## Verification

- Backend: `go vet` and `go test` for both modules. User: a test that a bearer token authenticates, and that a revoked token and a suspended user's token don't; curl every token row and message, and an issue create with a bearer token. MCP: one test with an `httptest` fake API and the SDK's in-memory transport, calling `create_issue` with a status name, label names and an assignee email, asserting the form the API receives; plus `curl` an `initialize` and `tools/list` against the running server.
- Frontend: `npm run build`.
- Integration (main session): create a token in Settings, add the server to Claude Code (`claude mcp add …`), list projects, create an issue, update its status, comment; see each live in the app. Revoke the token → the next tool call says the token is invalid.
