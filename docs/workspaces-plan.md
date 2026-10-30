# Workspaces plan

Scope: a user creates one or more workspaces (name + URL slug), lands in the last one they opened, switches between them from the navbar, and an admin can rename a workspace or change its URL. Each workspace member has a role (`admin` or `member`); the creator is admin.

Update (shipped follow-up):
- **Default workspace:** sign-up and a first Google sign-in create "{First name}'s workspace" at `/{first-name}` (random 4-character suffix when taken, `workspace` when the name gives no valid URL). `auth.New` takes an `onUserCreated` hook wired to `workspace.CreateDefault`; a failure is logged, and `/` then shows a create dialog that can't be dismissed.
- **Create in a dialog:** "Create workspace" opens the shared `Dialog` from the navbar switcher; `/new-workspace` is gone.
- **App sidebar:** workspace pages share a left sidebar (`_main` layout): Home, and Settings, which expands to Profile (`/settings/profile`) and Workspace (`/settings/workspace`, name + URL, and for admins "Delete workspace" confirmed by typing the name). `/settings` redirects to Workspace. Deleting goes to `/`, i.e. the next workspace or the create dialog.

Out of scope here (next features): inviting people, changing roles, removing members, deleting a workspace, billing.

## Schema

Migration `backend/user/internal/db/migrations/005_workspaces.sql`:

```sql
create table workspaces (
  id         uuid primary key default uuidv7(),
  name       text not null,
  slug       text not null unique,
  created_by uuid references users on delete set null,
  created_at timestamptz not null default now()
);

create table workspace_members (
  workspace_id   uuid not null references workspaces on delete cascade,
  user_id        uuid not null references users on delete cascade,
  role           text not null check (role in ('admin', 'member')),
  last_opened_at timestamptz not null default now(),
  created_at     timestamptz not null default now(),
  primary key (workspace_id, user_id)
);
create index on workspace_members (user_id, last_opened_at desc);
```

No new `system_config` keys.

## Validation (same rules and messages on both sides)

Add to `frontend/shared/src/validate/validate.ts` and mirror in Go:

| Field | Rule | Message |
|---|---|---|
| `name` | trimmed, required | `Workspace name is required.` |
| `name` | ≤ 50 characters | `Workspace name must be 50 characters or fewer.` |
| `slug` | required | `Workspace URL is required.` |
| `slug` | 3 to 32 characters | `Workspace URL must be 3 to 32 characters.` |
| `slug` | `^[a-z0-9]+(-[a-z0-9]+)*$` | `Use lowercase letters, numbers and single hyphens.` |
| `slug` | not reserved | `That URL is reserved.` |

Reserved slugs (top-level app paths and likely future ones): `api, admin, app, signin, signup, signout, forgot-password, reset-password, new-workspace, settings, account, invite, invites, join, help, support, docs, status, about, pricing, blog, www, mail, static, assets`.

Frontend also exports `slugify(name)`: lowercase, non `[a-z0-9]` runs → single `-`, trim hyphens, cut to 32. Used to prefill the URL field from the name until the user edits the URL.

## API contract

All endpoints need a signed-in user (access token cookie, same as `/api/me`); without one → `401 {"error":"Not signed in."}` so the frontend's refresh-and-retry works. Request bodies are FormData. JSON responses.

Workspace JSON: `{"id": "...", "name": "Acme", "slug": "acme", "role": "admin"}` (`role` is the caller's role).

| Method & path | Fields | Success | Failures |
|---|---|---|---|
| `GET /api/workspaces` | — | `200 [workspace, …]` caller's workspaces, most recently opened first | 401 |
| `POST /api/workspaces` | `name`, `slug` | `201 workspace`; caller becomes admin | 400 field errors; 409 `{"errors":{"slug":"That URL is taken."}}` |
| `GET /api/workspaces/{slug}` | — | `200 workspace`; sets the caller's `last_opened_at` to now | 404 `{"error":"Workspace not found."}` when it doesn't exist **or** the caller isn't a member (don't leak existence) |
| `POST /api/workspaces/{slug}` | `name`, `slug` (both required; send the current value to keep it) | `200 workspace` (new slug if changed) | 400; 403 `{"error":"Only admins can change workspace settings."}`; 404; 409 slug taken |
| `DELETE /api/workspaces/{slug}` | — | `204`; memberships go with it | 403 member; 404 missing or not a member |

Slugs are compared and stored lowercase.

## Backend (`backend/user`)

- New package `internal/workspace/` with `routes.go`, `controller.go`, `model.go`, `validate.go` (+ `_test.go` for validation), per CLAUDE.md.
- Authentication: export from `internal/auth` a method `func (h *Handler) Authenticate(r *http.Request) (string, error)` (rename the existing unexported `authenticate`; returns the user ID or "" when signed out). `workspace.New(pool, authH.Authenticate)` takes it as a `func(*http.Request) (string, error)`; no interface.
- Wire in `cmd/user/main.go`.
- Unique-violation on slug (create or update) → 409.

## Frontend (user app)

Routes (all under the signed-in `_app` layout, which already has the navbar):

| Route | File | Behaviour |
|---|---|---|
| `/` | `routes/_app/index.tsx` | `beforeLoad`: `GET /api/workspaces`; none → redirect `/new-workspace`; else redirect `/$workspace` of the first (most recent). No component. |
| `/new-workspace` | `routes/_app/new-workspace.tsx` | Create form: "Workspace name", "Workspace URL" with a `tether.app/` prefix shown before the input, prefilled by `slugify(name)` until edited. Submit → `POST /api/workspaces` → `router.invalidate()` → navigate to `/$workspace`. Title "Create a workspace"; if the user has none, subtitle explains it's needed to start. |
| `/$workspace` | `routes/_app/$workspace.tsx` | Layout. `loader`: `GET /api/workspaces/{slug}`; 404 → `throw notFound()`. Exposes the workspace to children. Renders `<Outlet />`. |
| `/$workspace/` | `routes/_app/$workspace/index.tsx` | Home: "Hi {user name}" and the workspace name. Placeholder for projects. |
| `/$workspace/settings` | `routes/_app/$workspace/settings.tsx` | "Workspace settings" → general: name + URL form. Admins can save (`POST /api/workspaces/{slug}`), then navigate to the new slug if it changed and show "Saved."; members see the values read-only with a note that only admins can change them. |

`_app.tsx` loader also fetches `GET /api/workspaces` so the navbar has the list.

Navbar (`components/app/navbar.tsx`): next to the logo, a workspace switcher button showing the current workspace name (from the `$workspace` param; hidden when not inside a workspace). It opens the shared `Popover` (`bottom-start`) listing the user's workspaces (current one marked), then "Workspace settings" (to `/$workspace/settings`) and "Create workspace" (to `/new-workspace`).

Forms follow existing patterns: `noValidate`, shared validators first, then the API, server `errors` onto fields, `error` as a form-level message, pending labels. Sentence case everywhere.

## Verification

- Backend: `go vet`, `go test`, curl every endpoint including 401, 400 (each rule), 409 on create and on rename, 404 for non-member, 403 for member (insert a member row with psql to test), last-opened ordering.
- Frontend: `npm run build`; click-through: new user → `/` → `/new-workspace` → create → `/acme`; switcher lists and switches; create second; settings rename + slug change redirects; non-member URL → not found.
