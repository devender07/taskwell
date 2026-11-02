# Status types plan

Scope: in **Settings → Workflow**, a workspace can add its own status **types** that work exactly like the built-in
ones (Backlog, Todo, In progress, Done, Canceled): each has a name, its **own icon and color**, and how it counts
(open / started / done / canceled). A status then just picks a type. This replaces today's free-text type with
"works like" ("Reopen · works like Todo", drawn with Todo's icon).

Example: type "In review", icon eye, orange, counts as In progress. The status "Code review" of type In review shows
an orange eye everywhere: lists, boards, the issue page, pickers, Home, the timeline, search.

## Rules

- The five built-in types are the categories as today (no rows, fixed icons). Custom types are rows per workspace.
- A custom type's `category` is its behaviour (what counts as open, done…, as today). Its statuses always take the
  type's category: saving a status with a type sets the status's category from it; changing a type's category
  updates its statuses.
- Deleting a type turns its statuses back into built-in ones of the same category (their own icon goes away).
- Status icons render from the status's `icon` and `color` when set, else the category's built-in icon. Old activity
  snapshots without them fall back the same way.
- Manage with the existing `workflow.manage` permission.

## Icons

A fixed set of keys, drawn by the shared icon library (`@tether/shared/icons`, `StatusTypeIcon`): `circle`,
`circle-dashed`, `circle-half`, `circle-check`, `circle-x`, `eye`, `refresh`, `pause`, `clock`, `flag`, `star`,
`bug`, `rocket`, `alert`, `lock`, `message`. Backend validates against the same list.

## Schema

Migration `backend/user/internal/db/migrations/043_status_types.sql`:

```sql
create table status_types (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null check (char_length(name) between 1 and 30),
  category     text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  icon         text not null,
  color        text check (color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'), -- null = the category's color
  created_at   timestamptz not null default now()
);
create unique index on status_types (workspace_id, lower(name));

alter table statuses add column type_id uuid references status_types on delete set null;
create index on statuses (type_id);
-- Backfill: one type per (workspace, statuses.type) with the first status's category and a default icon
-- ('refresh' for "Reopen", else by category: backlog circle-dashed, unstarted circle, started circle-half,
-- completed circle-check, canceled circle-x); point the statuses at it; then drop statuses.type.
```

## API contract

FormData in, JSON out, `workflow.manage` for writes (403 `{"error":"You don't have permission to do that."}` as today).

Status JSON everywhere a status appears (issue, lists, activity, notifications, pull requests, statuses endpoint):
`{"id","name","category","position","icon": string|null,"color": string|null}`. The statuses endpoints also give
`"type": {"id","name"} | null` (replacing today's `type` string).

Type JSON: `{"id","name","category","icon","color": string|null,"status_count": n}`.

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/status-types` | — | `200 [type…]` by name | 404 |
| `POST /api/workspaces/{slug}/status-types` | `name`, `category`, `icon`, `color` (optional) | `201 type` | 400 `{"errors":{"name":"Type name is required." / "Type name must be 30 characters or fewer.","category":"Choose how it counts.","icon":"Choose an icon.","color":"Choose a color."}}`; 409 `{"errors":{"name":"That type already exists."}}` |
| `POST /api/workspaces/{slug}/status-types/{id}` | same | `200 type`; its statuses take the new category | as above; 404 `{"error":"Type not found."}` |
| `DELETE /api/workspaces/{slug}/status-types/{id}` | — | `204`; its statuses become built-in | 404 |
| `POST /api/workspaces/{slug}/statuses`, `POST …/statuses/{id}` | `name`, `category`, `type_id` (optional; replaces `type`). With `type_id`, `category` is ignored and taken from the type | as today | as today; 400 `{"errors":{"type_id":"Choose a type."}}` for an unknown type |

Realtime: type changes publish `workflow.changed` with `…/status-types` and `…/statuses` (and everything showing
statuses refetches as it does for status changes today).

## Frontend (user app)

- Icons: `StatusTypeIcon({ icon, color, size })` and the key list in the shared icon library.
- `StatusIcon` takes a status (or `{category, icon, color}`) and draws the custom icon when set; update its callers.
- Settings → Workflow:
  - **Types** section: the five built-ins (read-only, their icons, "Built in"), then custom types with Edit and
    Delete, and an "Add type" button. The add/edit dialog: Name, Icon (a grid of the icon set, keyboard reachable),
    Color (`@tether/shared/color-picker`), "Counts as" (the five categories, radio-style, with their icons).
    Delete asks to confirm ("Statuses of this type become …").
  - **Statuses**: the form is Name + Type (one picker of built-in and custom types, each with its icon). No "works
    like", no free-text type. Rows show the status's icon and its type's name.

## Agents

- Backend: migration with backfill, `issue` package (statuses, the new type endpoints, the status JSON with icon and
  color in every place it's built: issue model, notify, gitlink, activity), tests for the endpoints, the backfill, a
  type's category change reaching its statuses, and delete. `go vet`, `go test ./user/...`.
- Frontend: as above. `npm run build`, `npm run check`.
