# Workflow rules plan

Scope: control who can mark work done. A new role permission, **Move issues to completed statuses** (`issues.complete`), gates moving an issue into any status in the Completed type. A workspace takes it away from its Developer role and gives it to its QA role, so developers can't close tickets and QA can.

Out of scope:
- Rules per status, per project, or per transition ("from X to Y"). That would be the automation builder, which stays unbuilt until a customer asks.
- Requiring test results before a move. This can come later as a second check in the same place.

## Model

No new tables. `issues.complete` joins the permission catalog (`workspace/permissions.go`) in the Issues group, after `issues.status_any`: `{"issues.complete", "Issues", "Move issues to completed statuses", true}`.

`043_issues_complete.sql`:
- Add `issues.complete` to every stored non-admin role (member, guest, custom) so nothing changes until an admin removes it. Admin already holds the whole catalog through `Effective`.
- `memberDefaults` and the guest defaults gain it too, so new workspaces start the same way.

## Enforcement (backend)

The check is "the target status's category is `completed`, and the status actually changes, and the caller lacks `issues.complete`". It runs in the issue controller, where permissions live, on every path a person uses to set `status_id`:

- `POST /workspaces/{slug}/issues/{id}` (update) and the board move endpoint, including the status-only path in `editable`
- create with a completed `status_id`
- bulk update (`bulk.go`): refuse the whole request if any issue would move into a completed status
- the public API and MCP, which go through the same handlers. The backend agent confirms this for each one.

Error: 403 `{"error": "You don't have permission to move issues to <status name>."}`. On create and update forms this is a field error instead: 400 `{"fields": {"status_id": "You don't have permission to move issues to <status name>."}}`.

Not checked:
- Imports and templates: they create issues on behalf of the workspace, and both already need admin-level permissions.
- Status delete with `move_to`: this needs `workflow.manage`, which is an admin action.
- A merged PR moving an issue (`gitlink`): no person is acting. Workspaces that want QA in the loop set the merge status to a non-completed one, such as "In QA", in the GitHub/GitLab settings. The settings page gets a caption saying so.

`Issue` JSON gains `can_complete` (bool), set in `allow` from `issues.complete` and `can_change_status`.

## Frontend

- Status pickers (issue page, quick-create, bulk bar, command palette "Change status"): completed statuses are disabled when the caller can't complete the issue, using `can_complete` (or `can(workspace, 'issues.complete')` where there's no issue yet), with the hint "Your role can't move issues here".
- Kanban: the Completed columns refuse a drop from such a user (no drop target), and the card stays put. If the server still returns 403, the error shows as the usual toast.
- Settings → Workflow: the Completed card shows "Only roles with Move issues to completed statuses can move issues here", linking to Settings → Roles.
- Settings → Roles shows the new permission from the catalog with no other change.

## Checks

- Go test: a member whose role lacks `issues.complete` gets 403 on update, board move, bulk and create into a completed status, and 200 into a started one. An admin gets 200.
- Click through: remove the permission from Member, then try every picker and a kanban drag as a member.
