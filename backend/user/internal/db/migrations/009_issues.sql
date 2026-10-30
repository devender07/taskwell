-- Issues, with workspace statuses and labels. An issue's identifier is its project's key plus its number, e.g. ENG-142.
create table statuses (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null,
  category     text not null check (category in ('backlog', 'unstarted', 'started', 'completed', 'canceled')),
  position     int not null,
  created_at   timestamptz not null default now()
);
create unique index on statuses (workspace_id, lower(name));

-- Every workspace starts with the five default statuses; the trigger covers both create paths.
create function seed_statuses() returns trigger language plpgsql as $$
begin
  insert into statuses (workspace_id, name, category, position) values
    (new.id, 'Backlog', 'backlog', 0), (new.id, 'Todo', 'unstarted', 1), (new.id, 'In progress', 'started', 2),
    (new.id, 'Done', 'completed', 3), (new.id, 'Cancelled', 'canceled', 4);
  return null;
end $$;
create trigger seed_statuses after insert on workspaces for each row execute function seed_statuses();

insert into statuses (workspace_id, name, category, position)
select w.id, d.name, d.category, d.position from workspaces w cross join (values
  ('Backlog', 'backlog', 0), ('Todo', 'unstarted', 1), ('In progress', 'started', 2),
  ('Done', 'completed', 3), ('Cancelled', 'canceled', 4)) d(name, category, position);

create table labels (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null,
  color        text not null check (color ~ '^#[0-9a-f]{6}$'),
  created_at   timestamptz not null default now()
);
create unique index on labels (workspace_id, lower(name));

alter table projects add column issue_seq int not null default 0; -- last issue number used

create table issues (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  project_id   uuid not null references projects on delete cascade,
  number       int not null,
  title        text not null,
  description  text not null default '',
  status_id    uuid not null references statuses,
  assignee_id  uuid references users on delete set null,
  priority     smallint not null default 0 check (priority between 0 and 4), -- 0 none, 1 urgent, 2 high, 3 medium, 4 low
  due_date     date,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now(),
  unique (project_id, number)
);
create index on issues (workspace_id, assignee_id);
create index on issues (status_id);

create table issue_labels (
  issue_id uuid not null references issues on delete cascade,
  label_id uuid not null references labels on delete cascade,
  primary key (issue_id, label_id)
);
create index on issue_labels (label_id);
