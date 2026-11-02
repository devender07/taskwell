-- Private projects (docs/private-projects-plan.md): only their members and workspace admins see them and their issues.
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
