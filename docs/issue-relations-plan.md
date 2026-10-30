# Sub-issues and issue links plan

Scope (Phase 2, part 2): an issue can have sub-issues (one level deep, same project), and issues can block each other ("blocks / blocked by", any project in the workspace). Adds a small issue search used by the pickers. Builds on `docs/issues-plan.md`; everything there stays.

Out of scope: "relates to" / duplicate links, deeper nesting, progress roll-up on parents, blocking enforcement (a blocked issue can still move to Done).

## Schema

Migration `backend/user/internal/db/migrations/010_issue_relations.sql`:

```sql
alter table issues add column parent_id uuid references issues on delete set null; -- deleting a parent makes its sub-issues top-level
create index on issues (parent_id);

create table issue_links (
  blocker_id uuid not null references issues on delete cascade,
  blocked_id uuid not null references issues on delete cascade,
  created_at timestamptz not null default now(),
  primary key (blocker_id, blocked_id),
  check (blocker_id <> blocked_id)
);
create index on issue_links (blocked_id);
```

Rules (checked in Go, inside the write's transaction):
- Parent must be in the **same project** and must not itself have a parent; the child must not have sub-issues; an issue can't be its own parent.
- A link needs both issues in the same workspace; at most one link per pair in either direction (A blocks B and B blocks A can't both exist).

## API contract

Same conventions as the issues plan (FormData in, JSON out, 401/404 workspace/403 messages). Edit rule = `issues.edit_any`, or creator, or assignee (of the issue named in the path).

Issue ref: `{"id","identifier","title","status": status}`

Issue JSON changes:
- Every issue (list and single): `"parent": issueRef|null`.
- Single issue (`GET …/issues/{identifier}` and the responses of the write endpoints below) also has `"sub_issues": [issueRef…]` (by number), `"blocks": [issueRef…]`, `"blocked_by": [issueRef…]`.

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/issues?q=` | member | query `q` (identifier like `ENG-12` / `eng-12`, or words in the title); optional `project` (key) to limit to one project | `200 [issueRef…]` up to 20; exact identifier first, then title matches newest first; empty `q` → newest 20 | 404 workspace |
| `POST /api/workspaces/{slug}/projects/{key}/issues` (changed) | as before | + `parent` (identifier, optional) | as before | + 400 `{"errors":{"parent": msg}}` |
| `POST /api/workspaces/{slug}/issues/{identifier}` (changed) | edit rule | + `parent` (identifier; empty clears) | as before | + 400 `parent` |
| `POST /api/workspaces/{slug}/issues/{identifier}/links` | edit rule | `type` (`blocks` or `blocked_by`), `issue` (identifier) | `201 issue` (the path issue, with its relations) | 400 `{"errors":{"type":"Choose a link type."}}` / `{"errors":{"issue": msg}}`; 403; 404 issue; 409 `{"errors":{"issue":"These issues are already linked."}}` |
| `DELETE /api/workspaces/{slug}/issues/{identifier}/links/{other}` | edit rule | — | `200 issue` (removes the link between the two, whichever direction) | 403; 404 `{"error":"Link not found."}` / issue not found |

`parent` messages: `Issue not found.` (no such identifier in the workspace) · `Choose an issue in the same project.` · `An issue can't be its own parent.` · `Sub-issues can't have sub-issues.` (the chosen parent has a parent) · `This issue has sub-issues, so it can't be one.`
`issue` (link) messages: `Issue not found.` · `An issue can't block itself.`

## Backend (`backend/user/internal/issue/`)

- Extend the existing package: queries in `model.go`, handlers in `controller.go` (or `relations.go` if that keeps files readable), routes in `routes.go`. Reuse the identifier parser and edit-rule helper.
- Search: identifier match via the parser (project key + number); title match `title ilike '%' || q || '%'` (escape `%`/`_` in `q`). `ponytail:` ILIKE scan; add trigram/full-text index when search gets slow (Phase 5).
- Verify: `go vet`, `go test`, curl every row and every parent/link message; deleting a parent makes children top-level; deleting an issue removes its links; search by identifier (any case) and by title; `project` filter.

## Frontend (user app)

`api.ts`: `IssueRef`; `Issue` gains `parent`, optional `sub_issues`, `blocks`, `blocked_by`.

Issue pickers use `SmartAutocomplete` with `loadOptions` hitting `GET …/issues?q=` (label = `ENG-12 Title`, hint = status name), excluding the current issue.

| Where | Behaviour |
|---|---|
| Issue page breadcrumb | `Project › ENG-1 › ENG-5` when the issue has a parent (parent links to it). |
| Issue page properties | New "Parent" picker (search limited to this project via `project=`), clearable ("No parent"); server message shown under the panel. Read-only text when `can_edit` is false. Hidden when the issue has sub-issues (it can't become one). |
| Issue page, under the description | **Sub-issues** section (only for top-level issues): rows like the project list (status icon, identifier, title, status, assignee avatar), each linking to the sub-issue; "Add sub-issue" (needs `issues.create`, project not archived) opens the existing New issue dialog with the parent preset and shown as a read-only line "Sub-issue of ENG-1". Empty: "No sub-issues." |
| Issue page, under sub-issues | **Relations** section: "Blocks" and "Blocked by" groups of rows (identifier, title, status) each with a remove × (`IconButton`, `aria-label="Remove link to ENG-7"`) when `can_edit`; "Add link" (when `can_edit`): type `SmartAutocomplete` (Blocks / Blocked by) + issue picker (whole workspace) + "Add" button; errors under it. Empty groups hidden; nothing at all → "No links." |
| Project issue list | Sub-issues show a muted "↳ ENG-1" before the title. Order unchanged. |
| New issue dialog | Accepts an optional preset parent (see above); nothing else changes. |

## Verification

- Backend: above.
- Frontend: `npm run build`.
- Integration (main session, headless Chrome): add sub-issue ENG-2 under ENG-1 from the issue page; breadcrumb and list "↳"; set/clear parent from properties; nesting error shows; link ENG-1 blocks ENG-3 → ENG-3 shows "Blocked by ENG-1"; duplicate link 409; remove link; delete ENG-1 → ENG-2 becomes top-level; read-only member sees relations without edit controls.
