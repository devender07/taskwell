-- Projects. Keys are stored uppercase and stay reserved while a project is archived.
create table projects (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  name         text not null,
  key          text not null,
  description  text not null default '',
  archived_at  timestamptz,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now(),
  unique (workspace_id, key)
);
