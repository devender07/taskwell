# Roadmap plan

Scope: a workspace **Roadmap** page that lays the active projects the viewer can see on a timeline. Each project is a
bar from its start date to its target date, filled by issue progress, with its cycles under the bar and its milestones
as markers. Projects get optional start and target dates; milestones are new (name + date, per project).

Out of scope: dragging bars to change dates, dependencies between projects, workspace-level milestones, roadmap
filters beyond the zoom, project health/status updates, exporting.

## Schema

Migration `backend/user/internal/db/migrations/002_roadmap.sql`:

```sql
alter table projects
  add column start_date  date,
  add column target_date date,
  add constraint projects_dates check (start_date is null or target_date is null or start_date <= target_date);

create table project_milestones (
  id         uuid primary key default uuidv7(),
  project_id uuid not null references projects on delete cascade,
  name       text not null check (char_length(name) between 1 and 60),
  date       date not null,
  created_by uuid references users on delete set null,
  created_at timestamptz not null default now()
);
create index on project_milestones (project_id, date);
```

No new permission: dates and milestones are written with the existing `projects.manage`; reading follows project
visibility (`can_see_project`). An archived project's milestones are read-only (409
`{"error":"This project is archived."}`), and archived projects are left off the roadmap.

## API contract

FormData in, JSON out. Dates are `YYYY-MM-DD`. Writes need `projects.manage` (403
`{"error":"You don't have permission to do that."}` as today). Unknown or invisible project: 404
`{"error":"Project not found."}` as today.

**Project JSON** (everywhere a project is returned) gains `"start_date": "YYYY-MM-DD" | null` and
`"target_date": "YYYY-MM-DD" | null`.

**Create / update project** (`POST /api/workspaces/{slug}/projects`, `POST …/projects/{key}`) take optional
`start_date` and `target_date`. On update a date changes only when its field is sent (empty clears it), like
`private`, so existing callers (MCP) that send only name/key/description keep the dates. Errors, 400 `{"errors":{…}}`:
- `start_date`: `"Enter a valid date."`
- `target_date`: `"Enter a valid date."` or `"Target date must be on or after the start date."` (checked against the
  stored start date when only one is sent)

**Milestones** — Milestone JSON: `{"id","name","date"}`.

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/projects/{key}/milestones` | — | `200 [milestone…]` by date, then name | 404 |
| `POST /api/workspaces/{slug}/projects/{key}/milestones` | `name`, `date` | `201 milestone` | 400 `{"errors":{"name":"Milestone name is required." / "Milestone name must be 60 characters or fewer.","date":"Enter a valid date."}}`; 403; 404; 409 archived |
| `POST /api/workspaces/{slug}/projects/{key}/milestones/{id}` | `name`, `date` | `200 milestone` | as above; 404 `{"error":"Milestone not found."}` |
| `DELETE /api/workspaces/{slug}/projects/{key}/milestones/{id}` | — | `204` | 403; 404 `{"error":"Milestone not found."}`; 409 archived |

**Roadmap** — `GET /api/workspaces/{slug}/roadmap` → `200`:

```json
[{
  "key": "ENG", "name": "Engineering", "private": false,
  "start_date": "2026-10-01" | null, "target_date": "2026-12-15" | null,
  "progress": {"total": 40, "completed": 12, "canceled": 2},
  "milestones": [{"id": "…", "name": "Beta", "date": "2026-11-20"}],
  "cycles": [{"number": 3, "name": "Cycle 3", "starts_on": "2026-10-28", "ends_on": "2026-11-10", "state": "current"}]
}]
```

Active (not archived) projects the viewer can see, ordered by `coalesce(start_date, target_date)` nulls last, then
name. `progress` counts the project's issues by status category; `cycles` are all its cycles by `starts_on`; cycle
`state` is computed as in the cycle package. One query per list (projects, then milestones and cycles for those ids),
no N+1. 404 when the caller isn't a member of the workspace.

**Realtime.** New path helpers `realtime.Roadmap(slug)` (`/workspaces/{slug}/roadmap`) and
`realtime.Milestones(slug, key)` (`…/projects/{key}/milestones`). Publish `…/roadmap` with: project create, update,
archive/unarchive and membership changes (`project.changed`); milestone writes (kind `milestone.changed`, also
`…/milestones` and `…/projects/{key}`); cycle create/update/delete; and issue create, delete and status change (the
event those already publish gets the extra path). Private-project audiences stay as they are today.

## Frontend (user app)

- `src/api.ts`: `Project` gains `start_date`/`target_date`; types `Milestone` and `RoadmapProject`; path helpers.
- **Project form** (`components/projects/project-form.tsx`): "Start date" and "Target date" fields side by side,
  optional, using `@taskwell/shared/date-picker` (as the issue due date does). Client check: target on or after start
  (`"Target date must be on or after the start date."`).
- **Milestones** on the project's settings/edit page (where `ProjectForm` is used for an existing project): a
  "Milestones" section listing name + date with Edit and Delete, and an "Add milestone" inline form (name, date).
  Read-only list without `projects.manage`. Delete asks to confirm.
- **Roadmap page** `routes/_app/$workspace/_main/roadmap.tsx` (`/$workspace/roadmap`), parts in
  `components/roadmap/`:
  - `validateSearch`: `zoom: 'month' | 'quarter' | 'year'` (default `quarter`): the visible span and column width
    (month: days, quarter: weeks, year: months as columns).
  - Header: title "Roadmap", zoom tabs (`@taskwell/shared/tabs`), a "Today" button that scrolls today into view.
  - Grid: a sticky left column (240px) with project name, key and progress percent; a horizontally scrollable timeline
    with month labels, light gridlines and a today line (accent pink). The timeline spans from the earliest to the
    latest date shown, padded a month each side, never less than the zoom's span around today.
  - Row per project: a bar (8px radius) from start to target, filled by `completed / (total − canceled)`; with only
    one date, a dot at that date; with none, a muted "No dates" note and, with `projects.manage`, a "Set dates" link
    to the project's edit page. Cycles as thin segments under the bar (the current one brighter) with name + dates on
    hover (`title`). Milestones as small diamonds with the name on hover and as an accessible label. The bar and the
    name link to the project.
  - Empty state when there are no active projects ("No projects yet" + link to Projects).
  - Live: loader via `loaderApi`, so realtime refetches on `…/roadmap`.
  - Keyboard and screen readers: rows are a list; each bar is a link labelled "{name}, {start} to {target}, {n}% done".
- **Sidebar**: "Roadmap" link after Projects with a new `RoadmapIcon` in `@taskwell/shared/icons`. Command palette:
  "Go to Roadmap".

## Agents

- Backend: migration, `project` package (dates on create/update/JSON, milestones endpoints, roadmap endpoint — or a
  small `roadmap` package if cleaner), realtime paths, tests: dates validation and partial update, milestones CRUD
  with permission/visibility/archived, roadmap ordering, progress and private-project visibility. `go vet`,
  `go test ./user/...`, curl against the running API.
- Frontend: as above. `npm run build`, `npm run check`.
