create table workspaces (
  id         uuid primary key default uuidv7(),
  name       text not null,
  slug       text not null unique,
  created_by uuid references users on delete set null,
  created_at timestamptz not null default now()
);

create table workspace_members (
  workspace_id   uuid not null references workspaces on delete cascade,
  user_id        uuid not null references users on delete cascade,
  role           text not null check (role in ('admin', 'member')),
  last_opened_at timestamptz not null default now(),
  created_at     timestamptz not null default now(),
  primary key (workspace_id, user_id)
);
create index on workspace_members (user_id, last_opened_at desc);
