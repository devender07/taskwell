# Guests plan

Scope: invite people outside the team (clients, contractors) as **guests**. A guest sees only the projects they're added to, and nothing workspace-wide.

Out of scope:
- Per-guest permission tweaks. The Guest role's permissions are editable like any role, except it can never gain workspace-level ones.
- Guest seat limits.

## Model

- A new built-in role, `guest`, created for every workspace (backfilled by the migration) alongside admin and member.
- Default permissions: `issues.create` and `comments.create`. Its editor hides the Workspace, Members and Analytics permission groups, and the API rejects them for this role ("Guests can't have workspace permissions.").
- Visibility comes from `project_members`, the same table private projects use. A guest sees a project only when they are a project member, whether or not it's private.

`018_guests.sql`:
- Allow `builtin = 'guest'` and insert the role for each workspace.
- Replace `can_see_project`:

```sql
select case when is_guest(p.workspace_id, viewer)
         then exists (select 1 from project_members m where m.project_id = p.id and m.user_id = viewer)
         else <today's rule> end
from projects p where p.id = project
```

  Add the helper `is_guest(ws, user)`.

## What guests can't see or do

Enforce this in the API with a 403 or 404 ("Guests can't see this."); hiding it in the UI isn't enough:

- the workspace chat and group chats they're not in (direct messages with members are allowed)
- the feedback inbox
- the team and member analytics pages
- the roadmap (only their projects show), which falls out of `can_see_project`
- the member list: limited to people who share a project with them, plus admins
- workspace settings, integrations, webhooks, roles, AI testing settings, and import
- creating projects

Search, My issues, Home, activity and notifications already filter by `can_see_project`. Check each one and add a test.

## API contract

- Invites (existing endpoint) accept the guest role id like any role, plus a new repeated FormData field `project_keys` (required when the role is guest: "Choose at least one project."). Accepting the invite adds the guest to those projects' `project_members`.
- `POST /api/workspaces/{slug}/projects/{key}/members` (the existing private-project member endpoint) now works on any project when the person added is a guest. Removing a guest from their last project is allowed: they stay in the workspace and see nothing.
- Member JSON gains `"guest": bool`. The `/api/workspaces/{slug}` workspace JSON gains `"guest": bool` for the caller.
- The workspace switcher and member lists label guests "Guest".

## Frontend

- Invite dialog: choosing the Guest role shows a required multi-select "Projects".
- **Members:** a "Guest" tag on guests. In a guest's row menu, "Projects…" edits which projects they're in.
- **Project page members:** the add-people picker also offers guests on public projects. For a public project the dialog title is "Guests on this project".
- **When the caller is a guest**, from `workspace.guest`:
  - The sidebar hides Inbox (or shows only DMs), Feedback, Roadmap and Team.
  - Settings shows only Profile, Notifications and API tokens.
  - The routes behind those links redirect to the workspace home.
- **Home** works as is, filtered by the API.

## Verification

- `go test`, covering every endpoint in the list above as a guest (expect 403/404), visibility through `can_see_project` (public project not added → hidden), the invite with projects, and the role editor rejecting workspace permissions.
- `npm run build`.
- Integration: invite a guest to one project, accept as them (second account via Mailpit), and check what they can and can't see.
