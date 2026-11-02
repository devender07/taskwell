-- Status types: a workspace's own kinds of status, each with an icon, an optional color and the category it counts
-- as. A status with a type takes the type's category; without one it's a built-in of its category.
create table status_types (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null check (char_length(name) between 1 and 30),
  category     text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  icon         text not null,
  color        text check (color ~ '^#[0-9a-f]{6}([0-9a-f]{2})?$'), -- null = the category's color
  created_at   timestamptz not null default now()
);
create unique index on status_types (workspace_id, lower(name));

alter table statuses add column type_id uuid references status_types on delete set null;
create index on statuses (type_id);

-- backfill: one type per workspace and free-text type, counting as its first status's category. Only statuses of
-- that category point at it, so no status changes what it counts as.
insert into status_types (workspace_id, name, category, icon)
select distinct on (workspace_id, lower(type)) workspace_id, type, category,
       case when lower(type) = 'reopen' then 'refresh'
            else case category when 'backlog' then 'circle-dashed' when 'unstarted' then 'circle'
                               when 'started' then 'circle-half' when 'completed' then 'circle-check' else 'circle-x' end end
from statuses where type is not null
order by workspace_id, lower(type), position, id;

update statuses s set type_id = t.id from status_types t
where s.type is not null and t.workspace_id = s.workspace_id and lower(t.name) = lower(s.type) and t.category = s.category;

alter table statuses drop column type;
-- end backfill

-- status_json is a status as the API shows it: with its type's icon and color, null when it has no type.
create function status_json(s statuses) returns json language sql stable as $$
  select json_build_object('id', s.id, 'name', s.name, 'category', s.category, 'position', s.position,
                           'icon', t.icon, 'color', t.color)
  from (select) x left join status_types t on t.id = s.type_id
$$;
