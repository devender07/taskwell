create table comments (
  id         uuid primary key default uuidv7(),
  issue_id   uuid not null references issues on delete cascade,
  author_id  uuid references users on delete set null,
  body       jsonb not null,
  created_at timestamptz not null default now(),
  edited_at  timestamptz
);
create index on comments (issue_id, id);

-- an issue's activity log; data holds name snapshots so entries read right after renames and deletes
create table issue_events (
  id         uuid primary key default uuidv7(),
  issue_id   uuid not null references issues on delete cascade,
  actor_id   uuid references users on delete set null,
  kind       text not null,
  data       jsonb not null default '{}',
  created_at timestamptz not null default now()
);
create index on issue_events (issue_id, id);

insert into issue_events (issue_id, actor_id, kind, created_at) select id, created_by, 'created', created_at from issues;
