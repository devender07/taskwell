# Manual issue order plan

Scope: members drag issues into any order in a project's list and board, the order is saved on the server and shared
with everyone, and other open views update live. A "Manual" sort shows it; it becomes a project's default sort once
anyone has ordered that project by hand. The MCP server lists in manual order, can reorder, and can place new issues.

Out of scope: per-user orderings, ordering across projects (My issues and Watching don't offer Manual), drag in My
issues / Watching, reordering sub-issues on the issue page.

## Why not the test case pattern

Test cases store an integer `position` and `POST …/test-cases/order` rewrites every case from a full list of ids
(`aitest.orderCases`). That fits a list of at most a few dozen cases that the client always holds in full. A project
has thousands of issues and the client only holds paged slices of filtered groups, so it can't send a full order, and
renumbering every row per move would lock the whole project. Issues therefore get a fractional rank: a move writes one
row. This is the only ordering mechanism for issues; test cases keep theirs.

## Data (migration `011_issue_rank.sql`, written to be safe to run twice)

```sql
alter table projects add column if not exists manual_order boolean not null default false;
alter table issues add column if not exists rank text collate "C";
-- backfill, keeping today's tiebreak order (newest number first) as the initial manual order
update issues i set rank = lpad(o.rn::text, 10, '0') || 'V'
from (select id, row_number() over (partition by project_id order by number desc, id) rn from issues) o
where o.id = i.id and i.rank is null;
alter table issues alter column rank set not null;
-- deferrable, so a rebalance can renumber a whole project in one statement (set constraints … deferred)
do $$ begin
  alter table issues add constraint issues_project_id_rank_key unique (project_id, rank) deferrable initially immediate;
exception when duplicate_table or duplicate_object then null; end $$;
```

- One rank per issue, ordered project-wide with byte (C) collation. A status column (or any group) is the project
  order filtered to that group, so moving within a column or across columns uses the same rank.
- `projects.manual_order` turns true on the project's first move and is exposed on the project JSON as
  `manual_order` (`GET /projects`, `GET /projects/{key}`).

## Rank function (`backend/user/internal/issue/rank.go`)

- Alphabet: `0-9A-Za-z` (62 digits, ascending in C collation). A rank is a non-empty string of digits that doesn't end
  in `0`, read as a base-62 fraction.
- `rankBetween(a, b string) string`: a key strictly between a and b; `a == ""` means before everything, `b == ""`
  means after everything. Midpoint digit by digit; result never ends in `0`; panics only if `a >= b` (callers never
  pass that).
- `rankAt(n int) string`: the evenly spaced key used by the backfill and rebalancing: `lpad(n, 10, '0') + "V"`
  (n from 1). Same format as the migration.
- When a computed rank is longer than 50 characters, the server rebalances the project in the same transaction:
  `set constraints issues_project_id_rank_key deferred`, then one `update` giving the project's issues
  `rankAt(row_number() over (order by rank))`, then recomputes the move.

## Concurrency

Every write that sets a rank (create, move, rebalance) first locks the project row
(`select 1 from projects where id = $1 for update`). Moves in one project are serialized, so two moves never read the
same neighbours and compute equal ranks; the unique index is the last guard. A move is never lost: each one reads the
neighbours' current ranks inside the lock.

## API contract (user service)

### Move

`POST /api/workspaces/{slug}/issues/{identifier}/move` — FormData:

| Field | |
|---|---|
| `after` | identifier of the issue the moved issue should come right after (the one above it), or empty |
| `before` | identifier of the issue it should come right before (the one below it), or empty |
| `status_id`, `assignee_id`, `priority` | optional; when present, applied exactly like `POST …/issues/{identifier}` (same validation, events, notifications) in the same transaction as the rank change |

At least one of `after` / `before` is required. Rank rules, read inside the project lock:
- `after` given: rank = between(after.rank, the next rank in the project after after.rank). `before` is then only used
  to validate; if the client's view was stale and `before` no longer follows `after`, `after` wins.
- only `before`: rank = between(the previous rank in the project before before.rank, before.rank).
- Moving next to itself is a no-op that still returns the issue.

Responses:
- `200` the issue (same JSON as `GET …/issues/{identifier}`, which now includes `rank`).
- `400 {"errors": {"after": "…"}}` / `{"before": "…"}`: neighbour not found, not visible to the caller, or in another
  project ("Pick an issue in the same project."); `{"error": "Pick where to move the issue."}` when both are empty.
  Field errors for `status_id` etc. as the update endpoint gives them.
- `403` without permission to edit the issue (same rule as update: `issues.edit_any`, creator or assignee).
- `404` issue not found / not visible.
- Sets `projects.manual_order = true`. Publishes `issue.moved` with `realtime.IssuePaths` (+ the project path, since
  `manual_order` may flip), like `issue.updated`.

### Create

`POST …/projects/{key}/issues` accepts optional `after` / `before` (identifiers in the same project, same rules as
move). Without them the new issue goes last: rank = between(project max rank, "").

### Lists

- `sort=manual` is added to the paged mode (`sortBy["manual"] = "i.rank"`; ties as today). Valid on every list;
  the frontend offers it only on project and cycle pages.
- Unpaged mode (`GET …/projects/{key}/issues` and the cycle list without `limit`) accepts `sort` with the same values;
  without it the order stays `number desc` (today's behaviour). Issue JSON gains `rank` (string).

## MCP

- `list_issues`: new optional `sort` (`manual` (default), `priority`, `updated`, `created`, `due`); passes `sort` to the
  unpaged API.
- `reorder_issues` (new): `workspace`, `project`, and either `identifiers` (ordered list; the first stays put and each
  next one is moved right after the previous) or `identifier` with `after` / `before`. Calls the move endpoint; returns
  the moved issues in order.
- `create_issue`: optional `after` / `before` identifiers, sent to the create endpoint.

## Frontend

- Sorts: `manual`, `priority`, `updated`, `created`, `due`. The URL keeps `sort` only when it differs from the page's
  default; the default is `manual` on a project or cycle page whose project has `manual_order`, else `priority`
  (My issues / Watching: always `priority`, no Manual option).
- Drag to reorder only when the sort is Manual:
  - Board: the existing pointer-events drag gains a drop position inside the column (insertion line between cards);
    dropping sends `move` with the neighbours and, across columns, the group field (`status_id` / `assignee_id` /
    `priority`) in the same request. Other sorts keep today's behaviour (column change via update).
  - List: rows drag within their group the same way (reuse the board's pointer handling; no new dependency).
  - Optimistic: the moved issue shows at its new place at once (the existing override mechanism), rolls back with the
    error message if the request fails.
- Keyboard: on a focused list row or card with Manual sort, Alt+↑ / Alt+↓ moves it one place within its group (same
  move request), and a live region announces the new position.
- Realtime: `issue.moved` refetches the lists like other issue events (already throttled for list paths).

## Tests

- `rank_test.go`: between two keys, before the first, after the last, 1,000 inserts at the same spot stay strictly
  ordered (and rebalancing kicks in past 50 chars), `rankAt` ordering matches the SQL backfill.
- Move endpoint (dbtest): reorder within a status, move across statuses in one call (status and rank both change,
  one `status` event), neighbour in another project → 400, user who can't edit → 403, can't see → 404, two moves into
  the same gap both land with distinct ranks.
- MCP: `reorder_issues` with an `identifiers` list against a fake API asserts the move calls and their order.
