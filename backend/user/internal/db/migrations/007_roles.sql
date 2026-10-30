-- Workspace roles. Every workspace has a built-in Admin (all permissions, computed in code) and Member role.
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

insert into workspace_roles (workspace_id, name, builtin, permissions)
select id, 'Admin', 'admin', '{}'::text[] from workspaces
union all
select id, 'Member', 'member', '{issues.create,comments.create}'::text[] from workspaces;

-- No action (not restrict) so deleting a workspace can cascade to members and roles in one statement;
-- it still blocks deleting a role that members point at.
alter table workspace_members add column role_id uuid references workspace_roles;
update workspace_members m set role_id = r.id
  from workspace_roles r where r.workspace_id = m.workspace_id and r.builtin = m.role;
alter table workspace_members alter column role_id set not null, drop column role;
create index on workspace_members (role_id);

-- Null means the workspace's Member role.
alter table workspace_invites add column role_id uuid references workspace_roles on delete set null;
