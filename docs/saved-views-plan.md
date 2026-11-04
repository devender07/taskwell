# Saved views plan

Scope: save the current issue page together with its display options and filters (everything in the URL) under a name, private or shared with the workspace, and open it from the sidebar.

Out of scope:
- "Update view" to overwrite a saved URL. `ponytail:` delete and save again for now. Add it when people edit views often.
- Ordering views by hand. They're sorted by name.
- Views on pages other than issue lists.

## Schema (`backend/user/internal/db/migrations/013_saved_views.sql`)

```sql
create table saved_views (
  id uuid primary key default uuidv7(),       -- match the id default the other tables use
  workspace_id uuid not null references workspaces(id) on delete cascade,
  user_id uuid not null references users(id) on delete cascade,
  name text not null,
  path text not null,
  shared boolean not null default false,
  created_at timestamptz not null default now()
);
create index saved_views_workspace on saved_views (workspace_id);
```

When a member leaves the workspace, their views go with them. The membership-removal code deletes them, or the agent uses whatever pattern other per-member data uses.

## API contract

A view is returned as `{"id", "name", "path", "shared", "owner": {"id", "name"}, "can_edit"}`. `can_edit` is true for the owner, or for anyone with `workspace.manage` when the view is shared.

| Method & path | Needs | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/views` | member | `200 [view…]`: the caller's own views plus shared ones, by name (case-insensitive) | 401, 404 |
| `POST /api/workspaces/{slug}/views` | member | `201 view` | 400, 401, 404 |
| `POST /api/workspaces/{slug}/views/{id}` | can_edit | `200 view`; only the fields present change | 400, 401, 403, 404 |
| `DELETE /api/workspaces/{slug}/views/{id}` | can_edit | `204` | 401, 403, 404 |

The FormData fields are `name`, `path` and `shared` (`1` or `0`). Validation:
- `name`: trimmed, 1–60 characters. Errors are "Enter a name." and "Use 60 characters or fewer."
- `path`: relative to the workspace and at most 2000 characters. It must match `^/(my-issues|projects/[^/?#]+(/cycles/[0-9]+)?)(\?[^#]*)?$`, otherwise the error is "This page can't be saved as a view."
- Up to 50 views per user per workspace. Past that the error is `400 {"error": "You have 50 views. Delete one first."}`.
- A view the caller can't see returns 404 "View not found."

Changes publish a realtime event on the path `/workspaces/{slug}/views`, to the workspace when the view is shared (before or after the change), else to the owner only.

## Backend

New package `backend/user/internal/view/`, with `routes.go`, `controller.go`, `model.go` and `view_test.go`. Wire it in `cmd/user/main.go` like the other packages. Add `realtime.Views(slug)` next to the other path helpers.

## Frontend (user app)

- `api.ts` gets `type SavedView`.
- The `_main` layout route loads `GET …/views` with `loaderApi`, so the sidebar can read them.
- `components/app/sidebar.tsx`:
  - A "Views" heading appears after Roadmap, only when there are views.
  - Below it is one link per view, with icon `LayersIcon` (add it to icons.tsx if missing) and the name, plus a small `Users` icon when the view is shared.
  - Each link goes to `/${workspace}${path}`. A plain `<Link to>` with the path and search split is fine.
  - The link is `aria-current="page"` when the current pathname and search equal the view's.
- `components/issues/issue-view.tsx`, toolbar: add a "Save view" ghost button.
  - It opens a `Dialog` titled "Save view", with a Name input, a Checkbox "Share with workspace" and a "Save" button.
  - On save it posts `path` = the current pathname with the workspace prefix stripped, plus the search string. The sidebar updates through the realtime event and `reloadPage`.
- When the current page and search match one of the views and `can_edit` is true, the toolbar shows a menu button labelled with the view's name, instead of "Save view". Its items are "Rename" (a dialog), "Share with workspace" or "Stop sharing", and "Delete view" (with a confirm).

Sentence case everywhere.

## Verification

- `go vet`, `go test` covering visibility (private vs shared), the 50 cap, path validation and `can_edit` for an admin.
- `npm run build`.
- Integration:
  - Save a filtered board view.
  - Open it from the sidebar and check that the filters are restored.
  - Share it and see it appear for another member live.
  - Rename and delete it.
