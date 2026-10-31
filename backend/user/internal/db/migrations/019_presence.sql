-- Who has an issue open, one row per realtime connection and issue. The owning API instance refreshes seen_at every
-- 30s; a row older than 90s counts as gone (its instance crashed) and any instance deletes it.
create table issue_viewers (
  conn_id  text not null,
  issue_id uuid not null references issues on delete cascade,
  user_id  uuid not null references users on delete cascade,
  instance text not null,
  seen_at  timestamptz not null default now(),
  primary key (conn_id, issue_id)
);
create index on issue_viewers (issue_id);
create index on issue_viewers (instance);
