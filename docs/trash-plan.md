# Trash and undo plan

Scope: deleting an issue moves it to the trash for 30 days. The person who deleted it gets an "Undo" toast. A Trash page lists trashed issues, which can be restored or deleted forever. After 30 days the sweeper deletes them for good.

Out of scope:
- Trash for comments, projects and chats.
- Undo for edits.

## Schema (`017_issue_trash.sql`)

Soft delete with zero query churn:

```sql
alter table issues rename to issues_all;
alter table issues_all add column deleted_at timestamptz, add column deleted_by uuid references users(id) on delete set null;
create view issues as select * from issues_all where deleted_at is null;   -- auto-updatable
create index issues_all_trash on issues_all (workspace_id, deleted_at) where deleted_at is not null;
```

- Existing queries keep reading and writing `issues` and never see trashed rows.
- Foreign keys follow the renamed table.
- Things to check:
  - triggers (search vectors, NOTIFY)
  - `insert … returning` and `on conflict` against the view
  - `select … for update`
  - functions that reference `issues` (the view is fine for reads)

  Move anything that breaks onto `issues_all`.
- `select *` in the view freezes the column list. Note in the migration that a future column must be added to both: recreate the view.
- Issue numbers stay unique because the constraint lives on the table, so a trashed issue keeps its number.

## Behaviour

- `DELETE …/issues/{identifier}` and bulk delete set `deleted_at = now()` and `deleted_by` instead of deleting. Activity, webhooks (`issue.deleted`) and realtime stay as today.
- Sub-issues of a trashed issue stay put. Their parent reference disappears from the view joins, and comes back on restore.
- Links stay in the table and reappear on restore.
- The sweeper deletes `issues_all` rows with `deleted_at < now() - 30 days`. The cascades then remove comments and the rest.
- Restoring clears `deleted_at`, logs a `restored` event ("Name restored this issue") and publishes the same realtime paths as create.

## API contract

| Method & path | Needs | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/trash?before=<rfc3339>` | member | `200 {"issues": [{"id", "identifier", "title", "project": {"key", "name"}, "deleted_at", "deleted_by": {"id", "name"} \| null, "can_restore"}], "has_more"}`, 50 per page, newest deleted first, only projects the caller can see | 401, 404 |
| `POST /api/workspaces/{slug}/trash/{identifier}/restore` | `can_delete` on the issue (same rule as delete) | `200 issue` | 403, 404, 409 "The project is archived." |
| `DELETE /api/workspaces/{slug}/trash/{identifier}` | `issues.edit_any` | `204` (gone for good) | 403, 404 |
| `DELETE /api/workspaces/{slug}/trash` | `issues.edit_any` | `204`, empties everything the caller can see | 403 |

The single delete response stays `204`. The frontend shows the Undo itself.

## Frontend

- **Undo toast.** There's no toast component yet, so add a minimal one: `frontend/shared/src/components/toast/` with a `ToastProvider` and `useToast().show({ message, action })`.
  - It sits fixed at the bottom-left, at z-index 995, with `role="status"`, and disappears after 8 seconds.
  - One toast at a time.
  - Mount the provider in the user app root.
- Deleting from the issue page, the bulk bar or the command palette shows "Deleted ABC-12" (or "Deleted 4 issues") with "Undo". Undo posts restore for each issue, then `reloadPage`. After deleting from the issue page, the page navigates as it does today, and the toast survives the navigation.
- **Trash page:** `/$workspace/trash`, linked at the bottom of the sidebar with `TrashIcon` (added if missing). Each row shows the identifier, title, project, "Deleted by X · 3 days ago", "Restore", and "Delete forever" (with `issues.edit_any`). The header has "Empty trash" with a confirm.
- The empty state: "Trash is empty. Deleted issues stay here for 30 days."
- The issue-delete confirm text changes to: "It moves to the trash for 30 days."

## Verification

- `go test`: delete → trashed and hidden from lists, search, my-issues and counts; restore; purge after 30 days (set `deleted_at` back in the test); permissions; bulk delete trashes.
- Run the full existing test suite to catch view incompatibilities.
- `npm run build`.
- Integration: delete, then Undo; delete, then restore from Trash; delete forever.
