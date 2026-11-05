# Member analytics plan

Scope: a page per member with their current workload and how their work moved over the last 12 weeks, and an
admin-facing Team page with one row per member. Workload and throughput only: no rankings, no "top" lists, no scores.

Who sees what: everyone sees their own numbers; seeing anyone else's needs the new `analytics.view` permission (Admin
has it, like every permission; grantable through custom roles). Every number counts only issues in projects the
**viewer** can see (`can_see_project(project_id, viewer)`), so analytics never leak private projects.

Out of scope: story points/estimates, per-project breakdowns, export, date-range picker (fixed 12 weeks), email digests.

No schema change, no migration, no `system_config` keys. Everything is computed from existing tables:
`issues`, `issue_events` (kind `status` carries `data.from` / `data.to` status snapshots with `category`; kind
`created`), `comments`, `test_runs`, `cycles`, `statuses`.

## Definitions

- **Completed**: an `issue_events` row of kind `status` whose `data->'to'->>'category' = 'completed'`, credited to the
  issue's current assignee (there's no assignee history) and only while the issue is still in a completed status. Each
  issue counts once, at its latest completion event in the window.
- **Cycle time** of a completed issue: from its first `status` event into category `started` to that completion
  event; issues never started are skipped. Reported as the median in days (one decimal) over the last 12 weeks, and
  `null` when there are none.
- Weeks are ISO weeks (Monday start) in UTC; the series always has 12 entries, oldest first, zeros included.
- **Open**: assigned to the member, status category in `backlog`, `unstarted`, `started`, project not archived.

## API contract (user service)

### `GET /api/workspaces/{slug}/members/{id}/analytics`

Auth: signed in, workspace member. `id` is the caller's id, or the caller has `analytics.view`; otherwise `403
{"error": "You don't have permission to see this member's analytics."}`. `404 {"error": "Member not found."}` when
`id` isn't a member (or isn't a uuid).

`200`:
```json
{
  "member": {"id": "…", "name": "…", "email": "…", "role": {"id": "…", "name": "…"}},
  "open": {"total": 7, "by_category": {"backlog": 2, "unstarted": 3, "started": 2}, "overdue": 1, "due_this_week": 2},
  "weeks": [{"week": "2026-08-17", "completed": 3, "created": 5, "comments": 12}],   // 12 entries, oldest first
  "cycle_time_days": 2.5,
  "completed_30d": 9,
  "tests": {"runs_started_30d": 4},
  "current_cycles": [{"project": {"key": "ENG", "name": "Engineering"}, "number": 12, "name": "Cycle 12",
                      "assigned": 6, "completed": 4}]
}
```
- `created`: issues the member created that week. `comments`: comments they wrote that week (not deleted).
- `tests.runs_started_30d`: test runs (any kind) they started in the last 30 days.
- `current_cycles`: cycles in state `current` (today within dates) where the member has assigned issues.

### `GET /api/workspaces/{slug}/analytics/team`

Auth: `analytics.view`, else `403`. `200`:
```json
{"members": [{"id": "…", "name": "…", "open": 7, "overdue": 1, "completed_30d": 9, "cycle_time_days": 2.5}]}
```
Every workspace member, ordered by name. Same definitions and project visibility as above.

### Permission

Add `PermAnalyticsView = "analytics.view"` to `backend/user/internal/workspace/permissions.go` catalog, group
"Analytics", label "See other members' analytics and the team page", active. Admin gets it automatically.

### Realtime

No new events. The pages load on open and refetch every 60 s while the tab is visible.

## Frontend

- Route `/$workspace/members/$id` (member page). Header: avatar, name, role. Cards (reuse Home's `Card`, DESIGN.md
  tokens):
  - **Workload**: open total with a by-category bar (status category icons), overdue (pink), due this week.
  - **Completed per week**: 12 bars with the week label on hover/focus; below it "Median cycle time: 2.5 days"
    (or "No completed issues yet").
  - **Activity per week**: created and comments as a second small bar pair or two compact rows.
  - **Testing** and **current cycles** (cycle name, "4 of 6 done", link to the cycle).
  - Empty states in sentence case. Charts: plain SVG or CSS bars, no chart library; accessible (a `<table>`
    alternative or `aria-label` per bar).
- Route `/$workspace/team` (needs `analytics.view`; hidden from the sidebar otherwise): sortable table — member, open,
  overdue, completed (30 days), median cycle time — each row linking to the member page. Sorting state in the URL
  (`validateSearch`).
- Links: clicking a member's name/avatar in Settings → Members, the people panel, and issue assignee chips opens their
  member page; "View your analytics" in the account menu. A sidebar "Team" link only with `analytics.view`.
- A member without the permission who opens someone else's page sees the 403 message in the page.

## Tests

- Backend (dbtest): seed statuses and events at known times; assert weeks series (12 entries, zeros), completed counts
  each issue once, median cycle time, private-project issues excluded for a viewer who can't see them, 403 without
  `analytics.view` for another member and 200 for self, 404 for a non-member, team endpoint 403/200.
- Frontend: a `.check.ts` for any pure helper (e.g. week labels, bar scaling).
