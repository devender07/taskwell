-- Saved views (docs/saved-views-plan.md): an issue page's path and query under a name, the owner's own or shared with
-- the workspace. A member's views go when they leave (workspace.removeMember).
create table saved_views (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces(id) on delete cascade,
  user_id      uuid not null references users(id) on delete cascade,
  name         text not null,
  path         text not null,
  shared       boolean not null default false,
  created_at   timestamptz not null default now()
);
create index saved_views_workspace on saved_views (workspace_id);
