# Views plan (Phase 3)

Scope: issue lists get display options and filters, all kept in the URL so a view can be shared: list or kanban board, grouping (status, assignee, priority, or none), sorting, and filters by status, assignee, label and priority. Dragging a card between board columns changes the issue's grouped field. A new **My issues** page shows the caller's assigned issues across projects with the same options.

Out of scope: saved/custom views, manual ordering within a column (cards follow the sort), server-side filtering/pagination (`ponytail:` the project and my-issues endpoints return every matching issue and the browser filters; move filters to SQL + paginate when a project passes a few thousand issues), swimlanes, sub-grouping.

## API contract

One new endpoint; everything else reuses the issues API (`docs/issues-plan.md`, `docs/issue-relations-plan.md`).

| Method & path | Needs | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces/{slug}/my-issues` | member | `200 [issue…]` assigned to the caller, in projects that aren't archived, newest first; same Issue JSON as the project list (incl. `parent`, `can_edit`, `can_delete`) | 401, 404 workspace |

Board drops use the existing partial update `POST /api/workspaces/{slug}/issues/{identifier}` with one field: `status_id`, `assignee_id` (empty = no assignee) or `priority`.

## URL state

Both issue pages validate the same search params (shared `validateSearch` helper in `components/issues/view-search.ts`); defaults are omitted from the URL.

| Param | Values | Default |
|---|---|---|
| `view` | `list`, `board` | `list` |
| `group` | `status`, `assignee`, `priority`, `none` (`none` not offered on the board; board treats it as `status`) | `status` |
| `sort` | `priority` (urgent→low, no priority last), `updated` (newest), `created` (newest), `due` (soonest, none last) | `priority` |
| `status` | comma-separated status ids | all |
| `assignee` | comma-separated user ids, plus `me` and `none` | all |
| `label` | comma-separated label ids (issue has **any** of them) | all |
| `priority` | comma-separated `0`–`4` | all |

Unknown values are dropped. Ties in every sort fall back to newest number first. Within a filter values are OR; across filters AND.

Links that change the search use `search={(prev) => …}` with `from={Route.fullPath}` (CLAUDE.md).

## Frontend (user app)

Shared pieces in `components/issues/` (only the user app needs them):

- `view-search.ts`: `validateSearch` + pure helpers `filterIssues(issues, search, userId)`, `sortIssues(issues, sort)`, `groupIssues(issues, group, {statuses, members})` returning ordered groups `{ key, label, issues }`. **One runnable check** `view-search.check.ts` (node --experimental-strip-types) covering filtering, each sort, grouping order and URL parsing.
- `issue-view.tsx`: the toolbar + list/board for a given issue array. Props: workspace, issues, statuses, labels, members, user, search, and `showProject` (My issues shows identifiers from many projects, so nothing extra is needed; `showProject` only hides the assignee filter's "me" redundancy — see below).
- Toolbar (one row, wraps on narrow screens): view `Tabs` (List / Board), Group `SmartAutocomplete` (Status / Assignee / Priority / No grouping), Sort `SmartAutocomplete` (Priority / Last updated / Created / Due date), and a "Filter" button opening a shared `Popover` with four checkbox sections (Status, Assignee with "Me" and "No assignee" first, Priority, Label). Active filters show as chips after the toolbar ("Status: Todo, Done ×"), plus "Clear filters". A count "N issues" on the right.
- **List**: one section per group in group order (status order; assignee: me first, then members by name, then "No assignee"; priority: Urgent → Low → No priority). Section header: icon/avatar, name, count. Empty groups hidden. Rows are today's issue rows (`projects/$key.tsx` row markup moves into `issue-view.tsx` and is reused). `group=none` → one unheaded list.
- **Board**: horizontally scrolling columns, one per group value — status: every status (empty columns shown); priority: all five; assignee: "No assignee" + every member. Column header: name + count. Cards: identifier, title, label dots, priority (when set), due date (pink when overdue), assignee avatar. Clicking a card opens the issue.
- **Drag between columns**: pointer events (works with mouse and touch; no HTML5 drag-and-drop, which is unreliable on iPad), no dependency. Press-and-hold 150ms or move > 6px starts a drag; the card follows the pointer as a translucent copy; the column under the pointer highlights (`document.elementFromPoint` → closest `[data-column]`); dropping on another column updates the field optimistically and saves; on failure the card snaps back and an error line shows above the board. Cards of issues with `can_edit: false` aren't draggable. Dragging within 56px of the board's left or right edge scrolls it (added during integration: on iPad most columns are off-screen). A plain click (no drag) still opens the issue.
- Keyboard: cards are links (Enter opens). No keyboard drag (status can be changed on the issue page); note this in a `ponytail:` comment.

Pages:

| Route | Behaviour |
|---|---|
| `/$workspace/projects/$key` (changed) | Issues section uses `IssueView` with the route's search. "New issue" stays. Project loader unchanged (fetches every issue). |
| `/$workspace/my-issues` (new, `routes/_app/$workspace/_main/my-issues.tsx`) | Title "My issues". Loader: `GET …/my-issues` + statuses + labels + members. `IssueView` without the assignee filter section and without the assignee grouping option (everything is yours). Empty: "Nothing assigned to you." |
| Sidebar | "My issues" (inbox/user icon, same style) between Home and Projects. |

Sentence case everywhere. Pages are full width (like the project page). Board columns 280px wide.

## Backend (`backend/user/internal/issue/`)

Add the my-issues handler + query (`i.assignee_id = $user and p.archived_at is null`, reusing `issueCols` and the same `can_edit`/`can_delete` computation as the project list) and the route. Verify with curl: only own assigned issues, archived projects excluded, 401/404.

## Verification

- `go vet`, `go test`, curl for my-issues.
- `npm run build`; `node --experimental-strip-types user/src/components/issues/view-search.check.ts`.
- Integration (main session, headless Chrome, also at iPad size with touch): switch list/board, each grouping and sort, filters (incl. "me" and "none") reflected in the URL and restored on reload/shared link, drag a card between status columns (mouse and touch) and see it persist, drag disabled for read-only issues, My issues shows only assigned issues from active projects.
