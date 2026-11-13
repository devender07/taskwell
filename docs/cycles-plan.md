# Cycles plan

Scope: time-boxed cycles (sprints) per project. A project has numbered cycles with start and end dates; an issue belongs to at most one cycle of its own project. A cycle page shows its issues in the usual list/board view with progress. When a cycle ends, its open issues can be moved to another cycle in one click.

Out of scope: automatic cycle schedules (cadence, auto-create), automatic rollover, burndown charts, workspace-wide cycles, cycle goals/descriptions, capacity or estimates.

A cycle's state is derived from today's date (UTC): **upcoming** (`starts_on > today`), **current** (`starts_on <= today <= ends_on`), **past** (`ends_on < today`). No stored state, nothing to "start" or "complete". `ponytail:` UTC date; use the viewer's timezone if cycles near midnight confuse people.

## Schema

Migration `backend/user/internal/db/migrations/033_cycles.sql`:

```sql
alter table projects add column cycle_seq int not null default 0; -- last cycle number used

create table cycles (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  project_id   uuid not null references projects on delete cascade,
  number       int not null,
  name         text check (char_length(name) between 1 and 60), -- null shows "Cycle {number}"
  starts_on    date not null,
  ends_on      date not null,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now(),
  unique (project_id, number),
  check (ends_on >= starts_on)
);
create index on cycles (project_id, starts_on);

alter table issues add column cycle_id uuid references cycles on delete set null;
create index on issues (cycle_id);
```

Rules (checked in Go inside the write's transaction, project row locked `for update`):
- Cycles of one project don't overlap (`starts_on <= other.ends_on and ends_on >= other.starts_on` is a conflict). Length 1–90 days.
- An issue's cycle must be in the issue's project. Moving an issue to another project is not a feature today; if it ever is, clear `cycle_id`.
- An archived project's cycles are read-only (same `errArchived` as issues).

Permission: new `cycles.manage` ("Cycles" group, label "Create, edit and delete cycles", active). Not in `memberDefaults`. Setting an issue's cycle uses the existing issue edit rule (creator, assignee or `issues.edit_any`).

## API contract

Same conventions as the issues plan: FormData in, JSON out, 401 / 404 workspace or project / 403 `{"error":"You don't have permission to do that."}` (copy the existing message).

Cycle JSON:
```json
{"id","number","name": "Cycle 3" ,"custom_name": string|null,"starts_on":"2026-11-02","ends_on":"2026-11-15",
 "state":"upcoming|current|past",
 "progress": {"total": 12, "started": 3, "completed": 5, "canceled": 1}}
```
`name` is the display name (`custom_name` or `Cycle {number}`). `progress` counts the cycle's issues by status category (`backlog`/`unstarted` are `total - started - completed - canceled`).

Cycle ref (inside issues): `{"id","number","name","starts_on","ends_on","state"}`.

Issue JSON (list and single) gains `"cycle": cycleRef|null`.

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/projects/{key}/cycles` | member | — | `200 [cycle…]` ordered by `starts_on` desc | 404 project |
| `POST /api/workspaces/{slug}/projects/{key}/cycles` | `cycles.manage` | `name` (optional), `starts_on`, `ends_on` (`YYYY-MM-DD`) | `201 cycle` | 400 `{"errors":{field: msg}}`; 403; 404; 409 archived `{"error":"This project is archived."}` (reuse existing message) |
| `GET /api/workspaces/{slug}/projects/{key}/cycles/{number}` | member | — | `200 {"cycle": cycle, "issues": [issue…]}` (list columns, same as project issues) | 404 `{"error":"Cycle not found."}` |
| `POST /api/workspaces/{slug}/projects/{key}/cycles/{number}` | `cycles.manage` | `name` (empty clears to default), `starts_on`, `ends_on`, each optional | `200 cycle` | 400; 403; 404; 409 archived |
| `DELETE /api/workspaces/{slug}/projects/{key}/cycles/{number}` | `cycles.manage` | — | `204` (its issues get `cycle = null`) | 403; 404 |
| `POST /api/workspaces/{slug}/projects/{key}/cycles/{number}/rollover` | `cycles.manage` | `to` (target cycle number, same project, not past) | `200 {"moved": n, "cycle": targetCycle}`; moves the cycle's issues whose status category isn't `completed`/`canceled` | 400 `{"errors":{"to": msg}}`; 403; 404 |
| `POST /api/workspaces/{slug}/projects/{key}/issues` (changed) | as before | + `cycle` (cycle id, optional) | as before | + 400 `{"errors":{"cycle": msg}}` |
| `POST /api/workspaces/{slug}/issues/{identifier}` (changed) | edit rule | + `cycle` (cycle id; empty clears) | as before | + 400 `cycle` |

Messages:
- `starts_on` / `ends_on`: `Choose a start date.` · `Choose an end date.` · `The end date can't be before the start date.` · `A cycle can be at most 90 days.` · `This overlaps Cycle 3 (Nov 2 – Nov 15).` (on `starts_on`; dates as `Jan 2`, add the year when it isn't the current one)
- `name`: `Use 60 characters or fewer.`
- `cycle`: `Cycle not found.` (unknown id or other project)
- `to`: `Choose a cycle.` · `Cycle not found.` · `Choose an upcoming or current cycle.` · `Choose a different cycle.`

Activity: setting or clearing an issue's cycle logs an event `cycle` with `{"from": {"number","name"}|null, "to": {...}|null}`; rollover logs the same event per moved issue (actor = the user who rolled over). No notifications for cycle changes.

Realtime: cycle writes publish `realtime.Project(slug,key)`, `realtime.ProjectIssues(slug,key)` and a new `…/projects/{key}/cycles` path plus `…/cycles/{number}` (add helpers next to the existing ones); issue writes that change `cycle` also publish both cycle paths (old and new).

## Backend (`backend/user/internal/cycle/`)

- New package `cycle` with `routes.go`, `controller.go`, `model.go`, `validate.go`; wired in `cmd/user/main.go` like `project`.
- Issue changes stay in `internal/issue`: `cycle` in `fields`, the select columns (`cycleRef` JSON like `refJSON`), the edit-rule path, the diff/event, and validation that the cycle is in the issue's project.
- Progress via one grouped query over `issues join statuses` per list call (no N+1).
- Verify: `go vet`, `go test` (a model test for overlap, rollover moving only open issues, delete nulling `cycle_id`), curl every row and message.

## Frontend (user app)

`api.ts`: `Cycle`, `CycleRef`; `Issue.cycle`; `listCycles`, `getCycle`, `createCycle`, `updateCycle`, `deleteCycle`, `rolloverCycle`; event kind `cycle` in `event-text.tsx` ("moved to Cycle 3", "removed from Cycle 2").

Routes (not nested under the project page's layout):
- `/{workspace}/projects/{key}/cycles` — the project's cycles.
- `/{workspace}/projects/{key}/cycles/{number}` — one cycle.

| Where | Behaviour |
|---|---|
| Project page header | A "Cycles" link next to the existing actions, plus a chip for the current cycle ("Cycle 3 · ends Nov 15") linking to it, when there is one. |
| Cycles page | Sections "Current", "Upcoming", "Past" (empty sections hidden). Each row: name, dates (`Nov 2 – Nov 15`), progress bar (completed share, green `#00BA7C`) and `5 of 12 done`. "New cycle" button (`cycles.manage`, project not archived) opens a dialog: name (optional, placeholder `Cycle 4`), start date, end date (native `<input type="date">`); defaults start = day after the latest cycle's end (or today), end = start + 13 days. Empty: "No cycles yet." |
| Cycle page | Header: name, dates, state pill, progress bar and counts; "Edit" and "Delete" in a menu (`cycles.manage`). Body: the shared `IssueView` (list/board, filters in the URL via `validateViewSearch`) over the cycle's issues. "New issue" presets this cycle. A past cycle with open issues shows a banner "4 issues aren't done." with "Move to…" (picker of upcoming/current cycles) → rollover. Delete confirms "Delete Cycle 3? Its issues stay in the project." |
| Issue page properties | "Cycle" picker (this project's upcoming and current cycles, plus the issue's current one even if past), clearable ("No cycle"); read-only when `can_edit` is false. |
| New issue dialog | Accepts an optional preset cycle (shown as a read-only line "In Cycle 3"), like the preset parent. |
| Project issue list / board | Filter key `cycle` (values: cycle id, or `none`) added to the existing filter menu and `ViewSearch`; a small muted cycle chip is not added to rows. |
| Command palette | "Go to current cycle" for the project in context, when there is one. |

## Verification

- Backend: above.
- Frontend: `npm run build`; extend `view-search.check.ts` for the `cycle` filter.
- Integration (main session, headless Chrome): create Cycle 1 (this week) and Cycle 2 (next); overlap error shows; add issues to Cycle 1 from the issue page and from the cycle page's New issue; progress updates live when an issue moves to Done in another tab; board view on the cycle page; edit Cycle 1's dates into the past → banner → move open issues to Cycle 2 (done ones stay); filter project list by cycle; delete Cycle 2 → its issues show "No cycle"; a member without `cycles.manage` sees no create/edit controls but can set an issue's cycle on their own issue.
