-- Workspace defaults (docs/workspace-defaults-plan.md): what a new workspace starts with, edited by staff in the admin
-- app. Copied once at creation: roles by workspace.create, the rest by the seed_statuses trigger.
create table default_roles (
  id          uuid primary key default uuidv7(),
  name        text not null check (char_length(name) between 1 and 40),
  builtin     text unique check (builtin in ('member', 'guest')), -- null: a custom role
  permissions text[] not null default '{}',
  created_at  timestamptz not null default now()
);
create unique index default_roles_name_idx on default_roles (lower(name));

create table default_status_types (
  id         uuid primary key default uuidv7(),
  name       text not null check (char_length(name) between 1 and 30),
  category   text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  icon       text not null,
  color      text check (color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'),
  position   integer not null default 0,
  created_at timestamptz not null default now()
);
-- Names are unique like the workspace tables', so copying them can't fail.
create unique index default_status_types_name_idx on default_status_types (lower(name));

create table default_statuses (
  id       uuid primary key default uuidv7(),
  name     text not null,
  category text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  type_id  uuid references default_status_types on delete set null,
  position integer not null
);
create unique index default_statuses_name_idx on default_statuses (lower(name));

create table default_labels (
  id    uuid primary key default uuidv7(),
  name  text not null,
  color text not null check (color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$')
);
create unique index default_labels_name_idx on default_labels (lower(name));

create table default_templates (
  id          uuid primary key default uuidv7(),
  name        text not null,
  title       text not null default '',
  description jsonb,
  status_id   uuid references default_statuses on delete set null,
  priority    smallint not null default 0 check (priority between 0 and 4),
  label_ids   uuid[] not null default '{}', -- deleted labels drop out when copied
  created_at  timestamptz not null default now()
);

insert into default_roles (name, builtin, permissions) values
  ('Member', 'member', '{issues.create,comments.create}'), ('Guest', 'guest', '{issues.create,comments.create}');
insert into default_statuses (name, category, position) values
  ('Backlog', 'backlog', 0), ('Todo', 'unstarted', 1), ('In progress', 'started', 2), ('Done', 'completed', 3),
  ('Cancelled', 'canceled', 4);

-- seed_statuses copies the default workflow, labels and templates into a new workspace, mapping their references to
-- the new rows' ids. Without default statuses it falls back to the five built-in ones: a workspace needs one.
create or replace function seed_statuses() returns trigger language plpgsql as $$
declare
  t_map jsonb := (select coalesce(jsonb_object_agg(id, uuidv7()), '{}') from default_status_types);
  s_map jsonb := (select coalesce(jsonb_object_agg(id, uuidv7()), '{}') from default_statuses);
  l_map jsonb := (select coalesce(jsonb_object_agg(id, uuidv7()), '{}') from default_labels);
begin
  insert into status_types (id, workspace_id, name, category, icon, color)
    select (t_map ->> d.id::text)::uuid, new.id, d.name, d.category, d.icon, d.color from default_status_types d;
  if s_map = '{}' then
    insert into statuses (workspace_id, name, category, position) values
      (new.id, 'Backlog', 'backlog', 0), (new.id, 'Todo', 'unstarted', 1), (new.id, 'In progress', 'started', 2),
      (new.id, 'Done', 'completed', 3), (new.id, 'Cancelled', 'canceled', 4);
  else
    insert into statuses (id, workspace_id, name, category, type_id, position)
      select (s_map ->> d.id::text)::uuid, new.id, d.name, d.category, (t_map ->> d.type_id::text)::uuid, d.position
      from default_statuses d;
  end if;
  insert into labels (id, workspace_id, name, color)
    select (l_map ->> d.id::text)::uuid, new.id, d.name, d.color from default_labels d;
  insert into issue_templates (workspace_id, name, title, description, status_id, priority, label_ids)
    select new.id, d.name, d.title, d.description, (s_map ->> d.status_id::text)::uuid, d.priority,
           array(select (l_map ->> l::text)::uuid from unnest(d.label_ids) l where l_map ? l::text)
    from default_templates d;
  return null;
end $$;
