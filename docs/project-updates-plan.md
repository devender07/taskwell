# Project updates plan

Scope: a short written update on a project with a health (on track, at risk or off track). The latest health shows on the project page, the projects list, the roadmap and Home.

Out of scope:
- Scheduled reminders to post an update.
- Reactions or comments on updates.
- Notifications. `ponytail:` the update appears live and in the activity feed; add a notification when people miss updates.

## Schema (`014_project_updates.sql`)

```sql
create table project_updates (
  id uuid primary key default uuidv7(),   -- match 001's id default
  project_id uuid not null references projects(id) on delete cascade,
  author_id uuid references users(id) on delete set null,
  health text not null check (health in ('on_track', 'at_risk', 'off_track')),
  body jsonb not null,                    -- rich-text doc, checked with doc.Check (max 10,000 chars)
  created_at timestamptz not null default now(),
  edited_at timestamptz
);
create index project_updates_project on project_updates (project_id, created_at desc);
```

## API contract

An update is returned as:

```json
{"id", "health", "body", "author": {"id", "name"} | null, "created_at", "edited_at", "can_edit"}
```

`can_edit` is true for the author, or for anyone with `projects.manage`. Every endpoint needs the caller to be able to see the project.

| Method & path | Needs | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/projects/{key}/updates?before=<rfc3339>` | see project | `200 {"updates": [...], "has_more": bool}`, 20 per page, newest first | 401, 404 |
| `POST /api/workspaces/{slug}/projects/{key}/updates` | `projects.manage`, or being a project member for a private project | `201 update` | 400 fields `health` ("Choose a health."), `body` ("Write an update." or the doc length message); 403; 404; 409 "This project is archived." |
| `POST /api/workspaces/{slug}/project-updates/{id}` | can_edit | `200 update` (`health` and `body` are optional, `edited_at` set) | 400, 403, 404 |
| `DELETE /api/workspaces/{slug}/project-updates/{id}` | can_edit | `204` | 403, 404 |

Existing project JSON (single project, project list, roadmap rows) gains `"health": "on_track" | "at_risk" | "off_track" | null`, taken from the latest update.

Each change publishes realtime on the project path, its `/updates` path, the projects list, the roadmap and activity. The workspace activity feed gains a `project_update` item:

- "Name posted an update on Project · At risk"

Add it wherever the activity feed is assembled, if that's simple. Otherwise skip it and note it.

## Backend

The `project` package gets `updates.go` and model functions, plus tests for:
- permissions
- the health on project JSON after create and delete
- paging

## Frontend

- **Project page:** a new "Updates" section above Issues.
  - An "Post update" button opens a composer: a health segmented control (On track, green; At risk, amber `#F5A524`, or the existing warning token if `tokens.css` has one; Off track, pink) and the shared rich editor.
  - Each update shows the author avatar, name, relative time, a health pill and the body, with Edit and Delete for `can_edit`.
  - The first 3 updates show, then "Show older".
- A health pill shows next to the project name in the header, on the projects list rows and on roadmap bars. It shows nothing when `null`.
- **Home**, project progress: show the health pill next to each project.

## Verification

- `go test` and curl.
- `npm run build`.
- Integration: post, edit and delete an update. Check that the health shows on the project, the projects list and the roadmap, and appears live in a second tab.
