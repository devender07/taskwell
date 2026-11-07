# Workspace audit log plan

Workspace admins see who did what to the workspace itself: members, roles, invites, projects, integrations,
webhooks, permanent deletes. The admin app's `admin_audit` stays the platform admins' log; this one belongs to each
workspace.

Out of scope: issue-level history (the issue activity log covers it), personal API tokens (they belong to a user, not
a workspace), exporting the log, alerts.

## Permission

`audit.view` ("See the audit log"), group Workspace (guests never), in `workspace/permissions.go` (already added as
`PermAuditView`). Admin has it; custom roles opt in.

## Schema: migration `029_workspace_audit.sql`

```sql
create table workspace_audit (
    id uuid default uuidv7() primary key,
    workspace_id uuid not null references workspaces (id) on delete cascade,
    actor_id uuid references users (id) on delete set null,
    actor_name text not null default '',   -- copied, so the entry reads right after the user is gone
    actor_email text not null default '',
    action text not null,
    target text not null default '',
    detail jsonb not null default '{}',
    ip text not null default '',
    at timestamptz default now() not null
);
create index workspace_audit_workspace_idx on workspace_audit (workspace_id, id desc);
```

Retention: the sweep deletes entries older than 1 year (an entry in `sweep/model.go`'s retention list).

## Recording

New package `internal/wsaudit` with one function used by every package:

`Record(ctx, q Execer, workspaceID, actorID, action, target string, detail map[string]any, ip string) error`

It inserts in the caller's transaction (so an entry commits with its change and never without it) and copies the
actor's name and email. `ip` is `web.ClientIP(r)`. Handlers pass them down to the model call that owns the
transaction.

Actions (exact strings; `target` and `detail` as listed):

| Action | Target | Detail |
| --- | --- | --- |
| `workspace.renamed` | new name | `{from, to}` |
| `workspace.url_changed` | new slug | `{from, to}` |
| `member.invited` | email | `{role, guest}` |
| `invite.resent` | email | |
| `invite.canceled` | email | |
| `member.joined` | email (actor is the joiner) | `{role}` |
| `member.role_changed` | email | `{from, to}` |
| `member.removed` | email | |
| `member.left` | email (actor is the leaver) | |
| `role.created` | role name | `{permissions}` |
| `role.updated` | role name | `{renamed_from?, added, removed}` |
| `role.deleted` | role name | |
| `project.created` | key | `{name}` |
| `project.deleted` | key | `{name}` |
| `project.archived` / `project.unarchived` | key | |
| `project.made_private` / `project.made_public` | key | |
| `project.member_added` / `project.member_removed` | key | `{email}` |
| `webhook.created` / `webhook.deleted` / `webhook.secret_rolled` | url | |
| `integration.connected` / `integration.disconnected` | provider (`github`, `gitlab`) | |
| `trash.emptied` | | `{count}` |
| `issue.purged` | identifier | `{title}` |

Find each change's handler and model (workspace, project, webhook, gitlink, issue/trash.go) and record there.

## API contract

`GET /api/workspaces/{slug}/audit?before=<id>&category=<members|roles|projects|workspace|integrations|trash>`

- `200` `{"entries": [{"id", "at", "actor": {"id", "name", "email"} | null, "action", "target", "detail", "ip"}],
  "more": bool}`: 50 per page, newest first; `before` continues after the last id shown; `category` filters by the
  action's prefix (`members` → `member.` and `invite.`, `roles` → `role.`, `projects` → `project.`, `workspace` →
  `workspace.`, `integrations` → `webhook.` and `integration.`, `trash` → `trash.` and `issue.`). `actor` is null for
  a deleted user (name/email then come from the copies: return `{"id": null, "name", "email"}` instead when the
  copies are set).
- `400` `{"error": "Invalid cursor."}` for a `before` that isn't a uuid; an unknown category is ignored.
- `403` `{"error": "You don't have permission to see the audit log."}`
- `404` `{"error": "Workspace not found."}`

## Frontend

Settings → **Audit log** (`/$workspace/settings/audit`), nav link with an icon from the shared set, shown with
`audit.view`.

- Page title "Audit log", subtitle "Changes to members, roles, projects and integrations. Entries can't be edited or
  deleted."
- Category filter (All, Members, Roles, Projects, Workspace, Integrations, Trash) in the URL (`validateSearch`), with
  `before` for paging ("Newest" / "Older" links, like the admin app's audit page).
- Table: Time, Who, Action (a sentence-case label per action: "Invited a member", "Changed a member's role", …),
  Target, Details (a short summary of `detail`, e.g. "Member → Admin"), IP.
- `src/api.ts`: types and a path helper.

## Verification

- `go vet`, `go test`: Record in a transaction that rolls back leaves nothing; one test per area that a real change
  writes its entry (invite, role change, remove member, create/update/delete role, delete project, webhook create,
  empty trash); the list endpoint's paging, category filter and 403.
- curl against a running API.
- Frontend: `npm run build`.
