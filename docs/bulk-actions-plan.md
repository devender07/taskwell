# Bulk actions plan

Scope: select several issues in the list view (project, cycle and My issues pages) and change their status, assignee, priority, cycle or labels, or delete them, in one request.

Out of scope: selecting on the board (`ponytail:` list only; add card checkboxes when someone asks), select-all across pages that aren't loaded (only loaded rows can be picked), undo.

## API contract

| Method & path | Needs | Success | Failures |
|---|---|---|---|
| `POST /api/workspaces/{slug}/issues/bulk` | member | `200 {"updated": n}` or `200 {"deleted": n}` | 400, 401, 403, 404 workspace |

FormData:

| Field | Meaning |
|---|---|
| `ids` | Repeated issue identifiers (`ABC-12`), 1–250, duplicates ignored. Required. |
| `status_id` | Set the status (a workspace status). |
| `assignee_id` | Set the assignee; empty = no assignee. Must be able to see every issue's project. |
| `priority` | `0`–`4`. |
| `cycle` | Set the cycle; empty = no cycle. Must belong to every issue's project. |
| `add_label_ids` | Repeated label ids added to each issue (kept labels stay). |
| `remove_label_ids` | Repeated label ids removed from each issue. |
| `delete` | `1` deletes every issue instead; no other field allowed with it. |

Rules:
- All or nothing, one transaction. Each issue goes through the same permission check as the single endpoints: `can_edit` for changes, `can_delete` for delete.
- Errors use the existing shapes:
  - `400 {"error": "Pick some issues."}` when `ids` is empty or over 250.
  - `400 {"error": "Choose something to change."}` when no field is present.
  - `400 {"error": "Some issues weren't found."}` for an unknown or invisible identifier.
  - `400 {"errors": {field: message}}` with the same messages as `POST …/issues/{identifier}` (for example a cycle not in an issue's project gives `cycle`, and an assignee who can't see a project gives `assignee_id`).
  - `403 {"error": "You can't edit ABC-3."}` (or "delete") naming the first issue that fails the check.
- Each issue logs its own activity events and notifications, and publishes the same realtime paths as a single update or delete.

## Backend (`backend/user/internal/issue/`)

- `bulk.go` holds the handler, which parses ids and fields. It reuses `readFields` per project for validation, or splits out the part that doesn't need a key. It also reuses `allow`.
- `model.go` gets `bulkUpdate` and `bulkDelete`, which run one tx, call `apply` per issue, and turn the label add and remove into the issue's full label set.
- Add the route in `routes.go`. Add a test in `bulk_test.go` that covers:
  - all-or-nothing on a 403
  - adding and removing labels
  - an assignee who can't see a private project
  - delete

## Frontend (user app, `components/issues/issue-view.tsx`)

- The list view gets selection state: a `Set` of issue ids, reset when the source or filters change.
- Each `IssueRow` in the list gets a 44px checkbox cell before the link. It's a sibling of the link, not inside it.
  - The checkbox is visible on hover, on focus-within, while anything is selected, and always on touch devices (`@media (hover: none)`).
  - It's disabled for issues with `can_edit: false`.
- Shift-click on a checkbox selects the range from the last one clicked. `X` on a focused row toggles it, and `Escape` clears the selection.
- While something is selected, a bar fixed to the bottom of the viewport (z-index 100) shows:
  - "N selected" with "Clear".
  - Status, Assignee, Priority and Labels, as popovers reusing the filter menu's option lists. Labels has "Add label" and "Remove label" sections.
  - Cycle, only when the page passes `cycles`.
  - Delete, with a confirm dialog: "Delete N issues?"
- Assignee options come from workspace members, the same source the issue form uses.
- Each action posts once to `…/issues/bulk`, then `reloadPage`. Errors show in the bar's `FormError`, and the selection stays so it can be retried. On success the selection clears and an sr-only status says "Updated N issues." or "Deleted N issues."
- At 640px and below the bar spans the width and its buttons wrap.

Sentence case everywhere.

## Verification

- `go vet`, `go test`, and curl for each field and error.
- `npm run build`.
- Integration: select with click, shift-click and `X`, then change the status on several, add and remove a label, bulk-assign, and bulk-delete with the confirm. Check a 403 on a read-only issue (via curl), and check the bar at 360px.
