# Webhooks and public API docs plan

Scope: a workspace registers HTTPS URLs that Tether POSTs signed JSON to when issues and comments change, with
retries and a delivery log. Plus a public API reference page, since personal API tokens already sign in any `/api`
request (`Authorization: Bearer tth_…`).

Out of scope: OAuth apps, per-webhook project filters, events beyond the four below (projects, cycles, chat,
feedback), events from private projects (see Events), an OpenAPI spec, SDKs, API rate limits beyond what exists.

## Events

Webhook events come from the database, so every writer (UI, MCP, GitHub/GitLab, cycles rollover, AI tests) is
covered without touching the issue package:

| Event | Fired by | Extra payload |
|---|---|---|
| `issue.created` | `insert into issue_events` with kind `created` | — |
| `issue.updated` | any other `insert into issue_events` (status, assignee, priority, title, labels, due date, cycle, link, test run, PR …) | `change: {"kind": "<activity kind>", "data": <the activity entry's data>}` — one delivery per logged change |
| `issue.deleted` | `delete from issues` | — |
| `comment.created` | `insert into comments` | `comment: {"id"}` |
| `ping` | "Send test" | — |

Description edits are logged by bumping an existing row within 10 minutes, so they fire `issue.updated` (kind
`description`) at most once per 10 minutes per person. That's fine; document it.

Issues in **private projects are never sent** (`projects.private`), because a webhook is workspace-wide and its
receiver isn't a member. `ponytail:` comment in the trigger: per-webhook opt-in if a customer asks.

Payload (every event):

```json
{
  "id": "<delivery id>",
  "type": "issue.updated",
  "created_at": "RFC3339 UTC",
  "workspace": "acme",
  "actor": {"id": "…", "name": "Ana"} | null,
  "issue": {"id": "…", "identifier": "ENG-12", "title": "…"},
  "change": {"kind": "status", "data": {"from": {…}, "to": {…}}},
  "comment": {"id": "…"}
}
```

`change` only on `issue.updated`, `comment` only on `comment.created`, `issue` absent on `ping`. Receivers fetch the
full issue with the API (`GET /api/workspaces/{slug}/issues/{identifier}`) using an API token.

## Schema

Migration `backend/user/internal/db/migrations/008_webhooks.sql`:

```sql
create table webhooks (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  url          text not null check (char_length(url) <= 2000),
  secret       text not null,  -- sealed with internal/secret (key and AD as gitlink does)
  events       text[] not null check (cardinality(events) > 0),
  active       boolean not null default true,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now()
);
create index on webhooks (workspace_id);

create table webhook_deliveries (
  id             uuid primary key default uuidv7(),
  webhook_id     uuid not null references webhooks on delete cascade,
  event          text not null,
  payload        jsonb not null,
  status         text not null default 'pending' check (status in ('pending', 'delivered', 'failed')),
  attempts       int not null default 0,
  next_at        timestamptz not null default now(),
  response_code  int,
  response_error text,
  delivered_at   timestamptz,
  created_at     timestamptz not null default now()
);
create index on webhook_deliveries (next_at) where status = 'pending';
create index on webhook_deliveries (webhook_id, id desc);
```

Plus a plpgsql function `webhook_enqueue(workspace_id, event, payload)` that inserts one delivery per active webhook
of the workspace whose `events` contains the event (and sets the payload's `id` to the delivery id), and three
`after` row triggers (`issue_events` insert, `issues` delete, `comments` insert) that build the payload by joining
issues/projects/users and skip private projects. `pg_notify('webhooks', '')` from `webhook_enqueue` wakes the worker.

No `system_config` keys. Delivery rows older than 30 days are deleted by the existing `sweep` package.

## Delivery

- Worker in the user service (`webhook.Run(ctx)`, started in `cmd/user/main.go` like the mailer): LISTEN `webhooks`
  plus a 10s tick; claims due rows with `for update skip locked`, so several instances share the queue.
- POST the stored payload with a 10s timeout, no redirects. Headers: `Content-Type: application/json`,
  `User-Agent: Tether-Webhooks/1`, `X-Tether-Event: <event>`, `X-Tether-Delivery: <id>`,
  `X-Tether-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of "<t>.<body>" with the webhook's secret>`.
- 2xx → `delivered`. Anything else (or a network error) → `attempts + 1`, `response_code` / `response_error`
  (first 500 chars) stored, retry after 1m, 5m, 30m, 2h, 6h, 12h (7 attempts in all); when the 7th fails → `failed`.
  Inactive webhooks' pending rows aren't sent (they wait; re-activating resumes them).
- SSRF: URLs must be `https` (plain `http` allowed outside production, as `aitest` does), and must not resolve to
  private, loopback or link-local addresses — reuse `aitest`'s `privateHost`/dial guard (move the shared bit to
  `web` or a tiny package rather than copy). Checked when saving and again at dial time. Dev escape hatch: the same
  allow-private flag `aitest` uses.
- Realtime: the worker publishes `…/webhooks` and `…/webhooks/{id}/deliveries` (kind `webhooks.changed`, audience:
  the workspace) when a delivery's status changes.

## API contract

FormData in, JSON out. Every endpoint needs `integrations.manage` (403
`{"error":"You don't have permission to do that."}`); its catalog label becomes "Manage GitHub, GitLab and
webhooks". Unknown webhook or delivery: 404 `{"error":"Webhook not found."}` / `{"error":"Delivery not found."}`.

Webhook JSON: `{"id","url","events":[…],"active","created_at","last_delivery":{"status","response_code","created_at"}|null}`

Delivery JSON: `{"id","event","status","attempts","response_code","response_error","created_at","delivered_at","next_at","payload"}`

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/webhooks` | — | `200 [webhook…]` oldest first | |
| `POST /api/workspaces/{slug}/webhooks` | `url`, `events` (repeated field, from the four event names) | `201 {"webhook": webhook, "secret": "whsec_…"}` — the only time the secret is shown | 400 `{"errors":{…}}`; 409 `{"error":"A workspace can have up to 10 webhooks."}` |
| `POST /api/workspaces/{slug}/webhooks/{id}` | `url`, `events`, `active` (`true`/`false`); only fields sent change | `200 webhook` | 400; 404 |
| `POST /api/workspaces/{slug}/webhooks/{id}/secret` | — | `200 {"secret": "whsec_…"}` (old secret stops working) | 404 |
| `POST /api/workspaces/{slug}/webhooks/{id}/test` | — | `202 {}` (queues a `ping`) | 404 |
| `DELETE /api/workspaces/{slug}/webhooks/{id}` | — | `204` | 404 |
| `GET /api/workspaces/{slug}/webhooks/{id}/deliveries` | — | `200 [delivery…]` newest 50 | 404 |
| `POST /api/workspaces/{slug}/webhooks/{id}/deliveries/{deliveryId}/redeliver` | — | `202 {}` (queues a copy with the same payload and a new id) | 404 |

Validation (trim first): `url`: `"Enter a URL."`, `"Enter a valid URL."`, `"Use an https URL."`,
`"This URL points to a private address."`; `events`: `"Choose at least one event."`, unknown names
`"Choose events from the list."`.

Secret: `whsec_` + 32 random bytes base64url (`web.RandomToken`).

## Frontend (user app)

- `src/api.ts`: `Webhook`, `WebhookDelivery`, `WebhookEvent` types.
- **Settings → Webhooks** (`routes/_app/$workspace/_main/settings/webhooks.tsx`, parts in
  `components/settings/webhooks*.tsx`; nav item after Integrations, shown with `integrations.manage`):
  - List: URL, event chips, active switch (checkbox), last delivery status (green delivered, pink failed, secondary
    pending) with relative time; "Add webhook" button; empty state with a link to the API docs' webhooks section.
  - Add/edit dialog: URL input, event checkboxes with one-line descriptions. After create, a dialog shows the secret
    once with Copy ("Copy it now; you won't see it again.").
  - Webhook detail (`…/settings/webhooks/$id`): URL, events, active, "Send test", "Roll secret" (confirm, then shows
    the new secret once), "Delete" (confirm); deliveries table (event, status, response code, attempts, time) with a
    row expanding to the payload (pretty JSON in Berkeley Mono) and the error, and "Redeliver". Live via `loaderApi`.
- **API docs page** `routes/docs.api.tsx` (`/docs/api`, public, outside `_app`), parts in `components/docs/`:
  sections Authentication (create a token in Settings → API tokens, `Authorization: Bearer tth_…`), Conventions
  (base URL `{origin}/api`, FormData or urlencoded in, JSON out, error shapes `{"error"}` / `{"errors":{field}}`,
  status codes, cursors/paging where they exist, workspace slug in paths, issue identifiers like `ENG-12`), a
  Reference of the main endpoints (workspaces, projects, issues incl. list filters, comments, labels/statuses,
  cycles, feedback, inbox) as method + path + fields + response tables — read the backend `routes.go` and
  controllers (read-only) so every listed endpoint and field is real — and Webhooks (events table, payload,
  headers, retries, signature verification examples in Node and Go, the private-project rule). Sticky section nav
  on wide screens. Link to it from Settings → API tokens and Settings → Webhooks.

## Agents

- **Backend**: migration with function and triggers, new `internal/webhook` package (routes/controller/model/
  validate, worker, signing), permission label, SSRF guard shared with `aitest`, sweep of old deliveries, wiring.
  Tests: validation and SSRF rejects, CRUD + permission, secret shown once and rotation, trigger enqueues for each
  event (and not for private projects, inactive webhooks or unsubscribed events), worker against an `httptest`
  server: signature verifies, retry schedule on 500, `failed` after the last attempt, redeliver. `go vet`,
  `go test` with `TEST_DATABASE_URL`, curl against your own instance.
- **Frontend**: everything under Frontend. `npm run build`, `npm run check`.
