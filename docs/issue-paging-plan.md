# Issue paging plan

Scope: issue lists stop downloading every issue. Filtering, sorting and grouping move to SQL, and the list and board
views load 50 issues per group (list section or board column) and the next 50 when that group's end scrolls into view.
This removes the 2000-issue cap (`maxList`, see the `ponytail:` in `issue/model.go`) and the `ponytail:` in
`docs/views-plan.md` about server-side filtering.

Applies to the three pages that render `IssueView`: a project (`/$workspace/projects/$key`), a cycle
(`/$workspace/projects/$key/cycles/$number`) and My issues (`/$workspace/my-issues`).

Out of scope: keyset pagination (offset is fine at this size; `ponytail:` comment naming it), changing URL params,
virtualised rendering, saved views, manual ordering.

No schema change, no migration, no `system_config` keys.

## API contract

The three list endpoints gain a paged mode, chosen by the presence of `limit`. Without `limit` they answer exactly as
today (other callers such as the MCP server keep working).

| Method & path | Source |
|---|---|
| `GET /api/workspaces/{slug}/projects/{key}/issues` | the project's issues (existing) |
| `GET /api/workspaces/{slug}/my-issues` | the caller's assigned issues in active projects they can see (existing) |
| `GET /api/workspaces/{slug}/projects/{key}/cycles/{number}/issues` | **new**: the cycle's issues; same access as `GET …/cycles/{number}` (404 for a missing cycle/project) |

The new cycle endpoint without `limit` returns `200 [issue…]` like the project list.

### Query params (paged mode)

Same names and value formats as the page URL (`docs/views-plan.md`), so the frontend forwards its search as is.

| Param | Values | Default |
|---|---|---|
| `limit` | 1–100 (required for paged mode) | — |
| `group` | `status`, `assignee`, `priority`, `none` | `status` |
| `sort` | `priority`, `updated`, `created`, `due` | `priority` |
| `status` | comma-separated status ids | all |
| `assignee` | comma-separated user ids, `me`, `none` | all |
| `label` | comma-separated label ids (issue has **any**) | all |
| `priority` | comma-separated `0`–`4` | all |
| `cycle` | comma-separated cycle ids, `none` | all |
| `in` | one group key: return only the next page of that group | — |
| `offset` | ≥ 0, with `in` | 0 |

OR within a filter, AND across filters. Invalid values in a filter are ignored (like the frontend's validateSearch);
a bad `limit`/`offset`/`group`/`sort` is `400 {"error": "That page doesn't exist."}`.

Sort order (ties: newest number first, then id):
- `priority`: urgent (1) → low (4), no priority (0) last
- `updated` / `created`: newest first
- `due`: soonest first, no due date last

Group keys: status id; assignee user id, or `none` for unassigned; priority `"0"`–`"4"`; `all` when `group=none`.

### Responses

First page of every group (no `in`):

```json
{ "total": 312, "groups": [ { "key": "<status id>", "count": 120, "issues": [issue…], "has_more": true } ] }
```

- `total`: issues matching the filters across all groups.
- `groups`: only non-empty groups, in any order (the frontend orders and adds empty columns). `count` is the group's
  total, `issues` its first `limit` issues in sort order, `has_more` = `count > limit`.
- Issue JSON is today's list JSON (no description, with `can_edit` / `can_delete`).

Next page of one group (`in=<key>&offset=<n>`):

```json
{ "issues": [issue…], "has_more": false }
```

Errors: 401, 404 workspace/project/cycle, 400 as above.

Implementation notes (backend): one query per request. First pages via
`row_number() over (partition by <group expr> order by <sort>)` ≤ limit plus `count(*) over (partition by …)`; the `in`
page via `where <group expr> = $key … offset/limit`. Share one builder across the three sources (each is a base
condition: project id / assignee+active+visible / cycle id). Parameters only, never interpolate values. `ponytail:`
offset paging shifts by one when issues move between groups mid-scroll.

Realtime: unchanged. Events already name `…/issues`, `…/my-issues` and the cycle path; the query string is ignored when
matching, so pages refetch live. Publish the new cycle issues path wherever the cycle path is published (add a
`realtime.CycleIssues` helper if needed).

## Frontend (user app)

- `components/issues/view-search.ts`: keep `validateViewSearch`, `filterValues`, `filterParam`, `PRIORITY_ORDER`,
  group ordering/labels. `groupIssues` becomes `orderGroups(page, group, ctx)`: builds every group in display order
  (status order; assignee: me first, members by name, then "No assignee"; priority: Urgent → Low → No priority), each
  with the server's `issues`, `count`, `has_more` (empty groups get count 0). Delete `filterIssues`/`sortIssues`
  (now SQL). Update `view-search.check.ts` to cover `validateViewSearch` and `orderGroups`.
- New `issuePageQuery(search, mine)` → the query string (`limit=50` + the effective group/sort/filters; group falls
  back exactly as IssueView does today: board never `none`, My issues never `assignee`).
- Loaders (project, cycle, my-issues): `loaderDeps` from the view search; fetch the first page with
  `loaderApi(ctx, source + '?' + query)`. The cycle page keeps `GET …/cycles/{number}` for the cycle itself (its
  `issues` stay for the rollover dialog's open count). My issues' empty state uses `total === 0` with no filters.
- `IssueView` props: replace `issues` with `page` (first page response) and `source` (the endpoint path). State: extra
  pages per group key, reset whenever `page` changes (a reload after a move or a realtime event starts every group
  over). Count "N issues" = `page.total`; group headers / column headers show `count`.
- Each group (list section and board column) ends with a sentinel while it `has_more`; when it scrolls into view
  (IntersectionObserver, board: `root` = the column's scroll container) fetch `source?query&in=key&offset=<loaded>`
  and append, de-duplicated by id. One request per group at a time; an error shows in the group and stops it.
- Board columns must scroll vertically on their own (fixed height to the viewport) so each column pages
  independently. List groups page as the window scrolls.
- Drag between columns keeps today's optimistic override, then reloads the page (which resets paging). The moved
  card appears in its new column immediately via the override even if that column hasn't loaded that far.
- Reuse: `useMemberPages`-style logic but for groups; put the hook in `components/issues/issue-pages.ts`.
- Sentence case for every string; no new dependency.

## Verification

- Backend: `go vet`, `go test` (one test for the paged query: filters, each sort, grouping counts, `in`/`offset`),
  curl against the running API with the 510 dummy issues in project VD of workspace `devender`.
- Frontend: `npm run build`.
- Integration: project VD list and board, each grouping and sort, filters, scroll every column to the end, drag a card,
  My issues and a cycle page.
