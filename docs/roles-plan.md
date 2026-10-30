# Roles and custom permissions plan

Scope: each workspace has roles. A role is a name plus a set of permissions. Every member has exactly one role. Two built-in roles are created with every workspace: **Admin** (every permission, can't be edited or deleted) and **Member** (the default for invites; its permissions can be edited, it can't be renamed or deleted). People with "Manage roles" create, edit and delete custom roles in **Settings → Roles**. People with "Manage members" change a member's role and remove members in **Settings → Members**. Invites carry a role.

Permissions for projects, issues and comments are stored and editable now but only take effect when those features ship (Phases 2 and 4); the UI says so.

Out of scope: per-project roles, permission overrides per member, guest/external access, audit log of role changes.

## Permissions

The catalog lives in Go (`internal/workspace/permissions.go`) and is served to the frontend; the frontend never hardcodes keys except to gate UI. Order below is display order.

| Key | Group | Label | Enforced by |
|---|---|---|---|
| `workspace.edit` | Workspace | Edit workspace name and URL | `POST /api/workspaces/{slug}` |
| `workspace.delete` | Workspace | Delete the workspace | `DELETE /api/workspaces/{slug}` |
| `members.invite` | Members | Invite people, resend and cancel invites | invite endpoints |
| `members.manage` | Members | Change members' roles and remove members | member endpoints |
| `roles.manage` | Members | Create, edit and delete roles | role endpoints |
| `projects.manage` | Projects | Create, edit and archive projects | Phase 2 |
| `issues.create` | Issues | Create issues | Phase 2 |
| `issues.edit_any` | Issues | Edit and delete anyone's issues | Phase 2 |
| `comments.create` | Comments | Comment on issues | Phase 4 |
| `comments.manage` | Comments | Edit and delete anyone's comments | Phase 4 |

Member role defaults: `issues.create`, `comments.create`.

Rules:
- **No escalation:** you can only assign a role (to a member or an invite) whose permissions are all ones you have yourself, and you can only create or edit a role to permissions you have yourself. Otherwise `403 {"error":"You can't grant permissions you don't have."}`. Admins have everything, so this never blocks them.
- **No reaching up:** changing someone's role or removing them also needs every permission of their *current* role (`403 You don't have permission to do that.`), so a non-admin with "Manage members" can't demote or remove an admin. Leaving yourself is always allowed (subject to the last-admin rule). The UI hides role selects and Remove on such rows.
- **At least one admin:** any change that would leave the workspace with no member on the Admin role (changing the last admin's role, removing them, the last admin leaving) → `409 {"error":"A workspace needs at least one admin."}`.
- Viewing (workspace, members list, roles list) needs only membership.
- The Admin role always has the full current catalog: compute it, don't store it (`builtin = 'admin'` → all keys), so new permissions apply to admins automatically.

## Schema

Migration `backend/user/internal/db/migrations/007_roles.sql`:

```sql
create table workspace_roles (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null,
  builtin      text check (builtin in ('admin', 'member')), -- null for custom roles
  permissions  text[] not null default '{}',               -- ignored for builtin = 'admin'
  created_at   timestamptz not null default now(),
  unique (workspace_id, builtin)
);
create unique index on workspace_roles (workspace_id, lower(name));

-- Backfill: Admin + Member roles for every existing workspace, point memberships at them, drop the text column.
-- workspace_members.role_id uuid not null references workspace_roles (on delete restrict)
-- workspace_invites.role_id uuid references workspace_roles on delete set null  (null = the Member role)
```

The backend agent writes the full backfill in the same migration. Creating a workspace (`create`, `CreateDefault`) also creates both built-in roles in the same transaction and makes the creator Admin.

Deleting a custom role moves its members and invites to the Member role in the same transaction.

## API contract

FormData in, JSON out. Signed out → 401 as today. Workspace missing or caller not a member → `404 {"error":"Workspace not found."}`. A missing permission → `403 {"error":"You don't have permission to do that."}` (one message for all permission failures, replaces "Only admins can …" messages).

Shapes:
- Role ref: `{"id": "...", "name": "Admin"}`
- Workspace (changed): `{"id", "name", "slug", "role": roleRef, "permissions": ["workspace.edit", …]}` — `permissions` is the caller's effective set. **Breaking:** `role` was a string.
- Member (changed): `{"id", "name", "email", "role": roleRef}`
- Invite (changed): `{"id", "email", "invited_by", "expires_at", "role": roleRef}`
- Role: `{"id", "name", "builtin": "admin" | "member" | null, "permissions": [...], "member_count": 3}` (Admin's `permissions` = full catalog)
- Catalog entry: `{"key": "workspace.edit", "group": "Workspace", "label": "Edit workspace name and URL", "active": true}` (`active` false for keys whose feature hasn't shipped)

| Method & path | Needs | Fields | Success | Failures |
|---|---|---|---|---|
| `GET /api/permissions` | signed in | — | `200 [catalog entry…]` in display order | 401 |
| `GET /api/workspaces/{slug}/roles` | member | — | `200 [role…]`: Admin, Member, then custom by name | 404 |
| `POST /api/workspaces/{slug}/roles` | `roles.manage` | `name`, `permissions` (repeat the field once per key; none = no permissions) | `201 role` | 400 `{"errors":{"name": …}}` / `{"errors":{"permissions":"Unknown permission."}}`; 403; 409 `{"errors":{"name":"A role with that name already exists."}}` |
| `POST /api/workspaces/{slug}/roles/{id}` | `roles.manage` | `name`, `permissions` | `200 role`. Member role: `name` ignored, permissions saved | 400; 403 (also `{"error":"The Admin role can't be changed."}` for Admin); 404 `{"error":"Role not found."}`; 409 name taken |
| `DELETE /api/workspaces/{slug}/roles/{id}` | `roles.manage` | — | `204`; its members and invites move to Member | 403 (also `{"error":"Built-in roles can't be deleted."}`); 404 |
| `POST /api/workspaces/{slug}/members/{userId}` | `members.manage` | `role_id` | `200 member` | 400 `{"errors":{"role_id":"Choose a role."}}` (missing or not this workspace's); 403 (incl. escalation); 404 `{"error":"Member not found."}`; 409 last admin |
| `DELETE /api/workspaces/{slug}/members/{userId}` | `members.manage`, or the caller themself (leave) | — | `204` | 403; 404 member not found; 409 last admin |
| `POST /api/workspaces/{slug}/invites` (changed) | `members.invite` | `email`, `role_id` (optional; default Member) | as before; invite JSON now has `role` | as before, plus 400 `role_id` "Choose a role.", 403 escalation |

Role name validation (Go and `@tether/shared/validate` `roleName`): trimmed, required → `Role name is required.`; ≤ 40 chars → `Role name must be 40 characters or fewer.`

Everything else from the workspaces and invites plans stays, except their admin checks become permission checks: workspace update → `workspace.edit`, delete → `workspace.delete`, invites (create/resend/cancel and seeing the `invites` list) → `members.invite`. Accepting an invite gives the invite's role (Member if null).

## Backend (`backend/user/internal/workspace/`)

- `permissions.go`: the catalog (ordered slice of key/group/label/active), `allPermissions()`, a `has(perms, key)` helper, and the subset check.
- New handlers in `roles.go` (roles) and `members.go` (member role change / remove), or extend `controller.go`/`invite.go` if smaller; every query in `model.go`.
- Replace the existing `role(ctx, userID, slug)` / `membership` admin checks with one query returning the caller's workspace id, role ref, builtin and effective permissions; reuse it in every handler.
- Tests (`workspace_test.go`): subset/escalation check, Admin effective permissions = catalog, role name validation.
- Verify with `go vet`, `go test`, then curl every row of the table, including escalation 403, last-admin 409 for each of change/remove/leave, role delete moving members to Member, migration backfill on the existing dev data (existing workspaces get Admin/Member roles and keep who is admin).

## Frontend (user app)

- `api.ts`: update `Workspace`, `Member`, `Invite` types; add `Role`, `Permission`, `RoleRef`. Add `can(workspace, key)` helper.
- Replace every `role === 'admin'` check with `can(...)`: settings form (`workspace.edit`), delete section (`workspace.delete`), invite forms on Workspace and Members pages and the pending list (`members.invite`).
- Shared `Select` component in `frontend/shared/src/components/select/` (native `<select>` styled like `Input`, with `label`/`error` props like `Input`). Ask nothing, it's in scope.
- **Sidebar:** Settings children: Profile, Workspace, Members, Roles.
- **Members page:** role column becomes a `Select` of the workspace's roles for people with `members.manage` (saves on change, shows error inline per row); otherwise the role name as text. A "Remove" ghost button per member (with `members.manage`) and "Leave workspace" on your own row (everyone), both confirmed with the shared `Dialog`. Leaving goes to `/`. The invite form gains a role `Select` (default Member). Pending invites show the role name.
- **Roles page** `/$workspace/settings/roles` (`routes/_app/$workspace/_main/settings/roles/index.tsx`): list of roles: name, "Built-in" tag for Admin/Member, member count, and a short summary ("All permissions" for Admin, else "N permissions"). With `roles.manage`: "Create role" button → `/settings/roles/new`; each row links to `/settings/roles/$roleId`.
- **Role editor** `/settings/roles/new` and `/settings/roles/$roleId` (one shared form component in `components/roles/`): role name input, then the catalog grouped by `group`, one shared `Checkbox` per permission with its label; inactive ones get a muted "Available when <group> ships" caption. Admin role: read-only with a note "Admins always have every permission." Member role: name read-only. Save → back to the list. Edit page has "Delete role" (custom only) with a `Dialog` confirm: "Its N members will move to Member." Without `roles.manage`: read-only view.
- Loaders: roles page `GET …/roles`; editor `GET /api/permissions` + `GET …/roles` (pick by id; not found → `notFound()`); members page also loads roles.

Sentence case everywhere. Forms `noValidate`, shared validators, server `errors` on fields, pending labels.

## Verification

- Backend: listed above.
- Frontend: `npm run build`.
- Integration (main session, headless Chrome): admin creates "Triage" role with `members.invite`; changes a member to Triage; that member now sees the invite form but no role selects or Roles create button; tries nothing they can't; admin deletes Triage → member back to Member; last-admin guard shows its message; leave workspace works.
