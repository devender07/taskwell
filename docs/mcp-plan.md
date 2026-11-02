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

## Phase 2: every feature, AI testing first

Same rules as above: tools call the existing REST API with the caller's token, take names and readable ids, and return compact JSON. No API, schema or frontend changes. A tool for an endpoint the caller lacks permission for just relays the API's 403.

### AI testing (the main ask)

Two ways an AI client tests an issue:
1. **Tether runs it**: `run_ai_test` starts an AI run (Tether's own Claude drives the sandboxed Chrome, spends the workspace's monthly limit). `get_test_run` with `wait_seconds` polls until it finishes.
2. **The client runs it**: the client tests with its own tools, then records the outcome as a manual run: `start_manual_test_run` returns one result per case, and `mark_test_result` sets each to passed / failed / skipped with a note.

| Tool | Inputs | Calls |
|---|---|---|
| `list_test_cases` | `workspace`, `identifier` | `GET …/issues/{identifier}/test-cases` (with last result, recorded flag) |
| `create_test_case` | `workspace`, `identifier`, `title`, `steps` ([]string), `expected` | `POST …/issues/{identifier}/test-cases` |
| `create_test_cases` | `workspace`, `identifier`, `cases` ([]{title, steps, expected}) | one POST per case; stops at the first error and says which were created |
| `update_test_case` | `workspace`, `case_id`, `title?`, `steps?`, `expected?` | `POST …/test-cases/{id}` (fields left out keep their value: read the case first, send all three) |
| `delete_test_case` | `workspace`, `case_id` | `DELETE …/test-cases/{id}` |
| `reorder_test_cases` | `workspace`, `identifier`, `case_ids` ([]string, all of them, in order) | `POST …/test-cases/order` |
| `run_ai_test` | `workspace`, `identifier`, `case_id?` (one case only), `regenerate?` (bool: Claude rewrites the cases first) | `POST …/test-runs` `kind=ai` |
| `start_manual_test_run` | `workspace`, `identifier` | `POST …/test-runs` `kind=manual` |
| `mark_test_result` | `workspace`, `run_id`, `result_id`, `status` (passed/failed/skipped), `note?` | `POST …/test-runs/{id}/results/{result_id}` |
| `get_test_run` | `workspace`, `run_id`, `wait_seconds?` (0–120: poll every 3 s until the run isn't pending/running) | `GET …/test-runs/{id}`: status, counts, cost, error, each result with summary, reproduction steps and screenshot URLs (absolute, `TETHER_API_URL`-relative paths made app links) |
| `list_test_runs` | `workspace`, `identifier` | `GET …/issues/{identifier}/test-runs` (first page) |
| `cancel_test_run` | `workspace`, `run_id` | `POST …/test-runs/{id}/cancel` |
| `ai_test_usage` | `workspace` | `GET …/ai-usage` |
| `list_test_sites` / `add_test_site` (`base_url`) / `delete_test_site` (`site_id`) | | `…/test-sites` |
| `list_test_accounts` / `save_test_account` (`account_id?`, `label`, `username`, `password?`) / `delete_test_account` (`account_id`) | | `…/test-accounts`; passwords are write-only, as in the API |

A failed result becomes a bug with the existing `create_issue` (the tool description says so: title from the case, description from the summary and reproduction steps).

### Everything else

| Area | Tools |
|---|---|
| Projects | `create_project` (name, key, description?), `update_project` (key, name?, description?), `archive_project`, `unarchive_project` |
| Issues | `delete_issue`; `link_issues` (identifier, type as the API names them, other identifier), `unlink_issues`; `watch_issue` (watch bool); `create_issue` / `update_issue` gain `parent` (identifier) and `cycle` (number, empty clears) |
| Comments | `get_issue` returns comment ids; `update_comment` (comment_id, body), `delete_comment` |
| Activity | `issue_activity` (identifier, `before?`): the timeline's events and comments; `workspace_activity` (`before?`) |
| Labels and statuses | `create_label` (name, color?), `delete_label`; `create_status` (name, category) |
| Cycles | `list_cycles` (project), `get_cycle` (project, number: cycle + its issues), `create_cycle` (project, starts_on, ends_on, name?), `update_cycle`, `delete_cycle`, `rollover_cycle` (project, number, to) |
| Inbox | `list_notifications`, `mark_notifications_read` (ids? or all, as the API takes) |
| Chat | `list_chats`, `read_chat` (chat_id, `before?`), `send_message` (chat_id, body, `parent_id?`), `send_direct_message` (member email or name, body) |
| Members | `invite_member` (email, role name) |

Left out (admin and secrets; add when asked): workspace create/delete/rename, roles, member role changes and removal, Git integrations, notification settings, profile and avatar, file uploads, reactions.

Server: the MCP HTTP client timeout stays 30 s per API call; `get_test_run`'s wait is its own loop with the request's context, so the MCP server's `WriteTimeout` (none set) doesn't cut it.

Verification: `go vet`, `go test` (extend the in-memory test with `create_test_cases` and `get_test_run` waiting on a fake run that finishes on the second poll), then live: create cases on an issue through MCP, record a manual run with mixed results, check the issue's Tests section in the app shows them; start an AI run if a tester and site are configured, else confirm the API's 503/409 message comes through.

## Phase 3: tests view (MCP Apps)

`get_test_run` and `list_test_cases` carry `_meta.ui.resourceUri = ui://tether/tests.html`, so hosts that support MCP Apps (Claude Desktop, claude.ai) render the run or the cases inline; their JSON text stays for text-only clients, and the same JSON goes in `structuredContent` for the view.

- `backend/mcp/internal/server/tests.html`, embedded with `go:embed`: no build step. It imports the ext-apps `App` client from unpkg (pinned `@2.0.4/app-with-deps`; the resource declares `csp.resourceDomains: ["https://unpkg.com"]`). `ponytail:` CDN; bundle it into the file if a host blocks unpkg.
- Run view: status, counts, cost, error; per result its summary or note, reproduction steps (open for failures), the case's steps and expected result, and "Show screenshots". "Refresh" on an unfinished run calls `get_test_run` with a 30 s wait. Cases view: each case with its steps and last result. "Open in Tether" asks the host to open the issue.
- Screenshots need the caller's Tether sign-in, which the view's iframe doesn't have: the app-only tool `get_test_screenshot` (`visibility: ["app"]`, hidden from the model) fetches an `/api/files/` path with the token and returns it as image content. Any other URL is refused.
- Theme: host CSS variables first, DESIGN.md tokens as the fallback for dark and light.
