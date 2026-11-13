# Estimates and cycle velocity plan

Optional story points on issues, a cycle's points planned vs. done with a burndown chart, and a project's velocity
across its last cycles.

Out of scope: custom point scales, estimates in bulk actions, sorting or filtering by estimate, estimate on the CSV
import, team-wide velocity across projects.

## Schema: migration `030_issue_estimates.sql`

```sql
alter table issues_all add column estimate smallint check (estimate in (1, 2, 3, 5, 8));
-- issues is `select * from issues_all where deleted_at is null`; a view's * is fixed when it's created.
create or replace view issues as select * from issues_all where deleted_at is null;

-- A cycle's points at the end of each day, written by the snapshot job, for the burndown.
create table cycle_points (
    cycle_id uuid not null references cycles (id) on delete cascade,
    day date not null,
    scope integer not null,  -- estimated points in the cycle, canceled issues left out
    done integer not null,   -- of those, in a completed status
    primary key (cycle_id, day)
);
```

Check the migration applies on a database with data and that every query selecting from `issues` still works (the
recreated view must keep the old column order, with `estimate` added at the end).

No `system_config` keys. One env var: `CYCLE_POINTS_INTERVAL` (default `1h`).

## Issue API

- Every issue JSON (list, board, get, create/update answers, search where issues are returned in full) gains
  `"estimate": 1 | 2 | 3 | 5 | 8 | null`.
- Create and update take an optional `estimate` field: `""` clears it, otherwise one of the values; anything else
  `400` `{"errors": {"estimate": "Choose 1, 2, 3, 5 or 8 points."}}`. Same permission as editing the issue.
- A change logs an activity event, kind `estimate`, data `{"from": n|null, "to": n|null}` (the events.go diff).
- MCP (`backend/mcp`): `create_issue` and `update_issue` accept `estimate` (number, or 0 to clear for update); the
  issue output includes it.

## Cycle API

- Cycle JSON's `progress` gains `"points": {"total": n, "started": n, "completed": n}`: sums of estimates of the
  cycle's issues, canceled issues left out of all three. Every endpoint that returns cycles includes it.
- New `GET /api/workspaces/{slug}/projects/{key}/cycles/{number}/burndown` (same access as the cycle):
  - `200` `{"planned": n | null, "days": [{"date": "YYYY-MM-DD", "scope": n, "done": n}]}`: the recorded days from
    `starts_on` to the earlier of today and `ends_on`, with today computed live (not from the table) while the cycle
    is current. `planned` is the scope on `starts_on` if recorded, else the earliest recorded day's, null when nothing
    is recorded. Missing days are simply absent.
  - `404` as for the cycle.

## Snapshot job

`cycle.Run(ctx, every)` (wired in `cmd/user/main.go` like `templateH.Run`): a minute after start, then every
`CYCLE_POINTS_INTERVAL`, upsert today's `cycle_points` row for every current cycle (state current by UTC date) in one
statement. Ended cycles keep their last row. The sweep needs no change (rows go with their cycle).

## Velocity

No endpoint: the cycles list already carries `progress.points.completed` per cycle. Velocity is the average
completed points of the project's last 3 past cycles (fewer when there are fewer), shown on the cycles page.

## Frontend

- **Issue detail sidebar:** an "Estimate" field (select: No estimate, 1, 2, 3, 5, 8 points), saved like the other
  sidebar fields.
- **Create issue form:** the same select.
- **List rows and board cards:** a small estimate chip (the number) when set, after the priority; nothing when not.
- **Timeline:** "changed the estimate from 2 to 5 points", "set the estimate to 3 points", "removed the estimate".
- **Cycle page:** under the progress, "12 of 30 points done" (hidden when the cycle has no estimates) and a
  burndown chart: x = days of the cycle, an ideal line from `planned` on the first day to 0 on the last, and the
  remaining points (`scope - done`) per recorded day. Inline SVG, tokens from DESIGN.md, a text summary for screen
  readers, an empty state ("Add estimates to issues in this cycle to see the burndown.").
- **Cycles page:** "Velocity: 14 points per cycle" (last 3 past cycles) and a small bar chart of completed points for
  up to the last 6 past cycles; hidden when no past cycle has points.
- **API reference** (`components/docs/reference.ts`): document `estimate` on issues and the burndown endpoint.
- `src/api.ts`: `estimate` on `Issue`, `points` on cycle progress, a burndown type and path.

## Verification

- `go vet`, `go test`: estimate validation and clearing, the activity event, cycle points sums (canceled left out),
  the snapshot upsert (twice in a day keeps one row), burndown planned/days/live today, the view still serving every
  issue query.
- curl against a running API; MCP tests still pass.
- Frontend: `npm run build`.
