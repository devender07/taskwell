# Workspace defaults

Staff decide what a new workspace starts with: its roles, its workflow (status types and statuses), its labels and
its issue templates. They edit these on a new **Defaults** page in the admin app. A workspace copies the defaults
once, when it's created; changing them later never touches existing workspaces.

Out of scope: pushing defaults to existing workspaces, per-plan defaults, default projects, cycles, saved views,
templates with a project, assignee or repeat schedule (a new workspace has no projects or other members).

## Schema

Migration `046_workspace_defaults.sql`. Global tables (no workspace), mirroring the workspace ones and their checks:

```sql
create table default_roles (
  id          uuid primary key default uuidv7(),
  name        text not null check (char_length(name) between 1 and 40),
  builtin     text unique check (builtin in ('member', 'guest')),   -- null: a custom role
  permissions text[] not null default '{}',
  created_at  timestamptz not null default now()
);
create unique index default_roles_name_idx on default_roles (lower(name));

create table default_status_types (
  id         uuid primary key default uuidv7(),
  name       text not null check (char_length(name) between 1 and 30),
  category   text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  icon       text not null,
  color      text check (color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'),
  position   integer not null default 0,
  created_at timestamptz not null default now()
);

create table default_statuses (
  id       uuid primary key default uuidv7(),
  name     text not null,
  category text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  type_id  uuid references default_status_types on delete set null,
  position integer not null
);

create table default_labels (
  id    uuid primary key default uuidv7(),
  name  text not null,
  color text not null check (color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$')
);
create unique index default_labels_name_idx on default_labels (lower(name));

create table default_templates (
  id          uuid primary key default uuidv7(),
  name        text not null,
  title       text not null default '',
  description jsonb,
  status_id   uuid references default_statuses on delete set null,
  priority    smallint not null default 0 check (priority between 0 and 4),
  label_ids   uuid[] not null default '{}',   -- deleted labels drop out when copied
  created_at  timestamptz not null default now()
);
```

Name lengths and other limits match the workspace tables' validators (`issue/validate.go`, `template/form.go`).

Seed so nothing changes on deploy: roles Member and Guest (`builtin` member / guest) with
`{issues.create, comments.create}` (today's `memberDefaults`), and statuses Backlog, Todo, In progress, Done and
Cancelled (positions 0–4, no type). No labels, types or templates.

### Copying at creation

- **Workflow, labels, templates:** `seed_statuses()` (the `after insert on workspaces` trigger function) is replaced
  so it copies `default_status_types` → `status_types`, `default_statuses` → `statuses` (types mapped to the new
  ids), `default_labels` → `labels`, and `default_templates` → `issue_templates` (status and labels mapped; labels
  that no longer exist dropped; `project_id`, `assignee_id`, repeat fields and `created_by` null). Every workspace
  insert gets them, test fixtures included. If `default_statuses` is empty, it falls back to today's five statuses
  (a workspace needs at least one).
- **Roles:** `workspace.create` (Go) still inserts Admin (`builtin 'admin'`, all permissions) itself, then inserts
  one role per `default_roles` row (keeping `builtin` for member and guest) instead of the hard-coded Member and
  Guest. If the member or guest row is missing, it inserts that one with today's defaults, so a workspace always
  has all three built-ins.

### Shared code

The admin service can't import the user API's `internal` packages. Move what both need into `backend/shared`:
- `shared/perms`: the permission catalog (key, group, label, active), the "guest can't hold" rule (Workspace,
  Members and Analytics groups). `user/internal/workspace` uses it instead of its own copy; behavior unchanged.
- The rich-text check (`doc.Check` and what it needs) if it's self-contained, so default template descriptions are
  validated the same way. Otherwise the admin API checks the description is a JSON object of at most 50000
  characters and the user API's check runs on use (copying stores it as is).

## Admin API contract

All routes need a signed-in admin (the admin service's usual wrapper). Bodies are FormData. Errors:
`400 {"errors": {"field": "Message."}}`, `404 {"error": "…"}`, `409 {"error": "…"}`. Every change writes an admin
audit entry (`defaults.update`, target the kind, e.g. `roles`) and publishes the admin realtime event the Config
page uses so other open admin tabs refetch `/api/defaults`.

`GET /api/defaults` → `200`

```json
{
  "permissions": [{"key": "issues.create", "group": "Issues", "label": "Create issues"}],
  "roles":        [{"id": "…", "name": "Member", "builtin": "member", "permissions": ["issues.create"]}],
  "status_types": [{"id": "…", "name": "In review", "category": "started", "icon": "eye", "color": "#5e6ad2", "position": 0}],
  "statuses":     [{"id": "…", "name": "Todo", "category": "unstarted", "type_id": null, "position": 1}],
  "labels":       [{"id": "…", "name": "Bug", "color": "#eb5757"}],
  "templates":    [{"id": "…", "name": "Bug report", "title": "", "description": null, "status_id": null, "priority": 0, "label_ids": []}]
}
```

Lists are ordered: roles Member, Guest, then custom by name; types and statuses by position; labels and
templates by name. `permissions` is the catalog's active entries.

| Method and path | Fields | Answers |
|---|---|---|
| `POST /api/defaults/roles` | `name`, `permission` (repeated) | `201` role |
| `POST /api/defaults/roles/{id}` | `name` (custom roles only), `permission` (repeated) | `200` role |
| `DELETE /api/defaults/roles/{id}` | | `204`; `409` "Built-in roles can't be deleted." |
| `POST /api/defaults/status-types` | `name`, `category`, `icon`, `color` (optional) | `201` type |
| `POST /api/defaults/status-types/{id}` | same | `200` type; statuses of the type take its category |
| `DELETE /api/defaults/status-types/{id}` | | `204`; its statuses keep their category, no type |
| `POST /api/defaults/statuses` | `name`, `category`, `type_id` (optional; category is then the type's) | `201` status, last position |
| `POST /api/defaults/statuses/{id}` | same | `200` status |
| `DELETE /api/defaults/statuses/{id}` | | `204`; `409` "Keep at least one status." for the last one |
| `POST /api/defaults/statuses/order` | `id` (repeated, every status, new order) | `200` statuses; `400` when the ids aren't exactly the statuses |
| `POST /api/defaults/labels` | `name`, `color` | `201` label |
| `POST /api/defaults/labels/{id}` | same | `200` label |
| `DELETE /api/defaults/labels/{id}` | | `204` (removed from templates' `label_ids`) |
| `POST /api/defaults/templates` | `name`, `title`, `description` (JSON doc, optional), `priority` (0–4), `status_id` (optional), `label_id` (repeated) | `201` template |
| `POST /api/defaults/templates/{id}` | same | `200` template |
| `DELETE /api/defaults/templates/{id}` | | `204` |

Validation messages match the user API's for the same fields (role name and permissions from
`workspace/roles.go`, status, type, label and template messages from `issue` and `template`): unknown permission
→ `permission` "Choose permissions from the list."; a Guest default holding a Workspace, Members or Analytics
permission → `permission` "Guests can't have workspace, member or analytics permissions."; duplicate role or label
name → `name` "A role with this name already exists." / "A label with this name already exists."; unknown
`status_id`, `type_id` or `label_id` → that field's "Choose … from the list." Missing id → `404`.

## Admin frontend

- Sidebar link **Defaults** (after Features) to `/defaults`, one page with four sections, in this order, each with a
  short line saying new workspaces start with these:
  - **Roles:** Admin shown as "All permissions" (not editable); Member, Guest and custom roles with their
    permission checkboxes grouped like the user app's role form; add, rename (custom only), delete (custom only).
    Guests' denied groups are disabled for Guest.
  - **Workflow:** status types (name, category, icon from the shared status icons, color) and statuses (name,
    category or type), drag or move-up/move-down to reorder statuses, add, edit, delete (last one can't be).
  - **Labels:** name and color; add, edit, delete.
  - **Templates:** name, title, description (the shared rich editor), priority, status (from the default
    statuses), labels (from the default labels); add, edit, delete.
- Reuse the user app's look for these editors where it's in `@taskwell/shared`; don't import from the user app.
  Forms follow the repo rules (`noValidate`, `@taskwell/shared/validate`, `<Input error>`), sentence case.
- Live: refetch on the admin realtime event, like Config.

## Checks

- Backend: a DB test that edits the defaults (a custom role, a type, a status reorder, a label, a template using
  both), creates a workspace through `workspace.create`, and finds exactly those roles, types, statuses (order),
  labels and template (mapped status and labels) in it; and that an existing workspace is unchanged.
- Admin API tests for the 409s and a guest permission refusal.
- Frontend: `npm run build`.
