-- Roadmap: optional project start and target dates, and per-project milestones.
alter table projects
  add column start_date  date,
  add column target_date date,
  add constraint projects_dates check (start_date is null or target_date is null or start_date <= target_date);

create table project_milestones (
  id         uuid primary key default uuidv7(),
  project_id uuid not null references projects on delete cascade,
  name       text not null check (char_length(name) between 1 and 60),
  date       date not null,
  created_by uuid references users on delete set null,
  created_at timestamptz not null default now()
);
create index on project_milestones (project_id, date);
