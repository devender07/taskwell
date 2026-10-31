# Home plan

Scope: the workspace Home page (`/$workspace`) becomes a personal dashboard. It shows a greeting, what needs your attention, your assigned issues, unread inbox items, a live workspace activity feed, recently viewed issues, and project progress. Everything updates live through the existing realtime socket. No new dependencies.

Out of scope: customisable or draggable widgets, charts beyond the project progress bars, per-user Home settings, and activity filters.

## Data

| Block | Source |
|---|---|
| Greeting | `user` from the `_app` context; date and time of day from the browser clock |
| Needs attention, Assigned to me | existing `GET /api/workspaces/{slug}/my-issues` (lean list, already live via `/my-issues` events) plus existing statuses |
| Inbox | existing `GET …/notifications` (first page), filtered to unread on the client, max 5; existing `POST …/notifications/read` with `all=true` |
| Recent activity | **new** `GET …/activity` |
| Recently viewed | browser `localStorage` (`tether:recent:<workspace slug>`), written by the issue page |
| Projects | **new** `GET …/project-stats` plus the existing projects list |

## API contract (new endpoints)

### `GET /api/workspaces/{slug}/activity?before=<RFC3339Nano>`

- **Access:** members only; non-members get 404 "Workspace not found."
- **Response:**

```json
{"items":[
  {"type":"event","id":"…","created_at":"…","actor":{"id","name"}|null,
   "issue":{"identifier":"ENG-12","title":"…","status":{"id","name","category","position"}},
   "kind":"status","data":{…}},
  {"type":"comment","id":"…","created_at":"…","actor":{"id","name"}|null,
   "issue":{…same…},"excerpt":"first 200 characters of the comment's plain text"}
 ],
 "has_more":true}
```

- **Sources:** issue events (the same kinds and `data` shapes as the timeline) and comments, merged. Only issues in non-archived projects are included.
- **Paging:**
  - Newest first, at most 30 items per page.
  - `before` is exclusive; leave it out to get the newest items.
  - The same tie rule as the timeline: a page never splits items that share a `created_at`.
  - A bad `before` returns 400 `{"error":"before must be an RFC 3339 time."}`.
- **Excluded:** `description` events (too noisy), and events whose actor is the only person who has ever touched the issue. That second rule is optional: implement it only if it's cheap; otherwise include them.
- **Indexes:** the query must use indexes. Add `issue_events (workspace-scoped) created_at` and `comments created_at` indexes through the issue join, or denormalise `workspace_id` onto `issue_events` and `comments` if that's simpler. Either way, use a non-transactional `concurrently` migration (`*.notx.sql`).

### `GET /api/workspaces/{slug}/project-stats`

- **Access:** members only; non-members get 404.
- **Response:** `200 [{"key":"ENG","open":12,"done":30,"canceled":2,"overdue":3}]`. There is one entry per non-archived project, in the same order as the projects list.
  - `open`: issues whose status category is `backlog`, `unstarted` or `started`.
  - `done`: category `completed`.
  - `overdue`: open issues with `due_date < current_date`, using the server's date.
- **Speed:** a single grouped query.

### Realtime

These paths join the existing events, in `backend/user/internal/realtime/event.go`:
- `IssuePaths` also includes `…/activity` and `…/project-stats`.
- Comment events also include `…/activity`.
- Project create, update, archive and unarchive events also include `…/project-stats`.
- Status deletes (the `"{ws}/"` prefix) are already covered.

The client throttles both new paths to at most one refetch every 3 s. That throttle goes in `frontend/user/src/realtime.ts`, next to the existing list throttle.

## Frontend (`routes/_app/$workspace/_main/index.tsx` + `components/home/*`)

**Loader:** in parallel, through `loaderApi`, it loads my-issues, statuses, the first page of notifications, the first page of activity, projects, and project-stats. Recently viewed comes from `localStorage` in the component.

**Layout:**
- At ≥ 1024px, a 2-column grid: the left column is roughly 60% and the right roughly 40%. Below 1024px, everything sits in one column.
- Left column, in order: Needs attention, Assigned to me, Recent activity.
- Right column, in order: Inbox, Recently viewed, Projects.
- Full width, with the same page padding as the project page.
- Each block is a card: nested surface, 8px radius, a header row with the title (sentence case) and an optional right-aligned link.

**Blocks:**
1. **Greeting:** "Good morning, {first name}" (morning 5–12, afternoon 12–17, evening 17–5), with the date below it ("Saturday, October 31") in the browser locale. Keep the existing "You no longer have access…" notice above it.
2. **Needs attention:** your open issues that are overdue, due today or due within 7 days, sorted by due date and then priority.
   - Each row shows the identifier, title and status icon, and the due date: pink when overdue, "Today" when it's today.
   - Show at most 8, with "View all" linking to My issues sorted by due date (`?sort=due`).
   - Empty: "Nothing due this week."
3. **Assigned to me:** your other open issues, meaning those not shown in Needs attention, grouped by status in workflow order with at most 5 per group.
   - Each group header shows the status name and count. Rows show identifier, title and priority.
   - "View all" links to My issues.
   - Empty: "Nothing assigned to you." with a "Create issue" button that opens quick-create.
4. **Inbox:** up to 5 unread notifications, using the same row style and wording as the inbox page. Clicking a row marks it read and opens the issue.
   - "Mark all read" calls `all=true`, and "View inbox" links to the inbox.
   - Empty: "You're all caught up."
5. **Recent activity:** the items from `/activity`.
   - Each row has the actor avatar, one line in the timeline's wording plus the issue link ("**Maya** moved **ENG-12** Fix login from Todo to Done"), and the relative time.
   - Comments show their excerpt on a second muted line.
   - 5 rows per page with the shared `Pagination` (Previous / Next). Rows come from the API 30 at a time; Next fetches the next batch with `before` only when the loaded rows run out. Realtime refetches replace only the first batch, the same way the timeline does.
   - Empty: "No activity yet."
6. **Recently viewed:** the last 6 issues opened in this browser.
   - The issue page records `{identifier, title}` on load, de-duplicated, newest first, at most 20 stored.
   - Each row shows identifier and title, plus the live status when the issue is in my-issues; otherwise just the title.
   - Issues deleted since then disappear when a click returns 404, and are pruned on 404.
   - Empty: "Issues you open will show up here."
7. **Projects:** one row or card per project, showing the name and key, a progress bar of done over (open + done), "N open · M done", and the overdue count in pink when it's above 0. Clicking opens the project.
   - Empty, for users with `projects.manage`: "Create a project" links to Projects. Otherwise: "No projects yet."

**Keyboard and accessibility:** every row is a link. Blocks are `section`s with `aria-labelledby` pointing at their heading. The progress bar is a `role="progressbar"` with values. Touch targets are 44px on touch devices.

**Realtime:** loader paths are tracked as on every other page, so events refresh the right blocks. A `storage` event refreshes Recently viewed when another tab adds to it.

## Backend

- **Where it lives:** `issue` package (activity: SQL in `model.go`, handler in `controller.go`, route in `routes.go`) and `project` package (project-stats), or `issue` for both if the queries need issue internals. The realtime path helpers go in `realtime/event.go`.
- **Tests:** integration tests guarded by `TEST_DATABASE_URL`, covering:
  - activity merge, order and paging with tied timestamps;
  - archived projects excluded;
  - project-stats counts, overdue included;
  - a non-member gets 404.

## Verification

- `go vet`, `go test -race` with `TEST_DATABASE_URL`, `npm run build`, `npm run check`, `npm run e2e`.
- **Integration (headless Chrome, demo and Maya):**
  - Home renders every block with seeded data, and every empty state renders in a fresh workspace.
  - Maya changes an issue's status, and Demo's Home activity and project bar update without a reload.
  - An issue's due date set to yesterday appears in Needs attention in pink.
  - Opening an issue adds it to Recently viewed.
  - "Mark all read" clears the Inbox block and the bell.
  - The layout fits at 820px (iPad) and 390px.
- Add a Home check to `frontend/e2e/user.mjs`.
