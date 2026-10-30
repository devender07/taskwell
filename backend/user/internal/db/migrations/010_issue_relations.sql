-- Sub-issues (one level deep, same project) and "blocks / blocked by" links between a workspace's issues.
alter table issues add column parent_id uuid references issues on delete set null; -- deleting a parent makes its sub-issues top-level
create index on issues (parent_id);

create table issue_links (
  blocker_id uuid not null references issues on delete cascade,
  blocked_id uuid not null references issues on delete cascade,
  created_at timestamptz not null default now(),
  primary key (blocker_id, blocked_id),
  check (blocker_id <> blocked_id)
);
create index on issue_links (blocked_id);
