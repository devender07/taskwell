-- Time-boxed cycles per project; an issue is in at most one cycle of its own project (docs/cycles-plan.md).
alter table projects add column cycle_seq int not null default 0; -- last cycle number used

create table cycles (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  project_id   uuid not null references projects on delete cascade,
  number       int not null,
  name         text check (char_length(name) between 1 and 60), -- null shows "Cycle {number}"
  starts_on    date not null,
  ends_on      date not null,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now(),
  unique (project_id, number),
  check (ends_on >= starts_on)
);
create index on cycles (project_id, starts_on);

alter table issues add column cycle_id uuid references cycles on delete set null;
create index on issues (cycle_id);
