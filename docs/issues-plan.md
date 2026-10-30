# Issues plan

Scope (Phase 2, part 1): issues inside projects with title, description, status, assignee, priority, due date and labels; readable IDs like `ENG-142` used in URLs; workspace statuses (seeded Backlog, Todo, In progress, Done, Cancelled) and labels, both editable in settings. Turns on `issues.create` and `issues.edit_any`, and adds `workflow.manage`.

Out of scope: sub-issues and blocks/blocked-by links (Phase 2 part 2, own plan), list grouping/sorting, kanban, filters, "My issues" (Phase 3), comments and activity (Phase 4), markdown rendering (descriptions are stored as markdown and **shown as plain text with line breaks**; decided, no dependency).

Update (shipped follow-up): old project keys are kept as aliases (`project_key_aliases`, migration 011; `canonical_key()` in SQL), so old project and issue URLs redirect to the current key. A key taken by another project stops being an alias.

## Schema

Migration `backend/user/internal/db/migrations/009_issues.sql`:

```sql
create table statuses (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null,
  category     text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  position     int not null,
  created_at   timestamptz not null default now()
);
create unique index on statuses (workspace_id, lower(name));

-- Every workspace gets the five defaults: a trigger on insert into workspaces (covers both create paths)
-- plus a backfill for existing workspaces. Backlog/backlog 0, Todo/unstarted 1, In progress/started 2,
-- Done/completed 3, Cancelled/canceled 4.

create table labels (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null,
  color        text not null check (color ~ '^#[0-9a-f]{6}$'),
  created_at   timestamptz not null default now()
);
create unique index on labels (workspace_id, lower(name));

alter table projects add column issue_seq int not null default 0; -- last issue number used

create table issues (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  project_id   uuid not null references projects on delete cascade,
  number       int not null,
  title        text not null,
  description  text not null default '',
  status_id    uuid not null references statuses,
  assignee_id  uuid references users on delete set null,
  priority     smallint not null default 0 check (priority between 0 and 4), -- 0 none, 1 urgent, 2 high, 3 medium, 4 low
  due_date     date,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now(),
  unique (project_id, number)
);
create index on issues (workspace_id, assignee_id);

create table issue_labels (
  issue_id uuid not null references issues on delete cascade,
  label_id uuid not null references labels on delete cascade,
  primary key (issue_id, label_id)
);
```

Numbers: on create, `update projects set issue_seq = issue_seq + 1 … returning issue_seq` in the same transaction (row lock serializes concurrent creates). Numbers are never reused.

Removing a member (existing `removeMember`, including leave) also unassigns them from that workspace's issues, in the same transaction.

## Permissions

`internal/workspace/permissions.go`: `issues.create` and `issues.edit_any` become active; add `workflow.manage` — group "Issues", label "Manage statuses and labels", active. Member default permissions stay `issues.create`, `comments.create` (don't change existing Member roles).

Rules:
- View issues, statuses, labels: any member.
- Create an issue: `issues.create`, and the project isn't archived.
- Edit an issue (any field): `issues.edit_any`, or you created it, or it's assigned to you.
- Delete an issue: `issues.edit_any`, or you created it.
- Statuses and labels (create/edit/reorder/delete): `workflow.manage`.

Issue JSON includes `can_edit` and `can_delete` computed for the caller.

## Validation (same messages both sides; add to `@tether/shared/validate` + check, mirror in Go)

| Field | Rule | Message |
|---|---|---|
| `title` | trimmed, required; ≤ 200 | `Title is required.` / `Title must be 200 characters or fewer.` |
| `description` | ≤ 50000 | `Description must be 50,000 characters or fewer.` |
| `priority` | integer 0–4 | `Choose a priority.` |
| `due_date` | `YYYY-MM-DD`, a real date, or empty | `Enter a valid date.` |
| `assignee_id` | empty or a current member of the workspace (server only) | `Choose someone in this workspace.` |
| `status_id` | a status of this workspace (server only) | `Choose a status.` |
| `label_ids` | labels of this workspace (server only) | `Choose labels from this workspace.` |
| status `name` | trimmed, required; ≤ 30 | `Status name is required.` / `Status name must be 30 characters or fewer.` |
| status `category` | one of the five | `Choose a category.` |
| label `name` | trimmed, required; ≤ 30 | `Label name is required.` / `Label name must be 30 characters or fewer.` |
| label `color` | `^#[0-9a-f]{6}$` after lowercasing | `Choose a color.` |

## API contract

FormData in, JSON out. Signed out 401; workspace missing / not a member `404 {"error":"Workspace not found."}`; no permission `403 {"error":"You don't have permission to do that."}`. Name clashes → 409 `{"errors":{"name":"A status with that name already exists."}}` / `"A label with that name already exists."`.

Shapes:
- Status: `{"id","name","category","position"}`
- Label: `{"id","name","color"}`
- Person ref: `{"id","name"}`
- Issue: `{"id","identifier":"ENG-142","number":142,"project":{"key":"ENG","name":"Engineering"},"title","description","status":status,"assignee":person|null,"priority":0-4,"due_date":"2026-11-02"|null,"labels":[label…] (by name),"created_by":person|null,"created_at","updated_at","can_edit":bool,"can_delete":bool}` (timestamps UTC)

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/statuses` | member | — | `200 [status…]` by position | 404 |
| `POST /api/workspaces/{slug}/statuses` | `workflow.manage` | `name`, `category` | `201 status` (appended last) | 400, 403, 409 |
| `POST /api/workspaces/{slug}/statuses/{id}` | `workflow.manage` | `name`, `category` | `200 status` | 400, 403, 404 `Status not found.`, 409 |
| `POST /api/workspaces/{slug}/statuses/order` | `workflow.manage` | `ids` (every status id, once each, in the new order) | `200 [status…]` | 400 `{"error":"Send every status once."}`, 403 |
| `DELETE /api/workspaces/{slug}/statuses/{id}` | `workflow.manage` | query `move_to` (status id) — required when issues use it | `204`; its issues move to `move_to` | 403; 404; 409 `{"error":"Move its issues to another status first."}` (in use, no/invalid `move_to`); 409 `{"error":"A workspace needs at least one status."}` |
| `GET /api/workspaces/{slug}/labels` | member | — | `200 [label…]` by name | 404 |
| `POST /api/workspaces/{slug}/labels` | `workflow.manage` | `name`, `color` | `201 label` | 400, 403, 409 |
| `POST /api/workspaces/{slug}/labels/{id}` | `workflow.manage` | `name`, `color` | `200 label` | 400, 403, 404 `Label not found.`, 409 |
| `DELETE /api/workspaces/{slug}/labels/{id}` | `workflow.manage` | — | `204` (removed from issues) | 403, 404 |
| `GET /api/workspaces/{slug}/projects/{key}/issues` | member | — | `200 [issue…]` newest first | 404 project / workspace |
| `POST /api/workspaces/{slug}/projects/{key}/issues` | `issues.create` | `title`, `description`?, `status_id`? (default: first status by position), `assignee_id`?, `priority`? (default 0), `due_date`?, `label_ids` (repeat per id) | `201 issue` | 400 field errors; 403; 404; 409 `{"error":"This project is archived."}` |
| `GET /api/workspaces/{slug}/issues/{identifier}` | member | — | `200 issue` (identifier case-insensitive, e.g. `eng-142`) | 404 `{"error":"Issue not found."}` |
| `POST /api/workspaces/{slug}/issues/{identifier}` | edit rule | any of the create fields; **only fields present are changed**. `assignee_id=` / `due_date=` (empty) clear them; `label_ids` sent once empty (`label_ids=`) clears labels | `200 issue` | 400; 403; 404 |
| `DELETE /api/workspaces/{slug}/issues/{identifier}` | delete rule | — | `204` | 403; 404 |

Every successful issue update sets `updated_at`.

## Backend (`backend/user`)

- New package `internal/issue/` (routes/controller/model/validate + `issue_test.go`) for issues, statuses and labels; it uses `workspace.Member`/`workspace.Has` like `internal/project`. Project lookup by key: query `projects` directly in this package's model (no cross-package call needed).
- Identifier parsing: `^([A-Za-z][A-Za-z0-9]{1,4})-([1-9][0-9]*)$`; anything else → 404 Issue not found.
- Verify: `go vet`, `go test`, curl every row: defaults seeded for new and existing workspaces; numbering 1,2,3 per project and concurrent creates (fire ~10 in parallel; no duplicate numbers); partial update clears; edit rule (creator, assignee, `edit_any`, other member → 403); delete rule; archived project 409; assignee must be member; removing a member unassigns; status delete with/without `move_to`; last-status 409; label delete strips from issues; order endpoint.

## Frontend (user app)

Validation via shared validators. `api.ts`: `Issue`, `Status`, `Label`, `PersonRef` types. Shared: priorities as a const list `[{value: 0, label: 'No priority'}, {1,'Urgent'}, {2,'High'}, {3,'Medium'}, {4,'Low'}]` in `user/src/components/issues/` (only the user app needs it).

Pickers use `SmartAutocomplete` (status, priority, assignee from `GET …/members`), native `<input type="date">` for due date (wrapped with `Input type="date"`), and a labels picker = shared `Popover` with one shared `Checkbox` per label (name + color dot). Status shows a small category icon or dot (gray backlog/unstarted, `#F7F8F8` outline started, green `#00BA7C` completed, secondary canceled; no new tokens).

| Route | File | Behaviour |
|---|---|---|
| `/$workspace/projects/$key` (changed) | existing `projects/$key.tsx` | Replace the "Issues will show up here." placeholder with the project's issues (loader also fetches `…/issues`). Row: identifier (mono, secondary), title, label dots, status, priority, assignee avatar, due date (accent pink if past and not completed/canceled). Row links to the issue. Empty: "No issues yet." "New issue" button (needs `issues.create` and not archived) opens a `Dialog` with the create form (title, description `Textarea`, status, priority, assignee, due date, labels); on success go to the new issue. |
| `/$workspace/issue/$identifier` | `routes/_app/$workspace/_main/issue/$identifier.tsx` | Loader: issue + statuses + labels + members in parallel; 404 → `notFound()`. Two columns (≥ 1024px; stacked below): left = breadcrumb `Project name › ENG-142`, title, description (pre-wrap; "No description." muted), "Edit" (title + description form inline, when `can_edit`); right = properties: status, priority, assignee, due date, labels, each saving immediately on change via the partial update (when `can_edit`, otherwise read-only text); created by + created/updated dates; "Delete issue" (when `can_delete`, `Dialog` confirm "Delete ENG-142? This can't be undone.", then back to the project). |
| `/$workspace/settings/workflow` | `routes/_app/$workspace/_main/settings/workflow.tsx` | Sidebar Settings child "Workflow". Statuses list in position order: name, category; with `workflow.manage`: add (name + category select), rename/recategorize inline, move up/down buttons (calls order endpoint), delete (`Dialog`; if issues may use it, pick a status to move them to — always send `move_to` from the dialog's picker, default the first other status). Read-only otherwise. |
| `/$workspace/settings/labels` | `routes/_app/$workspace/_main/settings/labels.tsx` | Sidebar Settings child "Labels". List with color dot; with `workflow.manage`: add (name + native `<input type="color">`), edit inline, delete (`Dialog`). |

Category select options: Backlog, Unstarted, Started, Completed, Canceled (sentence case labels; values as in the schema).

## Verification

- Backend: above.
- Frontend: `npm run build`, validator check.
- Integration (main session, headless Chrome): create issues ENG-1/ENG-2 from the project page, open by URL incl. lowercase, change status/priority/assignee/due date/labels inline and see them persist, edit title/description, delete; Member can create and edit own, can't edit others' (read-only properties); Workflow page add/reorder/rename/delete-with-move; Labels add/edit/delete; archived project hides "New issue".
