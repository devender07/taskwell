# Issue templates and recurring issues plan

Scope: workspace issue templates that prefill a new issue (title, description, status, priority, labels, assignee, project). A template can optionally **repeat**: every N days, weeks or months it creates an issue from itself in its project. This one model covers both "Issue templates" and "Recurring issues".

Out of scope:
- Variables in titles like `{date}`. The exception is the repeat title suffix described below.
- Per-project template lists. A template's optional project only preselects one.
- Skipping a single occurrence. Pause the template instead.

## Schema (`015_issue_templates.sql`)

```sql
create table issue_templates (
  id uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces(id) on delete cascade,
  name text not null,                         -- 1–60 chars
  title text not null default '',             -- may be empty for a plain template
  description jsonb,                          -- rich-text doc or null
  project_id uuid references projects(id) on delete cascade,
  status_id uuid references statuses(id) on delete set null,
  assignee_id uuid references users(id) on delete set null,
  priority smallint not null default 0,
  label_ids uuid[] not null default '{}',
  repeat_every int,                           -- null = no repeat; 1–365
  repeat_unit text check (repeat_unit in ('day', 'week', 'month')),
  next_on date,                               -- next date an issue is created (UTC)
  paused boolean not null default false,
  created_by uuid references users(id) on delete set null,
  created_at timestamptz not null default now(),
  check ((repeat_every is null) = (repeat_unit is null) and (repeat_every is null) = (next_on is null))
);
create index issue_templates_due on issue_templates (next_on) where repeat_every is not null and not paused;
```

## API contract

A template is returned as:

```json
{"id", "name", "title", "description",
 "project": {"id", "key", "name"} | null,
 "status": status | null,
 "assignee": {"id", "name"} | null,
 "priority",
 "labels": [label…],
 "repeat": {"every": 2, "unit": "week", "next_on": "2026-11-10", "paused": false} | null,
 "can_edit"}
```

`can_edit` comes from `workflow.manage`. Templates whose project the caller can't see are left out.

| Method & path | Needs | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/templates` | member | `200 [template…]` by name | 401, 404 |
| `POST /api/workspaces/{slug}/templates` | `workflow.manage` | `201 template` | 400 fields, 403, 404 |
| `POST /api/workspaces/{slug}/templates/{id}` | `workflow.manage` | `200 template` (fields present change) | 400, 403, 404 |
| `DELETE /api/workspaces/{slug}/templates/{id}` | `workflow.manage` | `204` | 403, 404 |

The FormData fields are:
- `name`, `title`, `description` (a doc as JSON), `project` (key, empty = none), `status_id`, `assignee_id`, `priority`, `label_ids` (repeated).
- For repeating: `repeat_every`, `repeat_unit` and `starts_on` (a date, today or later, which sets `next_on`). All three empty removes the repeat.
- `paused` (`1`/`0`).

Validation messages:
- "Enter a name."
- "Use 60 characters or fewer."
- Title: the existing title rules, but empty is allowed unless the template repeats ("A repeating template needs a title.").
- "A repeating template needs a project."
- "Repeat every 1 to 365."
- "Choose day, week or month."
- "Pick today or a later date."
- The assignee must be able to see the project ("Choose someone who can see this project.").

## Recurring runner

New package `backend/user/internal/template/` holds the routes, controller and model, plus `run.go`.
- `Run(ctx, every)` follows `sweep.Run`'s pattern, ticking every 5 minutes behind an advisory lock.
- It finds templates that repeat, aren't paused, and have `next_on <= today (UTC)`.
- For each, it creates one issue through a new exported `issue.CreateFromTemplate(ctx, tx, …)` in a new file `issue/template.go`. That function reuses the create internals: numbering, rank at the end, labels, the `created` event, notifying the assignee, and realtime.
  - The creator is `created_by`, or the first workspace admin when that is null.
  - The title gets " · Nov 10" (the occurrence date) appended.
- It then advances `next_on` by the interval until it lands in the future, so missed runs make one issue, not a burst.
- An archived project pauses the template instead of creating anything.

Wire it in `cmd/user/main.go` next to the sweeper. Add tests for:
- the advance logic, including month-end (Jan 31 + 1 month = Feb 28)
- a due template creating exactly one issue
- archived → paused

## Frontend

- **Settings → Templates** (icon `FileIcon` or similar, added to icons.tsx if missing). Every member can see the page; `workflow.manage` is needed to edit.
  - The list shows the name, the project, and the repeat summary ("Every 2 weeks · next Nov 10", "Paused").
  - The form, in a page or a wide dialog, has:
    - Name, Title, Description (the rich editor)
    - Project, Status, Assignee, Priority, Labels (the same pickers as the issue form)
    - a "Repeat" checkbox revealing Every [n] [day/week/month], Starting [date] and Paused
- **New issue form** (quick-create `C` and project "New issue"): when templates exist, a "Template" `SmartAutocomplete` sits at the top. Picking one fills the empty fields; fields the user already typed in are kept. On the project page, templates for that project are listed first.

## Verification

- `go test`; curl CRUD.
- Trigger the runner (call `Run` once in a test, or set a template's `next_on` to today and wait for a tick with a short interval in dev) and check that the issue appears.
- `npm run build`.
- Integration: create a template, use it in quick-create, create a repeating one due today, and see the issue appear.
