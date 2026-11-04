-- Guests (docs/guests-plan.md): a built-in Guest role in every workspace. A guest sees only the projects they're a
-- member of (project_members, the table private projects use), public or not. A guest invite carries its projects.
alter table workspace_roles drop constraint workspace_roles_builtin_check,
  add constraint workspace_roles_builtin_check check (builtin in ('admin', 'member', 'guest'));
insert into workspace_roles (workspace_id, name, builtin, permissions)
select w.id, case when exists (select 1 from workspace_roles r where r.workspace_id = w.id and lower(r.name) = 'guest')
                  then 'Guest (built-in)' else 'Guest' end,
       'guest', '{issues.create,comments.create}'
from workspaces w
on conflict (workspace_id, builtin) do nothing;

alter table workspace_invites add column project_ids uuid[] not null default '{}';

create function is_guest(ws uuid, viewer uuid) returns boolean language sql stable as $$
  select exists (select 1 from workspace_members m join workspace_roles r on r.id = m.role_id
                 where m.workspace_id = ws and m.user_id = viewer and r.builtin = 'guest')
$$;

create or replace function can_see_project(project uuid, viewer uuid) returns boolean language sql stable as $$
  select case when is_guest(p.workspace_id, viewer)
           then exists (select 1 from project_members m where m.project_id = p.id and m.user_id = viewer)
           else not p.private
             or exists (select 1 from project_members m where m.project_id = p.id and m.user_id = viewer)
             or exists (select 1 from workspace_members wm join workspace_roles r on r.id = wm.role_id
                        where wm.workspace_id = p.workspace_id and wm.user_id = viewer and r.builtin = 'admin')
         end
  from projects p where p.id = project
$$;
