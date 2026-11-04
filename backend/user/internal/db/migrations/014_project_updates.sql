-- Project updates (docs/project-updates-plan.md): a short written update with a health. The latest one's health is the
-- project's.
create table project_updates (
  id         uuid primary key default uuidv7(),
  project_id uuid not null references projects(id) on delete cascade,
  author_id  uuid references users(id) on delete set null,
  health     text not null check (health in ('on_track', 'at_risk', 'off_track')),
  body       jsonb not null,
  created_at timestamptz not null default now(),
  edited_at  timestamptz
);
create index project_updates_project on project_updates (project_id, created_at desc);
