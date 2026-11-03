-- Customer feedback: from the workspace's public form or added by a member, triaged by linking it to an issue or
-- archiving it.
create table feedback (
  id           uuid primary key default uuidv7(),
  workspace_id uuid not null references workspaces on delete cascade,
  body         text not null check (char_length(body) between 1 and 5000),
  email        text check (char_length(email) <= 254),
  name         text check (char_length(name) <= 80),
  source       text not null check (source in ('form', 'member')),
  issue_id     uuid references issues on delete set null,
  archived_at  timestamptz,
  created_by   uuid references users on delete set null,
  created_at   timestamptz not null default now()
);
create index on feedback (workspace_id, id desc);
create index on feedback (issue_id) where issue_id is not null;

-- The public form's link token; null while the form is off.
alter table workspaces add column feedback_token text unique;
