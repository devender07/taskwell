-- Creating workspaces is the role permission workspaces.create: a user may create one when a role in any of their
-- workspaces grants it (Admin has every permission), or when they're in no workspace yet (their first one).
create or replace function can_create_workspace(uid uuid) returns boolean language sql stable as $$
  select not exists (select 1 from workspace_members where user_id = uid)
      or exists (select 1 from workspace_members m join workspace_roles r on r.id = m.role_id
                 where m.user_id = uid and (r.builtin = 'admin' or 'workspaces.create' = any(r.permissions)))
$$;
