# Private projects plan

Scope: a project can be **private**. Only its members and workspace admins see it, its issues and anything about
them. Everyone else in the workspace can't find it anywhere: not in lists, search, My issues, Home, notifications,
cycles, tests or live updates. Chat is untouched, so everyone in the workspace can still message anyone.

Example: a 20-person workspace. The "Leadership" project is private with 4 members; the other 16 never see it, and
all 20 chat freely.

## Rules

- **Who sees a private project:** its members, plus workspace admins (the built-in Admin role), who see every project.
  A public project is seen by every workspace member, as today.
- **Hidden means not found:** anything inside a project someone can't see answers exactly as if it didn't exist:
  `404 {"error":"Project not found."}` / `{"error":"Issue not found."}`, and it's left out of every list and count.
- **Who manages it:** anyone with `projects.manage` who can see the project can make it private or public and add or
  remove members. Making a project private adds the person doing it as a member. Creating a private project adds the
  creator.
- **Assignees:** on a private project's issues, only people who can see the project can be assigned. People who were
  assigned before it went private keep the assignment but can't see the issue (known limit: add them as members).
- **Mentions and watching:** mentioning someone who can't see the issue doesn't notify them or make them a watcher.
  Watchers who can't see an issue get no notifications or emails about it.
- **Sub-issues** are already same-project only. **Relations** between a visible and a hidden issue show only on the
  side the viewer can see.
- Platform admin app: unchanged (operators, not workspace members).

## Schema

Migration `backend/user/internal/db/migrations/040_private_projects.sql`:

```sql
alter table projects add column private boolean not null default false;

create table project_members (
  project_id uuid not null references projects on delete cascade,
  user_id    uuid not null references users on delete cascade,
  added_by   uuid references users on delete set null,
  created_at timestamptz not null default now(),
  primary key (project_id, user_id)
);
create index on project_members (user_id);

-- The one visibility rule; every query that reads projects, issues or anything under them filters with it.
create function can_see_project(project uuid, viewer uuid) returns boolean language sql stable as $$
  select not p.private
      or exists (select 1 from project_members m where m.project_id = p.id and m.user_id = viewer)
      or exists (select 1 from workspace_members wm join workspace_roles r on r.id = wm.role_id
                 where wm.workspace_id = p.workspace_id and wm.user_id = viewer and r.builtin = 'admin')
    from projects p where p.id = project
$$;
```

Removing someone from the workspace removes their project memberships (delete in `workspace.removeMember`).

## API contract

FormData in, JSON out. All existing endpoints keep their shapes; they only stop returning what the caller can't see.

Project JSON gains `"private": bool` and `"members": [{"id","name"}]` (the explicit members, by name; `[]` for a
public project).

| Method & path | Who | Fields | Success | Failures |
|---|---|---|---|---|
| `POST /api/workspaces/{slug}/projects` | `projects.manage` | as today, plus `private` (`"true"`/`"false"`, default false) and `member_ids` (repeated, workspace members; you're added automatically) | `201 project` | as today; 400 `{"errors":{"member_ids":"Someone you picked isn't in this workspace."}}` |
| `POST /api/workspaces/{slug}/projects/{key}` | `projects.manage`, can see it | as today, plus `private` (optional; missing = unchanged) | `200 project`. Going private adds you as a member | as today |
| `GET /api/workspaces/{slug}/projects/{key}/people` | can see it | — | `200 [member…]` (the `/members` Member JSON): everyone who can see the project, for assignee and mention pickers. Public: the whole workspace | 404 |
| `POST /api/workspaces/{slug}/projects/{key}/members` | `projects.manage`, can see it | `user_ids` (repeated) | `200 project` | 400 `{"errors":{"user_ids":"Someone you picked isn't in this workspace."}}`; 403; 404; 409 `{"error":"Only private projects have members."}` |
| `DELETE /api/workspaces/{slug}/projects/{key}/members/{userId}` | `projects.manage`, can see it | — | `200 project` (removing yourself is allowed; you then lose access unless you're an admin) | 403; 404; 409 as above |
| `POST /api/workspaces/{slug}/issues/{identifier}` and issue create | as today | `assignee_id` | as today | 400 `{"errors":{"assignee_id":"They can't see this project."}}` |

Filtered by `can_see_project` (hidden = 404 or left out):
- projects: list, get, project-stats, archive/unarchive, cycles (list, get, create, update, delete, rollover)
- issues: project list, search (`/issues`, `/search`), My issues, activity, get, update, delete, links (and the
  related issues inside a visible issue), watch, timeline, comments (create, update, delete)
- notifications: list, unread count, emails and digests
- tests: test cases, test runs (per issue and by id, including `/live` and cancel), results, browser sessions,
  playwright spec
- pull requests shown on an issue (via the issue)
- realtime: `collab.join` and presence on a hidden issue are refused; events about a private project or its issues
  go only to the people who can see it (one `ToUser` audience each), never `ToWorkspace`

ponytail: files uploaded into a private issue stay workspace-scoped by their unguessable id; tighten if links leak.

## Backend (`backend/user`)

- Migration 040 as above.
- `project`: `private`/`members` on Project JSON, the create/update fields, the `people` and members endpoints,
  `can_see_project` in every query.
- `issue`, `cycle`, `notify`, `aitest`, `gitlink`, `realtime`: add the filter everywhere listed above; assignee check.
- Tests (one per package touched, against the test DB): a workspace with an admin, a project member and an outsider,
  one private project. The outsider gets 404 on the project, its issues, cycles, test runs and comments, and doesn't
  see them in lists, search, My issues, activity, project-stats, notifications or relations; the member and the
  admin see all of it. Assigning the outsider fails. Mentioning the outsider doesn't notify them. Making a public
  project private and back works; removing a member hides it from them.
- Verify: `go vet`, `go test ./user/...`.

## Frontend (user app)

- Icons: `LockIcon` in `@taskwell/shared/icons`.
- Projects list: a lock icon before a private project's name.
- Create project dialog: a "Private" checkbox ("Only members and admins can see it"); when checked, a member picker
  (workspace members, multi-select) under it.
- Project page: lock icon and a "Private" tag by the name. With `projects.manage`: the Edit dialog has the Private
  checkbox; a "Members" button (private projects only) opens a dialog listing members with Remove, and a picker to
  add people.
- Issue pages and the create-issue form in a private project: assignee and mention pickers use
  `GET …/projects/{key}/people` instead of the workspace members.
- Verify: `npm run build`.

## Verification (integration)

Run both together; sign in as the admin, a member and an outsider: create a private project with the member, add an
issue, assign and mention; check the outsider sees none of it anywhere (projects, search, Ctrl+K, My issues, Home,
inbox, direct URL) and can still chat with both.
