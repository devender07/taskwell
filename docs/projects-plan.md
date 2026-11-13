# Projects plan

Scope: a workspace has projects. A project has a name, a short **key** (`ENG`) that issue IDs will use later (`ENG-142`), and a description. Projects are archived, not deleted. Everyone in the workspace can see projects (active and archived); people with the `projects.manage` permission create, edit, archive and unarchive them. This turns `projects.manage` on (`active: true` in the catalog).

Out of scope: issues (next feature), project members/leads, project icons or colors, deleting projects, markdown rendering (description is stored as markdown text and shown as plain text with line breaks; rendering comes with issues, and needs a dependency decision then).

## Schema

Migration `backend/user/internal/db/migrations/008_projects.sql`:

```sql
create table projects (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null,
  key          text not null,
  description  text not null default '',
  archived_at  timestamptz,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now(),
  unique (workspace_id, key)
);
```

Keys are stored uppercase. A key stays reserved while its project is archived.

## Validation (same rules and messages on both sides)

Add to `frontend/shared/src/validate/validate.ts` (+ check) and mirror in Go:

| Field | Rule | Message |
|---|---|---|
| `name` | trimmed, required | `Project name is required.` |
| `name` | ≤ 60 characters | `Project name must be 60 characters or fewer.` |
| `key` | trimmed, uppercased, required | `Project key is required.` |
| `key` | `^[A-Z][A-Z0-9]{1,4}$` | `Use 2 to 5 letters or numbers, starting with a letter.` |
| `description` | ≤ 10000 characters | `Description must be 10,000 characters or fewer.` |

Frontend also exports `projectKey(name)`: letters/digits only, uppercased; several words → their initials (up to 5), one word → its first 3 characters; must start with a letter (drop leading digits); `''` if nothing usable. Used to prefill the key from the name until the user edits the key.

## API contract

FormData in, JSON out. Signed out → 401. Workspace missing or caller not a member → `404 {"error":"Workspace not found."}`. Missing `projects.manage` → `403 {"error":"You don't have permission to do that."}`. `{key}` in paths is matched case-insensitively.

Project JSON: `{"id": "...", "name": "Engineering", "key": "ENG", "description": "...", "archived": false, "created_at": "2026-10-30T10:00:00Z"}` (UTC).

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/workspaces/{slug}/projects` | member | query `archived=true` for archived ones | `200 [project…]`: active projects by name (or archived ones, most recently archived first) | 404 |
| `POST /api/workspaces/{slug}/projects` | `projects.manage` | `name`, `key`, `description` (optional) | `201 project` | 400 field errors; 403; 409 `{"errors":{"key":"That key is taken."}}` |
| `GET /api/workspaces/{slug}/projects/{key}` | member | — | `200 project` | 404 `{"error":"Project not found."}` |
| `POST /api/workspaces/{slug}/projects/{key}` | `projects.manage` | `name`, `key`, `description` (all sent; send current values to keep) | `200 project` (new key if changed) | 400; 403; 404 project; 409 key taken |
| `POST /api/workspaces/{slug}/projects/{key}/archive` | `projects.manage` | — | `200 project` (`archived: true`); archiving an archived project is a no-op 200 | 403; 404 |
| `POST /api/workspaces/{slug}/projects/{key}/unarchive` | `projects.manage` | — | `200 project` | 403; 404 |

## Backend (`backend/user`)

- New package `internal/project/` (`routes.go`, `controller.go`, `model.go`, `validate.go`, `project_test.go` for validation), per CLAUDE.md layout.
- It needs the caller's workspace and permissions. Export from `internal/workspace` a method `func (h *Handler) Member(ctx context.Context, userID, slug string) (*Workspace, error)` (nil when the workspace is missing or the user isn't a member; reuses the existing membership query) and `func Has(perms []string, key string) bool` (rename/alias the existing `has`). `project.New(pool, authenticate, wsH.Member)` takes the method as a func value; no interface.
- `internal/workspace/permissions.go`: `projects.manage` → active, and give it a `permProjectsManage`-style exported constant only if the project package needs the string (`PermProjectsManage`).
- Unique violation on `(workspace_id, key)` → 409.
- Verify: `go vet`, `go test`, curl every row incl. 401, 403 (member without the permission), 404 non-member, 409 on create and on key change, archive/unarchive listing, case-insensitive key lookup.

## Frontend (user app)

`api.ts`: `Project` type.

Shared: add `Textarea` to `frontend/shared/src/components/textarea/` styled like `Input` (`label`, `error` props; min-height 96px; vertical resize), exported as `@taskwell/shared/textarea`.

Sidebar (`components/app/sidebar.tsx`): add **Projects** (folder icon, same style as Home) between Home and Settings, linking to `/$workspace/projects`; active on any `/projects` route.

| Route | File | Behaviour |
|---|---|---|
| `/$workspace/projects` | `routes/_app/$workspace/_main/projects/index.tsx` | `validateSearch` `{ archived?: true }`. Loader `GET …/projects` (`?archived=true` when set; use `loaderDeps`). Title "Projects". Tabs (shared `Tabs`) "Active" / "Archived" switch the search param. List rows: key (mono, secondary), name, one-line description excerpt; row links to the project. Empty states: "No projects yet." (+ "Create project" button when allowed) / "No archived projects." With `projects.manage`: "Create project" button top right opens a `Dialog` with the create form (name, key prefilled by `projectKey`, description `Textarea`); on success navigate to the new project. |
| `/$workspace/projects/$key` | `routes/_app/$workspace/_main/projects/$key.tsx` | Loader `GET …/projects/{key}`; 404 → `notFound()`. Header: key tag + name, "Archived" tag if archived. Description as plain text (`white-space: pre-wrap`), or "No description." muted. Placeholder text "Issues will show up here." With `projects.manage`: "Edit" button (Dialog with the same form, prefilled; key change navigates to the new key) and "Archive project" / "Unarchive project" (archive confirmed in a `Dialog`: "Archived projects are hidden from the list. You can unarchive them later."). |

The create and edit forms share one component in `components/projects/project-form.tsx`. Forms follow existing patterns: `noValidate`, shared validators first, server `errors` onto fields, `FormError`, pending labels. Sentence case.

## Verification

- Backend: listed above.
- Frontend: `npm run build`.
- Integration (main session, headless Chrome): admin creates "Engineering" (key prefilled `ENG`), duplicate key 409 shown on field, edit name + key → URL follows, archive → gone from Active, in Archived tab, unarchive; a Member (no `projects.manage`) sees projects but no create/edit/archive buttons; giving Member `projects.manage` in Roles shows them.
